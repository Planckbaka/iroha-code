package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OAuthConfig holds OAuth 2.0 client configuration for MCP servers.
type OAuthConfig struct {
	AuthorizationURL string
	TokenURL         string
	ClientID         string
	Scopes           []string
}

// Token holds an OAuth 2.0 access token with metadata.
type Token struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope,omitempty"`
}

// GeneratePKCEVerifier generates a cryptographically random PKCE code verifier
// (43-128 characters, base64url-encoded).
func GeneratePKCEVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// GeneratePKCEChallenge creates a PKCE code challenge using S256 method:
// base64url(sha256(verifier)).
func GeneratePKCEChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// StartOAuthFlow performs the manual-copy OAuth 2.0 + PKCE authorization flow.
func StartOAuthFlow(ctx context.Context, config OAuthConfig) (Token, error) {
	verifier := GeneratePKCEVerifier()
	challenge := GeneratePKCEChallenge(verifier)

	u, err := url.Parse(config.AuthorizationURL)
	if err != nil {
		return Token{}, fmt.Errorf("parse authorization url: %w", err)
	}

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", config.ClientID)
	q.Set("redirect_uri", "urn:ietf:wg:oauth:2.0:oob")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if len(config.Scopes) > 0 {
		q.Set("scope", strings.Join(config.Scopes, " "))
	}
	u.RawQuery = q.Encode()

	fmt.Printf("Open this URL in any browser, then paste the authorization code here:\n%s\n", u.String())
	fmt.Print("Authorization code: ")

	var code string
	if _, err := fmt.Scanln(&code); err != nil {
		return Token{}, fmt.Errorf("read authorization code: %w", err)
	}
	code = strings.TrimSpace(code)

	return exchangeCode(ctx, code, verifier, config)
}

// RefreshToken exchanges a refresh token for a new access token.
func RefreshToken(ctx context.Context, refreshToken string, config OAuthConfig) (Token, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {config.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("refresh token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Token{}, fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return parseTokenResponse(resp.Body)
}

// StoreToken persists a token to ~/.iroha/tokens/{serverName}.json with 0600 permissions.
func StoreToken(serverName string, token Token) error {
	dir, err := tokenDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}

	tokData, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	path := filepath.Join(dir, serverName+".json")
	if err := os.WriteFile(path, tokData, 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}

	return nil
}

// LoadToken reads a stored token for the given server. Checks IROHA_MCP_TOKEN
// environment variable first as a bypass.
func LoadToken(serverName string) (Token, error) {
	if envToken := os.Getenv("IROHA_MCP_TOKEN"); envToken != "" {
		return Token{
			AccessToken: envToken,
			TokenType:   "Bearer",
		}, nil
	}

	dir, err := tokenDir()
	if err != nil {
		return Token{}, err
	}

	path := filepath.Join(dir, serverName+".json")
	tokData, err := os.ReadFile(path)
	if err != nil {
		return Token{}, fmt.Errorf("read token file: %w", err)
	}

	var token Token
	if err := json.Unmarshal(tokData, &token); err != nil {
		return Token{}, fmt.Errorf("parse token file: %w", err)
	}

	return token, nil
}

func exchangeCode(ctx context.Context, code, verifier string, config OAuthConfig) (Token, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"urn:ietf:wg:oauth:2.0:oob"},
		"client_id":     {config.ClientID},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("token exchange request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Token{}, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return parseTokenResponse(resp.Body)
}

func parseTokenResponse(body io.Reader) (Token, error) {
	var raw struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}

	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return Token{}, fmt.Errorf("decode token response: %w", err)
	}

	token := Token{
		AccessToken:  raw.AccessToken,
		TokenType:    raw.TokenType,
		RefreshToken: raw.RefreshToken,
		Scope:        raw.Scope,
	}
	if raw.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}

	return token, nil
}

func tokenDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".iroha", "tokens"), nil
}

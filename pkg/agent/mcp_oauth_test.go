package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPKCEGeneration(t *testing.T) {
	verifier := GeneratePKCEVerifier()
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("verifier length %d not in [43, 128]", len(verifier))
	}

	challenge := GeneratePKCEChallenge(verifier)
	if challenge == "" {
		t.Fatal("challenge is empty")
	}

	challenge2 := GeneratePKCEChallenge(verifier)
	if challenge != challenge2 {
		t.Error("challenge should be deterministic")
	}

	verifier2 := GeneratePKCEVerifier()
	challenge3 := GeneratePKCEChallenge(verifier2)
	if challenge == challenge3 && verifier != verifier2 {
		t.Error("different verifiers produced same challenge")
	}

	for _, c := range challenge {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			t.Errorf("challenge contains invalid char: %c", c)
		}
	}
}

func TestTokenRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	serverName := "test-server"
	token := Token{
		AccessToken:  "at-12345",
		TokenType:    "Bearer",
		RefreshToken: "rt-67890",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scope:        "read write",
	}

	if err := StoreToken(serverName, token); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	path := filepath.Join(tmpDir, ".iroha", "tokens", serverName+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 perms, got %o", info.Mode().Perm())
	}

	loaded, err := LoadToken(serverName)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}

	if loaded.AccessToken != token.AccessToken {
		t.Errorf("access token mismatch: got %q, want %q", loaded.AccessToken, token.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("refresh token mismatch: got %q, want %q", loaded.RefreshToken, token.RefreshToken)
	}
	if loaded.TokenType != token.TokenType {
		t.Errorf("token type mismatch: got %q, want %q", loaded.TokenType, token.TokenType)
	}
	if loaded.Scope != token.Scope {
		t.Errorf("scope mismatch: got %q, want %q", loaded.Scope, token.Scope)
	}
}

func TestManualCopyFlow(t *testing.T) {
	verifier := GeneratePKCEVerifier()

	var receivedCode, receivedVerifier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedCode = r.FormValue("code")
		receivedVerifier = r.FormValue("code_verifier")

		resp := map[string]any{
			"access_token":  "test-access-token",
			"token_type":    "Bearer",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
			"scope":         "read",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	token, err := exchangeCode(context.Background(), "test-auth-code", verifier, OAuthConfig{
		TokenURL: server.URL,
		ClientID: "test-client",
	})
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}

	if receivedCode != "test-auth-code" {
		t.Errorf("code mismatch: got %q, want %q", receivedCode, "test-auth-code")
	}
	if receivedVerifier != verifier {
		t.Errorf("verifier mismatch")
	}
	if token.AccessToken != "test-access-token" {
		t.Errorf("access token: got %q", token.AccessToken)
	}
	if token.RefreshToken != "test-refresh-token" {
		t.Errorf("refresh token: got %q", token.RefreshToken)
	}
	if token.ExpiresAt.IsZero() {
		t.Error("expires_at should be set")
	}
}

func TestRefreshFlow(t *testing.T) {
	var receivedGrantType, receivedRefreshToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedGrantType = r.FormValue("grant_type")
		receivedRefreshToken = r.FormValue("refresh_token")

		resp := map[string]any{
			"access_token":  "refreshed-access-token",
			"token_type":    "Bearer",
			"refresh_token": "new-refresh-token",
			"expires_in":    7200,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	token, err := RefreshToken(context.Background(), "old-refresh-token", OAuthConfig{
		TokenURL: server.URL,
		ClientID: "test-client",
	})
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}

	if receivedGrantType != "refresh_token" {
		t.Errorf("grant_type: got %q, want %q", receivedGrantType, "refresh_token")
	}
	if receivedRefreshToken != "old-refresh-token" {
		t.Errorf("refresh_token: got %q, want %q", receivedRefreshToken, "old-refresh-token")
	}
	if token.AccessToken != "refreshed-access-token" {
		t.Errorf("access token: got %q", token.AccessToken)
	}
}

func TestEnvVarBypass(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("IROHA_MCP_TOKEN", "env-token-xyz")

	token, err := LoadToken("any-server")
	if err != nil {
		t.Fatalf("LoadToken with env var: %v", err)
	}

	if token.AccessToken != "env-token-xyz" {
		t.Errorf("expected env token, got %q", token.AccessToken)
	}
	if token.TokenType != "Bearer" {
		t.Errorf("expected Bearer type, got %q", token.TokenType)
	}
}

func TestExchangeCodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	defer server.Close()

	_, err := exchangeCode(context.Background(), "bad-code", "verifier", OAuthConfig{
		TokenURL: server.URL,
		ClientID: "test-client",
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400: %v", err)
	}
}

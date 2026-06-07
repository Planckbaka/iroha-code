package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// MCPTransport is the interface for communicating with an MCP server.
type MCPTransport interface {
	Initialize(ctx context.Context) error
	Call(ctx context.Context, method string, params interface{}) (*JsonRpcMessage, error)
	Close() error
	IsConnected() bool
}

// NewMCPTransport creates a transport for the given MCP server config.
func NewMCPTransport(name string, config MCPServerConfig) MCPTransport {
	if config.URL != "" && (strings.HasPrefix(config.URL, "http://") || strings.HasPrefix(config.URL, "https://")) {
		return NewHTTPTransport(config.URL)
	}
	return &StdioTransport{
		client: NewMCPClient(name, config),
	}
}

// StdioTransport wraps MCPClient to implement MCPTransport via stdin/stdout.
type StdioTransport struct {
	client *MCPClient
}

var _ MCPTransport = (*StdioTransport)(nil)

func (st *StdioTransport) Initialize(ctx context.Context) error {
	return st.client.Start()
}

func (st *StdioTransport) Call(ctx context.Context, method string, params interface{}) (*JsonRpcMessage, error) {
	return st.client.Call(method, params)
}

func (st *StdioTransport) Close() error {
	st.client.Close()
	return nil
}

func (st *StdioTransport) IsConnected() bool {
	st.client.mu.Lock()
	defer st.client.mu.Unlock()
	return st.client.cmd != nil && st.client.cmd.Process != nil
}

// HTTPTransport implements MCPTransport over the MCP Streamable HTTP transport.
type HTTPTransport struct {
	baseURL    string
	sessionID  string
	httpClient *http.Client
	mu         sync.Mutex
}

var _ MCPTransport = (*HTTPTransport)(nil)

// NewHTTPTransport creates an HTTPTransport for the given MCP server URL.
func NewHTTPTransport(baseURL string) *HTTPTransport {
	return &HTTPTransport{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// Initialize sends the MCP initialize request over HTTP and stores the session ID.
func (t *HTTPTransport) Initialize(ctx context.Context) error {
	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "iroha-client",
			"version": "1.0.0",
		},
	}

	_, err := t.Call(ctx, "initialize", initParams)
	if err != nil {
		return fmt.Errorf("http initialize failed: %w", err)
	}

	notifMsg := JsonRpcMessage{
		Jsonrpc: "2.0",
		Method:  "notifications/initialized",
	}
	_, _ = t.doPost(ctx, &notifMsg)

	return nil
}

// Call executes a JSON-RPC request over HTTP and parses the SSE response.
func (t *HTTPTransport) Call(ctx context.Context, method string, params interface{}) (*JsonRpcMessage, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		pData, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		paramsRaw = pData
	}

	msg := &JsonRpcMessage{
		Jsonrpc: "2.0",
		Id:      1,
		Method:  method,
		Params:  paramsRaw,
	}

	respMsg, err := t.doPost(ctx, msg)
	if err != nil {
		return nil, err
	}

	if respMsg.Error != nil {
		return nil, fmt.Errorf("mcp error: %s (code %d)", respMsg.Error.Message, respMsg.Error.Code)
	}

	return respMsg, nil
}

// Close sends a DELETE request to terminate the session.
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()

	if sid == "" {
		return nil
	}

	req, err := http.NewRequest(http.MethodDelete, t.baseURL, nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}
	req.Header.Set("Mcp-Session-Id", sid)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	_ = resp.Body.Close()

	t.mu.Lock()
	t.sessionID = ""
	t.mu.Unlock()

	return nil
}

// IsConnected reports whether a session has been established.
func (t *HTTPTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID != ""
}

func (t *HTTPTransport) doPost(ctx context.Context, msg *JsonRpcMessage) (*JsonRpcMessage, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
	}

	if newSID := resp.Header.Get("Mcp-Session-Id"); newSID != "" {
		t.mu.Lock()
		t.sessionID = newSID
		t.mu.Unlock()
	}

	if msg.Id == nil {
		return &JsonRpcMessage{Jsonrpc: "2.0"}, nil
	}

	result, err := parseSSEResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse sse: %w", err)
	}

	return result, nil
}

// parseSSEResponse reads an SSE stream and extracts the first JSON-RPC message
// from a "data: " line.
func parseSSEResponse(body io.Reader) (*JsonRpcMessage, error) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}

		var msg JsonRpcMessage
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			continue
		}
		return &msg, nil
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading sse stream: %w", err)
	}

	return nil, fmt.Errorf("no data event found in sse stream")
}

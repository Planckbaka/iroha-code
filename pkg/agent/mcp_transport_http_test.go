package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPTransportWithTestServer(t *testing.T) {
	var sessionID atomic.Int64
	var nextID int64

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sid := sessionID.Load()
		if sid == 0 {
			id := nextID + 1
			nextID = id
			sessionID.Store(id)
			w.Header().Set("Mcp-Session-Id", fmt.Sprintf("session-%d", id))
		}

		if sentSID := r.Header.Get("Mcp-Session-Id"); sentSID != "" && sentSID != fmt.Sprintf("session-%d", sessionID.Load()) {
			t.Errorf("session ID mismatch: got %q, want %q", sentSID, fmt.Sprintf("session-%d", sessionID.Load()))
		}

		var reqMsg JsonRpcMessage
		if err := json.NewDecoder(r.Body).Decode(&reqMsg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		if reqMsg.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusOK)
			return
		}

		result, _ := json.Marshal(map[string]any{"status": "ok"})
		resp := JsonRpcMessage{
			Jsonrpc: "2.0",
			Id:      reqMsg.Id,
			Result:  result,
		}
		respData, _ := json.Marshal(resp)
		fmt.Fprintf(w, "data: %s\n\n", string(respData))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	transport := NewHTTPTransport(server.URL)

	if err := transport.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if !transport.IsConnected() {
		t.Fatal("expected transport to be connected after Initialize")
	}

	resp, err := transport.Call(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if transport.IsConnected() {
		t.Fatal("expected transport to be disconnected after Close")
	}
}

func TestHTTPTransportSessionManagement(t *testing.T) {
	receivedSessionIDs := []string{}
	var mux http.ServeMux
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sid := r.Header.Get("Mcp-Session-Id")
		receivedSessionIDs = append(receivedSessionIDs, sid)

		if sid == "" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		var reqMsg JsonRpcMessage
		json.NewDecoder(r.Body).Decode(&reqMsg)

		result, _ := json.Marshal(map[string]any{"ok": true})
		resp := JsonRpcMessage{Jsonrpc: "2.0", Id: reqMsg.Id, Result: result}
		respData, _ := json.Marshal(resp)
		fmt.Fprintf(w, "data: %s\n\n", string(respData))
	})

	server := httptest.NewServer(&mux)
	defer server.Close()

	transport := NewHTTPTransport(server.URL)
	transport.Initialize(context.Background())
	transport.Call(context.Background(), "tools/list", nil)

	if len(receivedSessionIDs) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(receivedSessionIDs))
	}
	if receivedSessionIDs[0] != "" {
		t.Errorf("first request should have no session ID, got %q", receivedSessionIDs[0])
	}
	if receivedSessionIDs[1] != "test-session-123" {
		t.Errorf("second request should have session ID, got %q", receivedSessionIDs[1])
	}

	transport.Close()
}

func TestHTTPTransportSSEParsing(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "valid single event",
			input: "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n",
		},
		{
			name:  "event with prefix lines",
			input: "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
		},
		{
			name:  "multiple data lines",
			input: "data: ignored\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"v\":1}}\n\n",
		},
		{
			name:    "no data event",
			input:   "event: message\n\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := parseSSEResponse(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msg.Jsonrpc != "2.0" {
				t.Errorf("expected jsonrpc 2.0, got %s", msg.Jsonrpc)
			}
		})
	}
}

func TestNewMCPTransportHTTPRouting(t *testing.T) {
	httpConfig := MCPServerConfig{URL: "https://example.com/mcp"}
	transport := NewMCPTransport("test", httpConfig)
	if _, ok := transport.(*HTTPTransport); !ok {
		t.Errorf("expected HTTPTransport for URL config, got %T", transport)
	}

	cmdConfig := MCPServerConfig{Command: "some-binary"}
	transport = NewMCPTransport("test", cmdConfig)
	if _, ok := transport.(*StdioTransport); !ok {
		t.Errorf("expected StdioTransport for Command config, got %T", transport)
	}

	httpConfig2 := MCPServerConfig{URL: "http://localhost:8080/mcp"}
	transport = NewMCPTransport("test", httpConfig2)
	if _, ok := transport.(*HTTPTransport); !ok {
		t.Errorf("expected HTTPTransport for http:// URL, got %T", transport)
	}

	emptyConfig := MCPServerConfig{Command: "another-binary", URL: ""}
	transport = NewMCPTransport("test", emptyConfig)
	if _, ok := transport.(*StdioTransport); !ok {
		t.Errorf("expected StdioTransport for empty URL, got %T", transport)
	}
}

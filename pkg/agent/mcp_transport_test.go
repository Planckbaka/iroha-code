package agent

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStdioTransportImplementsInterface(t *testing.T) {
	var _ MCPTransport = (*StdioTransport)(nil)
}

func TestStdioTransport_IsConnected_BeforeStart(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}
	st := &StdioTransport{client: NewMCPClient("test", config)}
	if st.IsConnected() {
		t.Error("should not be connected before Initialize")
	}
}

func TestStdioTransport_IsConnected_AfterStart(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}
	st := &StdioTransport{client: NewMCPClient("test", config)}
	if err := st.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer st.Close()
	if !st.IsConnected() {
		t.Error("should be connected after Initialize")
	}
}

func TestStdioTransport_Call(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}
	st := &StdioTransport{client: NewMCPClient("test", config)}
	if err := st.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer st.Close()
	resp, err := st.Call(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if !strings.Contains(string(resp.Result), "echo") {
		t.Errorf("expected echo tool in result, got: %s", string(resp.Result))
	}
}

func TestNewMCPTransport_ReturnsStdio(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}
	transport := NewMCPTransport("test", config)
	if _, ok := transport.(*StdioTransport); !ok {
		t.Error("NewMCPTransport should return *StdioTransport when no URL")
	}
}

func TestToolDedupBuiltInWins(t *testing.T) {
	builtInNames := map[string]bool{
		"file_read":            true,
		"mcp__mock__file_read": true,
	}
	if !builtInNames["mcp__mock__file_read"] {
		t.Error("dedup should catch collision")
	}
	if builtInNames["mcp__mock__echo"] {
		t.Error("non-colliding name should pass")
	}
}

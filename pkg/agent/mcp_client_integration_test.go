package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegration_MCP_NewClientConstruction(t *testing.T) {
	config := MCPServerConfig{
		Command: "echo",
		Args:    []string{"test"},
	}
	client := NewMCPClient("test-client", config)

	if client.name != "test-client" {
		t.Errorf("expected name 'test-client', got %q", client.name)
	}
	if client.config.Command != "echo" {
		t.Errorf("expected command 'echo', got %q", client.config.Command)
	}
	if client.pending == nil {
		t.Error("expected pending map to be initialized")
	}
	if client.nextID != 1 {
		t.Errorf("expected nextID 1, got %d", client.nextID)
	}
	if client.stopChan == nil {
		t.Error("expected stopChan to be initialized")
	}
}

func TestIntegration_MCP_StartHandshake(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}

	client := NewMCPClient("test-handshake", config)
	err := client.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Close()

	// After Start, nextID should have advanced past initialize
	if client.nextID < 2 {
		t.Errorf("expected nextID >= 2 after handshake, got %d", client.nextID)
	}
}

func TestIntegration_MCP_CallRequestResponse(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}

	client := NewMCPClient("test-call", config)
	if err := client.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Close()

	// Test tools/list
	resp, err := client.Call("tools/list", nil)
	if err != nil {
		t.Fatalf("Call tools/list failed: %v", err)
	}
	if !strings.Contains(string(resp.Result), "echo") {
		t.Errorf("expected result to contain 'echo', got: %s", string(resp.Result))
	}

	// Test tools/call
	resp, err = client.Call("tools/call", map[string]any{
		"name":      "echo",
		"arguments": map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("Call tools/call failed: %v", err)
	}
	if !strings.Contains(string(resp.Result), "hello mock") {
		t.Errorf("expected result to contain 'hello mock', got: %s", string(resp.Result))
	}
}

func TestIntegration_MCP_SendNotification(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}

	client := NewMCPClient("test-notify", config)
	if err := client.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Close()

	// SendNotification should not error (notifications have no ID)
	err := client.SendNotification("notifications/progress", map[string]any{
		"progress":     50,
		"total":        100,
		"progressToken": "test-token",
	})
	if err != nil {
		t.Fatalf("SendNotification failed: %v", err)
	}
}

func TestIntegration_MCP_CloseIdempotent(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}

	client := NewMCPClient("test-close", config)
	if err := client.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Close twice should not panic
	client.Close()
	client.Close()
}

func TestIntegration_MCP_CallOnClosedClient(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}

	client := NewMCPClient("test-closed-call", config)
	if err := client.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	client.Close()

	// Call on closed client should return error
	_, err := client.Call("tools/list", nil)
	if err == nil {
		t.Error("expected error when calling on closed client")
	}
}

func TestIntegration_MCP_SendNotificationOnClosedClient(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}

	client := NewMCPClient("test-closed-notify", config)
	if err := client.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	client.Close()

	// SendNotification on closed client should return error
	err := client.SendNotification("test/notify", nil)
	if err == nil {
		t.Error("expected error when sending notification on closed client")
	}
}

func TestIntegration_MCP_StartInvalidCommand(t *testing.T) {
	config := MCPServerConfig{
		Command: "nonexistent-binary-that-does-not-exist-12345",
	}

	client := NewMCPClient("test-invalid", config)
	err := client.Start()
	if err == nil {
		client.Close()
		t.Error("expected error for invalid command")
	}
}

func TestIntegration_MCP_ReadLoopJSONParse(t *testing.T) {
	config := MCPServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1"},
	}

	client := NewMCPClient("test-readloop", config)
	if err := client.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Close()

	// The readLoop is already running in the background.
	// Verify it correctly dispatches responses by doing a Call round-trip.
	// This implicitly tests readLoop's JSON parsing and ID-based dispatch.
	resp, err := client.Call("tools/list", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Id != nil {
		// Response should have an ID that matches
		t.Logf("Response ID: %v", resp.Id)
	}

	// Wait briefly to ensure readLoop doesn't panic
	time.Sleep(100 * time.Millisecond)
}

package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// runHTTP tests using httptest.Server
// ---------------------------------------------------------------------------

func TestHookManager_RunHTTP_Allow(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Parse the incoming payload to verify structure
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		if payload["tool_name"] != "shell_run" {
			t.Errorf("payload tool_name = %v, want 'shell_run'", payload["tool_name"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"decision": "allow",
			"message":  "proceed safely",
		})
	}))
	defer ts.Close()

	hm := NewHookManager()

	result := hm.runHTTP(HookPreToolUse, HookDef{
		Type: HookTypeHTTP,
		URL:  ts.URL,
	}, HookContext{ToolName: "shell_run", SessionID: "sess-1"})

	if result.Blocked {
		t.Errorf("expected not blocked, got Blocked=true (reason: %q)", result.BlockReason)
	}
	if len(result.Messages) != 1 || result.Messages[0] != "proceed safely" {
		t.Errorf("expected message 'proceed safely', got %v", result.Messages)
	}
}

func TestHookManager_RunHTTP_Deny(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"decision": "deny",
			"reason":   "operation not allowed by policy",
		})
	}))
	defer ts.Close()

	hm := NewHookManager()

	result := hm.runHTTP(HookPreToolUse, HookDef{
		Type: HookTypeHTTP,
		URL:  ts.URL,
	}, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Fatal("expected Blocked=true")
	}
	if result.BlockReason != "operation not allowed by policy" {
		t.Errorf("BlockReason = %q, want 'operation not allowed by policy'", result.BlockReason)
	}
}

func TestHookManager_RunHTTP_Timeout_Block(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond, force timeout
		time.Sleep(5 * time.Second)
	}))
	defer ts.Close()

	hm := NewHookManager()

	result := hm.runHTTP(HookPreToolUse, HookDef{
		Type:     HookTypeHTTP,
		URL:      ts.URL,
		Timeout:  1, // 1 second timeout
		OnTimeout: "block",
	}, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Error("expected Blocked=true on timeout with OnTimeout=block")
	}
}

func TestHookManager_RunHTTP_Timeout_Pass(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer ts.Close()

	hm := NewHookManager()

	result := hm.runHTTP(HookPreToolUse, HookDef{
		Type:    HookTypeHTTP,
		URL:     ts.URL,
		Timeout: 1,
	}, HookContext{ToolName: "shell_run"})

	// Default OnTimeout (empty) should pass through
	if result.Blocked {
		t.Errorf("expected not blocked on timeout without OnTimeout=block, got: %q", result.BlockReason)
	}
}

func TestHookManager_RunHTTP_CustomHeaders(t *testing.T) {
	var receivedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"decision": "allow"})
	}))
	defer ts.Close()

	hm := NewHookManager()

	_ = hm.runHTTP(HookPreToolUse, HookDef{
		Type: HookTypeHTTP,
		URL:  ts.URL,
		Headers: map[string]string{
			"Authorization": "Bearer test-token-123",
		},
	}, HookContext{ToolName: "shell_run"})

	if receivedAuth != "Bearer test-token-123" {
		t.Errorf("Authorization header = %q, want 'Bearer test-token-123'", receivedAuth)
	}
}

func TestHookManager_RunHTTP_AllowedEnvVarsExpansion(t *testing.T) {
	// Set a test env var
	os.Setenv("TEST_HOOK_SECRET", "my-secret-value")
	defer os.Unsetenv("TEST_HOOK_SECRET")

	var receivedSecret string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSecret = r.Header.Get("X-Secret")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"decision": "allow"})
	}))
	defer ts.Close()

	hm := NewHookManager()

	_ = hm.runHTTP(HookPreToolUse, HookDef{
		Type: HookTypeHTTP,
		URL:  ts.URL,
		Headers: map[string]string{
			"X-Secret": "$TEST_HOOK_SECRET",
		},
		AllowedEnvVars: []string{"TEST_HOOK_SECRET"},
	}, HookContext{ToolName: "shell_run"})

	if receivedSecret != "my-secret-value" {
		t.Errorf("X-Secret = %q, want 'my-secret-value'", receivedSecret)
	}
}

func TestHookManager_RunHTTP_BadStatusCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	hm := NewHookManager()

	result := hm.runHTTP(HookPreToolUse, HookDef{
		Type: HookTypeHTTP,
		URL:  ts.URL,
	}, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Error("expected Blocked=true for 500 response")
	}
}

func TestHookManager_RunHTTP_UpdatedInput(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"hookSpecificOutput": map[string]any{
				"permissionDecision": "allow",
				"updatedInput": map[string]any{
					"command": "npm test -- --coverage",
				},
				"additionalContext": "running with coverage",
			},
		})
	}))
	defer ts.Close()

	hm := NewHookManager()

	result := hm.runHTTP(HookPreToolUse, HookDef{
		Type: HookTypeHTTP,
		URL:  ts.URL,
	}, HookContext{ToolName: "shell_run"})

	if result.Blocked {
		t.Errorf("expected not blocked, got: %q", result.BlockReason)
	}
	if result.AdditionalContext != "running with coverage" {
		t.Errorf("AdditionalContext = %q, want 'running with coverage'", result.AdditionalContext)
	}
	updatedMap, ok := result.UpdatedInput.(map[string]any)
	if !ok {
		t.Fatalf("UpdatedInput type = %T, want map[string]any", result.UpdatedInput)
	}
	if updatedMap["command"] != "npm test -- --coverage" {
		t.Errorf("UpdatedInput command = %v, want 'npm test -- --coverage'", updatedMap["command"])
	}
}

// ---------------------------------------------------------------------------
// runCommand tests
// ---------------------------------------------------------------------------

func TestHookManager_RunCommand_Exit0(t *testing.T) {
	hm := NewHookManager()

	result := hm.runCommand(HookPreToolUse, HookDef{
		Command: "exit 0",
	}, HookContext{ToolName: "shell_run"})

	if result.Blocked {
		t.Errorf("exit 0 should not block, got: %q", result.BlockReason)
	}
}

func TestHookManager_RunCommand_Exit1(t *testing.T) {
	hm := NewHookManager()

	result := hm.runCommand(HookPreToolUse, HookDef{
		Command: "echo 'denied' >&2; exit 1",
	}, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Error("exit 1 should block")
	}
	if result.BlockReason != "denied" {
		t.Errorf("BlockReason = %q, want 'denied'", result.BlockReason)
	}
}

func TestHookManager_RunCommand_Exit2_Inject(t *testing.T) {
	hm := NewHookManager()

	result := hm.runCommand(HookPostToolUse, HookDef{
		Command: "echo 'lint ok' >&2; exit 2",
	}, HookContext{ToolName: "file_write"})

	if result.Blocked {
		t.Error("exit 2 should not block")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "lint ok" {
		t.Errorf("expected message 'lint ok', got %v", result.Messages)
	}
}

func TestHookManager_RunCommand_Timeout_Block(t *testing.T) {
	hm := NewHookManager()

	result := hm.runCommand(HookPreToolUse, HookDef{
		Command:  "sleep 10",
		Timeout:  1,
		OnTimeout: "block",
	}, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Error("expected Blocked=true on timeout with OnTimeout=block")
	}
}

func TestHookManager_RunCommand_Timeout_Pass(t *testing.T) {
	hm := NewHookManager()

	result := hm.runCommand(HookPreToolUse, HookDef{
		Command: "sleep 10",
		Timeout: 1,
	}, HookContext{ToolName: "shell_run"})

	if result.Blocked {
		t.Errorf("expected not blocked on timeout without block policy, got: %q", result.BlockReason)
	}
}

func TestHookManager_RunCommand_EnvVars(t *testing.T) {
	hm := NewHookManager()

	outFile := t.TempDir() + "/env_output.txt"
	result := hm.runCommand(HookPreToolUse, HookDef{
		Command: "echo \"$HOOK_EVENT $HOOK_TOOL_NAME\" > " + outFile,
	}, HookContext{
		ToolName:  "file_read",
		SessionID: "sess-abc",
	})

	if result.Blocked {
		t.Errorf("should not block, got: %q", result.BlockReason)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	content := string(data)
	if content != "PreToolUse file_read\n" {
		t.Errorf("env output = %q, want 'PreToolUse file_read\\n'", content)
	}
}

// ---------------------------------------------------------------------------
// runOne routing
// ---------------------------------------------------------------------------

func TestHookManager_RunOne_HTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"decision": "deny", "reason": "routed via runOne"})
	}))
	defer ts.Close()

	hm := NewHookManager()

	result := hm.runOne(HookPreToolUse, HookDef{
		Type: HookTypeHTTP,
		URL:  ts.URL,
	}, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Error("expected runOne to route to runHTTP and block")
	}
	if result.BlockReason != "routed via runOne" {
		t.Errorf("BlockReason = %q, want 'routed via runOne'", result.BlockReason)
	}
}

func TestHookManager_RunOne_Command(t *testing.T) {
	hm := NewHookManager()

	// Default type (empty) routes to command
	result := hm.runOne(HookPreToolUse, HookDef{
		Command: "exit 0",
	}, HookContext{ToolName: "shell_run"})

	if result.Blocked {
		t.Error("expected runOne to route to runCommand and not block")
	}
}

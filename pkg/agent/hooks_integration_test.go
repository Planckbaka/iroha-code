package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ─── parseJSONResult Integration Tests ────────────────────────────────────────

func TestIntegration_ParseJSONResult_DenyDecision(t *testing.T) {
	jsonInput := `{"decision":"deny","reason":"security violation"}`
	result := parseJSONResult(HookPreToolUse, []byte(jsonInput), 10, HookContext{ToolName: "shell_run"}, 0)

	if !result.Blocked {
		t.Error("expected Blocked=true for deny decision")
	}
	if result.BlockReason != "security violation" {
		t.Errorf("expected block reason 'security violation', got %q", result.BlockReason)
	}
}

func TestIntegration_ParseJSONResult_DenyViaHookSpecificOutput(t *testing.T) {
	jsonInput := `{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"blocked by policy"}}`
	result := parseJSONResult(HookPreToolUse, []byte(jsonInput), 10, HookContext{ToolName: "shell_run"}, 0)

	if !result.Blocked {
		t.Error("expected Blocked=true for hookSpecificOutput deny")
	}
	if result.BlockReason != "blocked by policy" {
		t.Errorf("expected block reason 'blocked by policy', got %q", result.BlockReason)
	}
}

func TestIntegration_ParseJSONResult_ExitCode2Blocks(t *testing.T) {
	jsonInput := `{"decision":"allow","reason":""}`
	result := parseJSONResult(HookPreToolUse, []byte(jsonInput), 10, HookContext{ToolName: "shell_run"}, 2)

	if !result.Blocked {
		t.Error("expected Blocked=true for exit code 2")
	}
}

func TestIntegration_ParseJSONResult_AllowWithModifications(t *testing.T) {
	jsonInput := `{
		"decision": "allow",
		"hookSpecificOutput": {
			"permissionDecision": "allow",
			"updatedInput": {"command": "npm test -- --coverage"},
			"additionalContext": "timeout is 5s"
		}
	}`
	result := parseJSONResult(HookPreToolUse, []byte(jsonInput), 10, HookContext{ToolName: "shell_run"}, 0)

	if result.Blocked {
		t.Error("expected not blocked for allow decision")
	}
	if result.AdditionalContext != "timeout is 5s" {
		t.Errorf("expected additionalContext 'timeout is 5s', got %q", result.AdditionalContext)
	}
	updatedMap, ok := result.UpdatedInput.(map[string]any)
	if !ok {
		t.Fatalf("expected UpdatedInput to be map, got %T", result.UpdatedInput)
	}
	if updatedMap["command"] != "npm test -- --coverage" {
		t.Errorf("expected updated command 'npm test -- --coverage', got %v", updatedMap["command"])
	}
}

func TestIntegration_ParseJSONResult_ModificationsToolInput(t *testing.T) {
	jsonInput := `{"modifications":{"tool_input":{"path":"/safe/path"}}}`
	result := parseJSONResult(HookPreToolUse, []byte(jsonInput), 5, HookContext{ToolName: "file_write"}, 0)

	if result.Blocked {
		t.Error("expected not blocked")
	}
	updatedMap, ok := result.UpdatedInput.(map[string]any)
	if !ok {
		t.Fatalf("expected UpdatedInput to be map, got %T", result.UpdatedInput)
	}
	if updatedMap["path"] != "/safe/path" {
		t.Errorf("expected updated path '/safe/path', got %v", updatedMap["path"])
	}
}

func TestIntegration_ParseJSONResult_InvalidJSON(t *testing.T) {
	result := parseJSONResult(HookPreToolUse, []byte("not valid json{"), 5, HookContext{ToolName: "shell_run"}, 0)

	if result.Blocked {
		t.Error("expected not blocked for invalid JSON")
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected no messages for invalid JSON, got %v", result.Messages)
	}
}

func TestIntegration_ParseJSONResult_DenyNoReason(t *testing.T) {
	jsonInput := `{"decision":"deny"}`
	result := parseJSONResult(HookPreToolUse, []byte(jsonInput), 5, HookContext{ToolName: "shell_run"}, 0)

	if !result.Blocked {
		t.Error("expected blocked for deny with no reason")
	}
	if !strings.Contains(result.BlockReason, "blocked by hook decision") {
		t.Errorf("expected default block reason, got %q", result.BlockReason)
	}
}

func TestIntegration_ParseJSONResult_MessageField(t *testing.T) {
	jsonInput := `{"decision":"allow","message":"Operation logged successfully"}`
	result := parseJSONResult(HookPreToolUse, []byte(jsonInput), 5, HookContext{ToolName: "shell_run"}, 0)

	if result.Blocked {
		t.Error("expected not blocked")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "Operation logged successfully" {
		t.Errorf("expected message 'Operation logged successfully', got %v", result.Messages)
	}
}

// ─── hookTimeoutForEvent Integration Tests ────────────────────────────────────

func TestIntegration_HookTimeoutForEvent(t *testing.T) {
	tests := []struct {
		event  HookEvent
		expect time.Duration
	}{
		{HookPreToolUse, 5 * time.Second},
		{HookPostToolUse, 5 * time.Second},
		{HookToolError, 5 * time.Second},
		{HookSessionStart, 10 * time.Second},
		{HookSessionEnd, 10 * time.Second},
		{HookSubagentStop, 10 * time.Second},
		{HookUserPrompt, 15 * time.Second},
		{HookAgentResponse, 15 * time.Second},
		{HookCompaction, 10 * time.Second},
		{HookPreCompact, 10 * time.Second},
		{HookPostCompact, 10 * time.Second},
		{HookNotification, 5 * time.Second}, // default
	}

	for _, tc := range tests {
		t.Run(string(tc.event), func(t *testing.T) {
			got := hookTimeoutForEvent(tc.event)
			if got != tc.expect {
				t.Errorf("hookTimeoutForEvent(%s) = %v, want %v", tc.event, got, tc.expect)
			}
		})
	}
}

// ─── hookTruncate Integration Tests ───────────────────────────────────────────

func TestIntegration_HookTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		expect string
	}{
		{"short string passes through", "hello", 10, "hello"},
		{"exact length passes through", "hello", 5, "hello"},
		{"long string truncated", "hello world", 5, "hello"},
		{"empty string", "", 10, ""},
		{"zero maxLen", "test", 0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hookTruncate(tc.input, tc.maxLen)
			if result != tc.expect {
				t.Errorf("hookTruncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, result, tc.expect)
			}
		})
	}
}

// ─── mergePluginHooks Integration Tests ───────────────────────────────────────

func TestIntegration_MergePluginHooks(t *testing.T) {
	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		timeout: 5 * time.Second,
	}

	// Merge first set of plugin hooks
	hm.mergePluginHooks(map[string][]HookDef{
		"PreToolUse": {
			{Command: "exit 0"},
		},
	})

	hooks := hm.GetHooks()
	if len(hooks["PreToolUse"]) != 1 {
		t.Fatalf("expected 1 PreToolUse hook, got %d", len(hooks["PreToolUse"]))
	}

	// Merge second set - should append, not replace
	hm.mergePluginHooks(map[string][]HookDef{
		"PreToolUse": {
			{Command: "exit 1"},
		},
		"PostToolUse": {
			{Command: "exit 0"},
		},
	})

	hooks = hm.GetHooks()
	if len(hooks["PreToolUse"]) != 2 {
		t.Errorf("expected 2 PreToolUse hooks after second merge, got %d", len(hooks["PreToolUse"]))
	}
	if len(hooks["PostToolUse"]) != 1 {
		t.Errorf("expected 1 PostToolUse hook, got %d", len(hooks["PostToolUse"]))
	}
}

// ─── Full Hook Pipeline Integration ───────────────────────────────────────────

func TestIntegration_FullHookPipeline_MultipleHookTypes(t *testing.T) {
	// Set up an HTTP server that returns allow
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"allow","message":"http hook passed"}`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				// Hook 1: command that exits 0 (passes)
				{Command: "exit 0"},
				// Hook 2: HTTP hook that returns allow
				{Type: HookTypeHTTP, URL: ts.URL},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})

	if result.Blocked {
		t.Errorf("expected not blocked, got Blocked=true (reason: %q)", result.BlockReason)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message from http hook, got %d", len(result.Messages))
	}
	if result.Messages[0] != "http hook passed" {
		t.Errorf("expected 'http hook passed', got %q", result.Messages[0])
	}
}

func TestIntegration_FullHookPipeline_BlockingHookStopsPipeline(t *testing.T) {
	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				// First hook passes
				{Command: "exit 0"},
				// Second hook blocks
				{Command: "echo 'forbidden operation' >&2; exit 1"},
				// Third hook would also pass, but should never run
				{Command: "exit 0"},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Error("expected Blocked=true")
	}
	if !strings.Contains(result.BlockReason, "forbidden") {
		t.Errorf("expected block reason containing 'forbidden', got %q", result.BlockReason)
	}
}

// ─── Hook with Timeout Integration ────────────────────────────────────────────

func TestIntegration_HookTimeout_OnTimeoutEmpty(t *testing.T) {
	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{
					Command:  "sleep 5",
					Timeout:  1,
					OnTimeout: "",
				},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	start := time.Now()
	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})
	elapsed := time.Since(start)

	// Should timeout and return empty result (not blocked)
	if result.Blocked {
		t.Error("expected not blocked on timeout with empty OnTimeout")
	}
	// Should have timed out within ~2s (1s timeout + overhead)
	if elapsed > 3*time.Second {
		t.Errorf("expected quick timeout, took %v", elapsed)
	}
}

func TestIntegration_HookTimeout_OnTimeoutBlock(t *testing.T) {
	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{
					Command:  "sleep 5",
					Timeout:  1,
					OnTimeout: "block",
				},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})

	if !result.Blocked {
		t.Error("expected blocked on timeout with OnTimeout=block")
	}
	if !strings.Contains(result.BlockReason, "timed out") {
		t.Errorf("expected block reason containing 'timed out', got %q", result.BlockReason)
	}
}

// ─── HTTP Hook Edge Cases ─────────────────────────────────────────────────────

func TestIntegration_HTTPHook_ServerReturnsDeny(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"deny","reason":"forbidden tool"}`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{Type: HookTypeHTTP, URL: ts.URL},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})
	if !result.Blocked {
		t.Error("expected blocked from HTTP hook deny")
	}
}

func TestIntegration_HTTPHook_Non200Status(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{Type: HookTypeHTTP, URL: ts.URL},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})
	if !result.Blocked {
		t.Error("expected blocked for HTTP 500")
	}
}

// ─── Reload Integration ──────────────────────────────────────────────────────

func TestIntegration_HookReload_UpdatesHooks(t *testing.T) {
	dir := t.TempDir()

	// Start with no hooks
	hm := newManagerFromDir(t, dir)
	if !hm.IsEmpty() {
		t.Error("expected empty initially")
	}

	// Write config and reload
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "exit 0"},
				{Command: "exit 0"},
			},
		},
	})
	hm.Reload()

	if hm.IsEmpty() {
		t.Error("expected non-empty after reload")
	}
	hooks := hm.GetHooks()
	if len(hooks["PreToolUse"]) != 2 {
		t.Errorf("expected 2 hooks after reload, got %d", len(hooks["PreToolUse"]))
	}
}

// ─── GetHooks returns deep copy ──────────────────────────────────────────────

func TestIntegration_GetHooks_DeepCopy(t *testing.T) {
	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "exit 0"},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	copy1 := hm.GetHooks()
	copy2 := hm.GetHooks()

	// Mutating the copy should not affect the original
	copy1["PreToolUse"] = append(copy1["PreToolUse"], HookDef{Command: "exit 1"})

	if len(copy2["PreToolUse"]) != 1 {
		t.Error("GetHooks should return a deep copy, but mutation affected it")
	}
}

// ─── JSON output parsing in runCommand ────────────────────────────────────────

func TestIntegration_RunCommand_JSONOutputWithMessage(t *testing.T) {
	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PostToolUse": {
				{Command: `echo '{"message":"post-tool annotation","modifications":{"tool_input":{"extra":"data"}}}'`},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	result := hm.RunHooks(HookPostToolUse, HookContext{ToolName: "file_write", ToolOutput: "ok"})

	if result.Blocked {
		t.Error("expected not blocked")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "post-tool annotation" {
		t.Errorf("expected message 'post-tool annotation', got %v", result.Messages)
	}
	updatedMap, ok := result.UpdatedInput.(map[string]any)
	if !ok {
		t.Fatalf("expected UpdatedInput map, got %T", result.UpdatedInput)
	}
	if updatedMap["extra"] != "data" {
		t.Errorf("expected extra='data', got %v", updatedMap["extra"])
	}
}

// ─── Hook config load from multiple sources ──────────────────────────────────

func TestIntegration_HooksFromProjectConfig(t *testing.T) {
	dir := t.TempDir()

	// Write hooks.json in project .iroha dir
	hooksDir := dir + "/.iroha"
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := HookConfig{
		Hooks: map[string][]HookDef{
			"SessionStart": {
				{Command: "echo 'session started'"},
			},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(hooksDir+"/hooks.json", data, 0644); err != nil {
		t.Fatal(err)
	}

	hm := newManagerFromDir(t, dir)

	if hm.IsEmpty() {
		t.Error("expected hooks loaded from project config")
	}

	hooks := hm.GetHooks()
	if len(hooks["SessionStart"]) != 1 {
		t.Errorf("expected 1 SessionStart hook, got %d", len(hooks["SessionStart"]))
	}
}

// ─── HTTP hook receives correct payload ───────────────────────────────────────

func TestIntegration_HTTPHook_ReceivesPayload(t *testing.T) {
	var receivedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"allow"}`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	writeHooksConfig(t, dir, HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{Type: HookTypeHTTP, URL: ts.URL},
			},
		},
	})
	hm := newManagerFromDir(t, dir)

	hm.RunHooks(HookPreToolUse, HookContext{
		ToolName:  "file_write",
		ToolInput: map[string]any{"path": "/tmp/test.txt"},
		SessionID: "session-42",
	})

	if receivedBody == nil {
		t.Fatal("HTTP server did not receive request body")
	}
	if receivedBody["tool_name"] != "file_write" {
		t.Errorf("expected tool_name=file_write, got %v", receivedBody["tool_name"])
	}
	if receivedBody["session_id"] != "session-42" {
		t.Errorf("expected session_id=session-42, got %v", receivedBody["session_id"])
	}
	if receivedBody["hookEventName"] != "PreToolUse" {
		t.Errorf("expected hookEventName=PreToolUse, got %v", receivedBody["hookEventName"])
	}
}

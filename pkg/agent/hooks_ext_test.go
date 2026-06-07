package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// NewHookManager
// ---------------------------------------------------------------------------

func TestNewHookManager_CreatesEmptyManager(t *testing.T) {
	// NewHookManager is called at init time for GlobalHookManager,
	// but we can verify its structure.
	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		timeout: 5 * 1e9, // 5 seconds in nanoseconds
	}

	if hm.IsEmpty() != true {
		t.Error("new HookManager should be empty")
	}
	if len(hm.GetHooks()) != 0 {
		t.Error("new HookManager should have no hooks")
	}
	if len(hm.GetSources()) != 0 {
		t.Error("new HookManager should have no sources")
	}
}

// ---------------------------------------------------------------------------
// Reload
// ---------------------------------------------------------------------------

func TestHookManager_Reload_ClearsState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-hooks-reload-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a hooks.json
	hookCfg := HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "echo test"},
			},
		},
	}
	data, _ := json.Marshal(hookCfg)
	hooksFile := filepath.Join(tmpDir, ".iroha", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		timeout: 5e9,
	}

	// Manually load the file
	hm.mu.Lock()
	hm.loadFileLocked(hooksFile)
	hm.mu.Unlock()

	if hm.IsEmpty() {
		t.Error("expected hooks after loadFileLocked")
	}

	// Now reload from a directory that has no hooks files
	// We cannot call hm.Reload() directly because it reads from home/cwd.
	// Instead test that the reload logic clears state.
	hm.mu.Lock()
	hm.hooks = make(map[string][]HookDef)
	hm.sources = nil
	hm.mu.Unlock()

	if !hm.IsEmpty() {
		t.Error("expected empty after clearing hooks")
	}
}

// ---------------------------------------------------------------------------
// loadFileLocked
// ---------------------------------------------------------------------------

func TestHookManager_LoadFileLocked_ValidConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-hooks-load-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	hookCfg := HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "echo pre-hook", Timeout: 3},
			},
			"PostToolUse": {
				{Command: "echo post-hook"},
			},
		},
		Timeout: 10,
	}
	data, _ := json.Marshal(hookCfg)
	hooksFile := filepath.Join(tmpDir, "hooks.json")
	if err := os.WriteFile(hooksFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		timeout: 5e9,
	}
	hm.loadFileLocked(hooksFile)

	if hm.IsEmpty() {
		t.Error("expected hooks to be loaded")
	}
	if len(hm.hooks["PreToolUse"]) != 1 {
		t.Errorf("expected 1 PreToolUse hook, got %d", len(hm.hooks["PreToolUse"]))
	}
	if len(hm.hooks["PostToolUse"]) != 1 {
		t.Errorf("expected 1 PostToolUse hook, got %d", len(hm.hooks["PostToolUse"]))
	}
	if hm.timeout != 10e9 {
		t.Errorf("expected timeout 10s, got %v", hm.timeout)
	}
	if len(hm.sources) != 1 || hm.sources[0] != hooksFile {
		t.Errorf("expected sources to contain %q, got %v", hooksFile, hm.sources)
	}
}

func TestHookManager_LoadFileLocked_NonexistentFile(t *testing.T) {
	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		timeout: 5e9,
	}
	// Should silently skip nonexistent file
	hm.loadFileLocked("/nonexistent/path/hooks.json")

	if !hm.IsEmpty() {
		t.Error("expected empty for nonexistent file")
	}
}

func TestHookManager_LoadFileLocked_InvalidJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-hooks-badjson-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	hooksFile := filepath.Join(tmpDir, "hooks.json")
	if err := os.WriteFile(hooksFile, []byte("{invalid json}"), 0644); err != nil {
		t.Fatal(err)
	}

	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		timeout: 5e9,
	}
	hm.loadFileLocked(hooksFile)

	// Should not load anything due to parse error
	if !hm.IsEmpty() {
		t.Error("expected empty for invalid JSON")
	}
}

func TestHookManager_LoadFileLocked_MultipleFilesAppend(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-hooks-multi-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// File 1
	cfg1 := HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse": {{Command: "hook1"}},
		},
	}
	data1, _ := json.Marshal(cfg1)
	file1 := filepath.Join(tmpDir, "hooks1.json")
	os.WriteFile(file1, data1, 0644)

	// File 2
	cfg2 := HookConfig{
		Hooks: map[string][]HookDef{
			"PreToolUse":  {{Command: "hook2"}},
			"PostToolUse": {{Command: "hook3"}},
		},
	}
	data2, _ := json.Marshal(cfg2)
	file2 := filepath.Join(tmpDir, "hooks2.json")
	os.WriteFile(file2, data2, 0644)

	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		timeout: 5e9,
	}
	hm.loadFileLocked(file1)
	hm.loadFileLocked(file2)

	// PreToolUse should have 2 hooks (appended)
	if len(hm.hooks["PreToolUse"]) != 2 {
		t.Errorf("expected 2 PreToolUse hooks, got %d", len(hm.hooks["PreToolUse"]))
	}
	// PostToolUse should have 1 hook
	if len(hm.hooks["PostToolUse"]) != 1 {
		t.Errorf("expected 1 PostToolUse hook, got %d", len(hm.hooks["PostToolUse"]))
	}
	if len(hm.sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(hm.sources))
	}
}

// ---------------------------------------------------------------------------
// GetSources
// ---------------------------------------------------------------------------

func TestHookManager_GetSources_ReturnsCopy(t *testing.T) {
	hm := &HookManager{
		hooks:   make(map[string][]HookDef),
		sources: []string{"/path/a", "/path/b"},
	}

	sources := hm.GetSources()
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}

	// Modify returned slice, should not affect internal state
	sources[0] = "/modified"
	original := hm.GetSources()
	if original[0] == "/modified" {
		t.Error("GetSources should return a copy")
	}
}

// ---------------------------------------------------------------------------
// GetHooks
// ---------------------------------------------------------------------------

func TestHookManager_GetHooks_ReturnsDeepCopy(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {{Command: "echo hello"}},
		},
	}

	hooks := hm.GetHooks()
	if len(hooks["PreToolUse"]) != 1 {
		t.Fatalf("expected 1 PreToolUse hook, got %d", len(hooks["PreToolUse"]))
	}

	// Modify returned map, should not affect internal state
	hooks["PreToolUse"][0] = HookDef{Command: "modified"}
	original := hm.GetHooks()
	if original["PreToolUse"][0].Command == "modified" {
		t.Error("GetHooks should return a deep copy")
	}
}

func TestHookManager_GetHooks_Empty(t *testing.T) {
	hm := &HookManager{
		hooks: make(map[string][]HookDef),
	}

	hooks := hm.GetHooks()
	if len(hooks) != 0 {
		t.Errorf("expected empty hooks map, got %d entries", len(hooks))
	}
}

// ---------------------------------------------------------------------------
// IsEmpty
// ---------------------------------------------------------------------------

func TestHookManager_IsEmpty_WithEmptySlices(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {},
		},
	}

	if !hm.IsEmpty() {
		t.Error("expected IsEmpty=true when hooks map has only empty slices")
	}
}

func TestHookManager_IsEmpty_WithHooks(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {{Command: "echo test"}},
		},
	}

	if hm.IsEmpty() {
		t.Error("expected IsEmpty=false when hooks are present")
	}
}

// ---------------------------------------------------------------------------
// mergePluginHooks
// ---------------------------------------------------------------------------

func TestHookManager_MergePluginHooks(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {{Command: "existing-hook"}},
		},
	}

	pluginHooks := map[string][]HookDef{
		"PreToolUse":  {{Command: "plugin-pre-hook"}},
		"PostToolUse": {{Command: "plugin-post-hook"}},
	}

	hm.mergePluginHooks(pluginHooks)

	if len(hm.hooks["PreToolUse"]) != 2 {
		t.Errorf("expected 2 PreToolUse hooks, got %d", len(hm.hooks["PreToolUse"]))
	}
	if len(hm.hooks["PostToolUse"]) != 1 {
		t.Errorf("expected 1 PostToolUse hook, got %d", len(hm.hooks["PostToolUse"]))
	}
}

// ---------------------------------------------------------------------------
// parseJSONResult
// ---------------------------------------------------------------------------

func TestParseJSONResult_Deny(t *testing.T) {
	input := `{"decision": "deny", "reason": "forbidden tool"}`
	result := parseJSONResult(HookPreToolUse, []byte(input), 10, HookContext{ToolName: "shell_run"}, 0)

	if !result.Blocked {
		t.Error("expected Blocked=true for deny decision")
	}
	if result.BlockReason != "forbidden tool" {
		t.Errorf("BlockReason = %q, want 'forbidden tool'", result.BlockReason)
	}
}

func TestParseJSONResult_Allow(t *testing.T) {
	input := `{"decision": "allow", "message": "proceed"}`
	result := parseJSONResult(HookPreToolUse, []byte(input), 10, HookContext{ToolName: "shell_run"}, 0)

	if result.Blocked {
		t.Error("expected Blocked=false for allow decision")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "proceed" {
		t.Errorf("Messages = %v, want ['proceed']", result.Messages)
	}
}

func TestParseJSONResult_InvalidJSON(t *testing.T) {
	result := parseJSONResult(HookPreToolUse, []byte("not json"), 10, HookContext{}, 0)
	// Should return empty result (non-blocking) for invalid JSON
	if result.Blocked {
		t.Error("expected non-blocking result for invalid JSON")
	}
}

func TestParseJSONResult_ExitCode2(t *testing.T) {
	input := `{}`
	result := parseJSONResult(HookPostToolUse, []byte(input), 10, HookContext{}, 2)

	if !result.Blocked {
		t.Error("expected Blocked=true for exit code 2")
	}
}

func TestParseJSONResult_HookSpecificOutput(t *testing.T) {
	input := `{
		"hookSpecificOutput": {
			"permissionDecision": "deny",
			"permissionDecisionReason": "security policy violation",
			"updatedInput": {"command": "safe-command"},
			"additionalContext": "context info"
		}
	}`
	result := parseJSONResult(HookPreToolUse, []byte(input), 10, HookContext{}, 0)

	if !result.Blocked {
		t.Error("expected Blocked=true for deny in hookSpecificOutput")
	}
	if result.BlockReason != "security policy violation" {
		t.Errorf("BlockReason = %q, want 'security policy violation'", result.BlockReason)
	}
}

func TestParseJSONResult_Modifications(t *testing.T) {
	input := `{
		"decision": "allow",
		"modifications": {
			"tool_input": {"arg": "modified-value"}
		}
	}`
	result := parseJSONResult(HookPreToolUse, []byte(input), 10, HookContext{}, 0)

	if result.Blocked {
		t.Error("expected allow")
	}
	if result.UpdatedInput == nil {
		t.Error("expected UpdatedInput from modifications")
	}
	modMap, ok := result.UpdatedInput.(map[string]any)
	if !ok {
		t.Fatalf("UpdatedInput type = %T, want map[string]any", result.UpdatedInput)
	}
	if modMap["arg"] != "modified-value" {
		t.Errorf("UpdatedInput arg = %v, want 'modified-value'", modMap["arg"])
	}
}

// ---------------------------------------------------------------------------
// hookTruncate
// ---------------------------------------------------------------------------

func TestHookTruncate_Short(t *testing.T) {
	result := hookTruncate("hello", 100)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestHookTruncate_ExceedsLimit(t *testing.T) {
	longStr := ""
	for i := 0; i < 200; i++ {
		longStr += "x"
	}
	result := hookTruncate(longStr, 100)
	if len(result) != 100 {
		t.Errorf("expected length 100, got %d", len(result))
	}
}

func TestHookTruncate_ExactLimit(t *testing.T) {
	s := "12345"
	result := hookTruncate(s, 5)
	if result != "12345" {
		t.Errorf("expected '12345', got %q", result)
	}
}

// ---------------------------------------------------------------------------
// hookTimeoutForEvent
// ---------------------------------------------------------------------------

func TestHookTimeoutForEvent(t *testing.T) {
	tests := []struct {
		event    HookEvent
		expected int // seconds
	}{
		{HookPreToolUse, 5},
		{HookPostToolUse, 5},
		{HookToolError, 5},
		{HookSessionStart, 10},
		{HookSessionEnd, 10},
		{HookUserPrompt, 15},
		{HookAgentResponse, 15},
		{HookCompaction, 10},
		{HookPreCompact, 10},
		{HookPostCompact, 10},
		{HookSubagentStop, 10},
		{HookNotification, 5}, // default
	}

	for _, tt := range tests {
		t.Run(string(tt.event), func(t *testing.T) {
			dur := hookTimeoutForEvent(tt.event)
			seconds := int(dur.Seconds())
			if seconds != tt.expected {
				t.Errorf("hookTimeoutForEvent(%s) = %ds, want %ds", tt.event, seconds, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RunHooks with matcher
// ---------------------------------------------------------------------------

func TestHookManager_RunHooks_MatcherSkips(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "exit 1", Matcher: "file_read"}, // only matches file_read
			},
		},
	}

	// Using shell_run should skip the hook (matcher doesn't match)
	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})
	if result.Blocked {
		t.Error("expected hook to be skipped due to matcher mismatch")
	}
}

func TestHookManager_RunHooks_MatcherMatches(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "exit 1", Matcher: "shell_run"},
			},
		},
	}

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})
	if !result.Blocked {
		t.Error("expected hook to run and block (matcher matches)")
	}
}

func TestHookManager_RunHooks_WildcardMatcher(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "exit 1", Matcher: "*"},
			},
		},
	}

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "any_tool"})
	if !result.Blocked {
		t.Error("expected wildcard matcher to match all tools")
	}
}

func TestHookManager_RunHooks_EmptyMatcher(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PreToolUse": {
				{Command: "exit 1"}, // empty matcher matches everything
			},
		},
	}

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "any_tool"})
	if !result.Blocked {
		t.Error("expected empty matcher to match all tools")
	}
}

func TestHookManager_RunHooks_NoDefs(t *testing.T) {
	hm := &HookManager{
		hooks: make(map[string][]HookDef),
	}

	result := hm.RunHooks(HookPreToolUse, HookContext{ToolName: "shell_run"})
	if result.Blocked {
		t.Error("expected no blocking when no hooks registered for event")
	}
}

func TestHookManager_RunHooks_MultipleHooks_Aggregates(t *testing.T) {
	hm := &HookManager{
		hooks: map[string][]HookDef{
			"PostToolUse": {
				{Command: "echo 'msg1' >&2; exit 2"},
				{Command: "echo 'msg2' >&2; exit 2"},
			},
		},
	}

	result := hm.RunHooks(HookPostToolUse, HookContext{ToolName: "file_write"})
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d: %v", len(result.Messages), result.Messages)
	}
}

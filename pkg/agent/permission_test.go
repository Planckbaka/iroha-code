package agent

import (
	"testing"
)

func TestBashSecurityValidator(t *testing.T) {
	v := NewBashSecurityValidator()

	tests := []struct {
		name     string
		command  string
		expected bool // true if it should fail validation
	}{
		{"Safe Echo", "echo 'hello world'", false},
		{"Safe Cat", "cat pkg/agent/permission.go", false},
		{"Sudo Command", "sudo apt-get install git", true},
		{"Rm Rf Direct", "rm -rf /", true},
		{"Rm R Direct", "rm -r pkg", true},
		{"Shell Metacharacter Semicolon", "echo 'a'; rm -rf /", true},
		{"Shell Metacharacter Pipe", "cat file | grep password", true},
		{"Command Substitution", "echo $(whoami)", true},
		{"IFS Injection", "IFS=;echo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := v.Validate(tt.command)
			hasFailures := len(failures) > 0
			if hasFailures != tt.expected {
				t.Errorf("Validate(%q) got failures = %v (%d failures), expected failure: %t", tt.command, failures, len(failures), tt.expected)
			}
		})
	}
}

func TestPermissionManagerModes(t *testing.T) {
	// 1. Default Mode Tests
	t.Run("Default Mode Pipeline", func(t *testing.T) {
		pm := NewPermissionManager(ModeDefault)

		// File read is allowed by default rule: {Tool: "file_read", Path: "*", Behavior: "allow"}
		decision, reason := pm.Check("file_read", FileReadArgs{Path: "main.go"})
		if decision != "allow" {
			t.Errorf("Expected 'allow' for file_read under default mode, got %q (reason: %q)", decision, reason)
		}

		// Shell run with safe command has no default allow/deny rule -> should ask
		decision, reason = pm.Check("shell_run", ShellRunArgs{Command: "go test ./..."})
		if decision != "ask" {
			t.Errorf("Expected 'ask' for safe shell_run, got %q (reason: %q)", decision, reason)
		}

		// Shell run with 'rm -rf /' matches default deny rule
		decision, reason = pm.Check("shell_run", ShellRunArgs{Command: "rm -rf /"})
		if decision != "deny" {
			t.Errorf("Expected 'deny' for rm -rf /, got %q (reason: %q)", decision, reason)
		}

		// Shell run with 'sudo' flags validator severe pattern -> deny
		decision, reason = pm.Check("shell_run", ShellRunArgs{Command: "sudo rm -rf"})
		if decision != "deny" {
			t.Errorf("Expected 'deny' for sudo, got %q (reason: %q)", decision, reason)
		}
	})

	// 2. Plan Mode Tests
	t.Run("Plan Mode Restrictions", func(t *testing.T) {
		pm := NewPermissionManager(ModePlan)

		// File read should follow normal allow rule -> allow
		decision, reason := pm.Check("file_read", FileReadArgs{Path: "main.go"})
		if decision != "allow" {
			t.Errorf("Expected 'allow' for file_read in Plan mode, got %q (reason: %q)", decision, reason)
		}

		// File write is a write tool -> blocked immediately in Plan mode
		decision, reason = pm.Check("file_write", FileWriteArgs{Path: "out.go", Content: "test"})
		if decision != "deny" {
			t.Errorf("Expected 'deny' for file_write in Plan mode, got %q (reason: %q)", decision, reason)
		}

		// Shell run is a write tool -> blocked immediately in Plan mode
		decision, reason = pm.Check("shell_run", ShellRunArgs{Command: "go test ./..."})
		if decision != "deny" {
			t.Errorf("Expected 'deny' for shell_run in Plan mode, got %q (reason: %q)", decision, reason)
		}
	})

	// 3. Auto Mode Tests (Phase 2: uses 4-tier risk classifier)
	t.Run("Auto Mode Permissions", func(t *testing.T) {
		pm := NewPermissionManager(ModeAuto)

		// TierTrusted: File read is a read-only tool -> auto-approved
		decision, reason := pm.Check("file_read", FileReadArgs{Path: "main.go"})
		if decision != "allow" {
			t.Errorf("Expected 'allow' for file_read in Auto mode, got %q (reason: %q)", decision, reason)
		}

		// TierTrusted: Todo is a known safe tool -> auto-approved
		decision, reason = pm.Check("todo", nil)
		if decision != "allow" {
			t.Errorf("Expected 'allow' for todo in Auto mode, got %q (reason: %q)", decision, reason)
		}

		// TierLowRisk: File write is low-risk -> auto-approved with logging
		decision, reason = pm.Check("file_write", FileWriteArgs{Path: "out.go", Content: "test"})
		if decision != "allow" {
			t.Errorf("Expected 'allow' for file_write in Auto mode (low_risk tier), got %q (reason: %q)", decision, reason)
		}

		// TierTrusted: shell_run with trusted command (ls) -> auto-approved
		decision, reason = pm.Check("shell_run", ShellRunArgs{Command: "ls -la"})
		if decision != "allow" {
			t.Errorf("Expected 'allow' for ls in Auto mode, got %q (reason: %q)", decision, reason)
		}

		// TierHighRisk: shell_run with dangerous command (rm) -> deny (caught by security validator)
		decision, reason = pm.Check("shell_run", ShellRunArgs{Command: "rm -rf /"})
		if decision != "deny" {
			t.Errorf("Expected 'deny' for rm -rf / in Auto mode, got %q (reason: %q)", decision, reason)
		}

		// TierHighRisk: unknown tool -> ask human
		decision, reason = pm.Check("unknown_tool", nil)
		if decision != "ask" {
			t.Errorf("Expected 'ask' for unknown tool in Auto mode, got %q (reason: %q)", decision, reason)
		}
	})
}

func TestDynamicRulesAndAlwaysAllow(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// A file_write normally asks because there is no matching rule in the default list
	decision, reason := pm.Check("file_write", FileWriteArgs{Path: "main.go", Content: "pkg"})
	if decision != "ask" {
		t.Errorf("Expected initial check for file_write to ask, got %q (reason: %q)", decision, reason)
	}

	// Dynamically add a temporary allow rule (simulating "always" option)
	pm.AddRule(PermissionRule{
		Tool:     "file_write",
		Behavior: "allow",
		Path:     "*",
	})

	// Now check again, should be approved dynamically
	decision, reason = pm.Check("file_write", FileWriteArgs{Path: "main.go", Content: "pkg"})
	if decision != "allow" {
		t.Errorf("Expected subsequent check for file_write to be allowed, got %q (reason: %q)", decision, reason)
	}
}

func TestDenialCountersAndCircuitBreaker(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	if pm.ConsecutiveDenials() != 0 {
		t.Errorf("Expected initial denials to be 0, got %d", pm.ConsecutiveDenials())
	}

	// Note a denial
	d1 := pm.NoteDenial()
	if d1 != 1 || pm.ConsecutiveDenials() != 1 {
		t.Errorf("Expected denials to be 1, got d1=%d consecutive=%d", d1, pm.ConsecutiveDenials())
	}

	// Note another denial
	d2 := pm.NoteDenial()
	if d2 != 2 || pm.ConsecutiveDenials() != 2 {
		t.Errorf("Expected denials to be 2, got d2=%d consecutive=%d", d2, pm.ConsecutiveDenials())
	}

	// An approval should reset it
	pm.NoteApproval()
	if pm.ConsecutiveDenials() != 0 {
		t.Errorf("Expected approval to reset denials, got %d", pm.ConsecutiveDenials())
	}
}

func TestEnhancedBashSecurityValidator(t *testing.T) {
	v := NewBashSecurityValidator()

	tests := []struct {
		name     string
		command  string
		expected bool // true if it should fail validation
	}{
		{"Heredoc Pattern", "cat <<EOF\nhello\nEOF", true},
		{"Named Pipe", "mkfifo /tmp/pipe", true},
		{"Proxy Injection", "git -c core.sshCommand=evilCommand fetch", true},
		{"Unsafe Find Pipe", "find . -name '*.log' | while read file; do rm $file; done", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := v.Validate(tt.command)
			hasFailures := len(failures) > 0
			if hasFailures != tt.expected {
				t.Errorf("Validate(%q) got failures = %v (%d failures), expected failure: %t", tt.command, failures, len(failures), tt.expected)
			}
		})
	}
}

func TestMatchesPatternWildcardGlob(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		value    string
		expected bool
	}{
		{"Exact match", "pkg/agent", "pkg/agent", true},
		{"Exact match case insensitive", "pkg/agent", "PKG/AGENT", true},
		{"Wildcard end", "pkg/*", "pkg/agent/permission.go", true},
		{"Wildcard end mismatch", "pkg/*", "other/agent/permission.go", false},
		{"Wildcard start", "*.go", "main.go", true},
		{"Wildcard start mismatch", "*.go", "main.py", false},
		{"Wildcard middle", "pkg/*/agent", "pkg/test/agent", true},
		{"Wildcard middle mismatch", "pkg/*/agent", "pkg/test/other", false},
		{"Multiple wildcards", "pkg/*/agent/*.go", "pkg/test/agent/permission.go", true},
		{"Substring fallback", "agent", "pkg/agent/permission.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPattern(tt.pattern, tt.value)
			if result != tt.expected {
				t.Errorf("matchesPattern(%q, %q) got %t, expected %t", tt.pattern, tt.value, result, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Additional coverage for Check() — map[string]any args, BackgroundRunArgs,
// AcceptEdits mode, non-severe security gate warnings, wildcard mcp__*
// ---------------------------------------------------------------------------

func TestCheck_MapArgs_ShellRun(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Use map[string]any args for shell_run (instead of typed struct)
	decision, _ := pm.Check("shell_run", map[string]any{"command": "go test ./..."})
	if decision != "ask" {
		t.Errorf("Expected 'ask' for safe shell_run with map args, got %q", decision)
	}

	// map args with sudo -> severe -> deny
	decision, reason := pm.Check("shell_run", map[string]any{"command": "sudo apt-get install git"})
	if decision != "deny" {
		t.Errorf("Expected 'deny' for sudo via map args, got %q (reason: %q)", decision, reason)
	}
}

func TestCheck_BackgroundRunArgs(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// BackgroundRunArgs with safe command -> no security pattern -> ask (no matching rule)
	decision, _ := pm.Check("background_run", BackgroundRunArgs{Command: "go build ./..."})
	if decision != "ask" {
		t.Errorf("Expected 'ask' for safe background_run, got %q", decision)
	}

	// BackgroundRunArgs with sudo -> severe security pattern -> deny
	decision, reason := pm.Check("background_run", BackgroundRunArgs{Command: "sudo rm something"})
	if decision != "deny" {
		t.Errorf("Expected 'deny' for sudo background_run, got %q (reason: %q)", decision, reason)
	}

	// BackgroundRunArgs with rm -rf -> severe -> deny
	decision, _ = pm.Check("background_run", BackgroundRunArgs{Command: "rm -rf /tmp/old"})
	if decision != "deny" {
		t.Errorf("Expected 'deny' for rm -rf background_run, got %q", decision)
	}
}

func TestCheck_MapArgs_BackgroundRun(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// map args with background_run and sudo
	decision, _ := pm.Check("background_run", map[string]any{"command": "sudo true"})
	if decision != "deny" {
		t.Errorf("Expected 'deny' for sudo via background_run map args, got %q", decision)
	}
}

func TestCheck_NonSevereSecurityWarning(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Shell metacharacter (pipe) is non-severe -> should "ask" not "deny"
	decision, reason := pm.Check("shell_run", ShellRunArgs{Command: "cat file | grep pattern"})
	if decision != "ask" {
		t.Errorf("Expected 'ask' for non-severe shell metachar, got %q (reason: %q)", decision, reason)
	}
	if reason == "" {
		t.Error("Expected non-empty reason for security gate warning")
	}
}

func TestCheck_AcceptEditsMode(t *testing.T) {
	pm := NewPermissionManager(ModeAcceptEdits)

	// file_write should be auto-approved in acceptEdits mode
	decision, reason := pm.Check("file_write", FileWriteArgs{Path: "test.go", Content: "hello"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_write in acceptEdits mode, got %q (reason: %q)", decision, reason)
	}

	// file_edit should be auto-approved
	decision, _ = pm.Check("file_edit", FileEditArgs{Path: "test.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_edit in acceptEdits mode, got %q", decision)
	}

	// file_delete should be auto-approved (isFileEdit check includes file_delete)
	decision, _ = pm.Check("file_delete", FileWriteArgs{Path: "test.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_delete in acceptEdits mode, got %q", decision)
	}

	// Non-file tool should fall through to normal rules -> file_read has allow rule
	decision, _ = pm.Check("file_read", FileReadArgs{Path: "main.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_read in acceptEdits mode, got %q", decision)
	}
}

func TestCheck_BypassMode(t *testing.T) {
	pm := NewPermissionManager(ModeBypass)

	// In bypass mode, tools are auto-approved. Use a safe command since
	// the security validator runs before mode check.
	decision, reason := pm.Check("shell_run", ShellRunArgs{Command: "echo hello"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' in bypass mode for shell_run, got %q (reason: %q)", decision, reason)
	}

	decision, _ = pm.Check("file_write", FileWriteArgs{Path: "anything.go", Content: "data"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' in bypass mode for file_write, got %q", decision)
	}
}

func TestCheck_MCPWildcardRule(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// mcp__* tools have an "ask" rule by default
	decision, _ := pm.Check("mcp__plugin_tool", nil)
	if decision != "ask" {
		t.Errorf("Expected 'ask' for mcp__plugin_tool in default mode, got %q", decision)
	}
}

func TestCheck_DefaultMode_NoMatchingRule_Ask(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// A tool with no matching rule at all should "ask"
	decision, reason := pm.Check("totally_unknown_tool", nil)
	if decision != "ask" {
		t.Errorf("Expected 'ask' for unknown tool in default mode, got %q (reason: %q)", decision, reason)
	}
}

func TestCheck_PlanMode_WriteTool_Deny(t *testing.T) {
	pm := NewPermissionManager(ModePlan)

	// shell_run is a write tool -> blocked in plan mode
	decision, _ := pm.Check("shell_run", ShellRunArgs{Command: "go test ./..."})
	if decision != "deny" {
		t.Errorf("Expected 'deny' for shell_run in plan mode, got %q", decision)
	}

	// background_run is a write tool -> blocked
	decision, _ = pm.Check("background_run", BackgroundRunArgs{Command: "echo hi"})
	if decision != "deny" {
		t.Errorf("Expected 'deny' for background_run in plan mode, got %q", decision)
	}

	// mcp__ tool is a write tool -> blocked
	decision, _ = pm.Check("mcp__plugin_tool", nil)
	if decision != "deny" {
		t.Errorf("Expected 'deny' for mcp__ tool in plan mode, got %q", decision)
	}
}

func TestCheck_AutoMode_MediumHighRisk_Ask(t *testing.T) {
	pm := NewPermissionManager(ModeAuto)

	// shell_run with curl is high risk -> ask
	decision, _ := pm.Check("shell_run", ShellRunArgs{Command: "curl http://example.com"})
	if decision != "ask" {
		t.Errorf("Expected 'ask' for curl in auto mode, got %q", decision)
	}
}

func TestMatches_AllTypeAssertions(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Test FileEditArgs path matching
	pm.AddRule(PermissionRule{Tool: "file_edit", Path: "specific/path.go", Behavior: "allow"})
	decision, _ := pm.Check("file_edit", FileEditArgs{Path: "specific/path.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_edit with FileEditArgs path match, got %q", decision)
	}

	// Test FileWriteArgs path matching
	pm2 := NewPermissionManager(ModeDefault)
	pm2.AddRule(PermissionRule{Tool: "file_write", Path: "other/path.go", Behavior: "allow"})
	decision, _ = pm2.Check("file_write", FileWriteArgs{Path: "other/path.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_write with FileWriteArgs path match, got %q", decision)
	}

	// Test map args path matching
	pm3 := NewPermissionManager(ModeDefault)
	pm3.AddRule(PermissionRule{Tool: "file_read", Path: "map/path.go", Behavior: "allow"})
	decision, _ = pm3.Check("file_read", map[string]any{"path": "map/path.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_read with map path match, got %q", decision)
	}

	// Test map args command matching
	pm4 := NewPermissionManager(ModeDefault)
	pm4.AddRule(PermissionRule{Tool: "shell_run", Content: "safe_command", Behavior: "allow"})
	decision, _ = pm4.Check("shell_run", map[string]any{"command": "safe_command"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for shell_run with map command match, got %q", decision)
	}

	// Test ShellRunArgs content matching
	pm5 := NewPermissionManager(ModeDefault)
	pm5.AddRule(PermissionRule{Tool: "shell_run", Content: "my_special_cmd", Behavior: "allow"})
	decision, _ = pm5.Check("shell_run", ShellRunArgs{Command: "my_special_cmd"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for shell_run with ShellRunArgs content match, got %q", decision)
	}
}

func TestMatches_ToolNameMismatch(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// A deny rule for a different tool should not match
	decision, _ := pm.Check("file_read", FileReadArgs{Path: "main.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for file_read, got %q", decision)
	}
}

func TestMatches_EmptyPathAndContentRules(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Rule with only Tool field (no path, no content) should match any args
	pm.AddRule(PermissionRule{Tool: "custom_tool", Behavior: "allow"})
	decision, _ := pm.Check("custom_tool", map[string]any{"anything": "value"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' for custom_tool with no path/content rule, got %q", decision)
	}
}

func TestMatchesPattern_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		value    string
		expected bool
	}{
		{"Empty pattern", "", "anything", true},
		{"Empty pattern empty value", "", "", true},
		{"Star pattern", "*", "anything", true},
		{"Exact no wildcard match", "hello", "hello", true},
		{"Exact no wildcard no match", "hello", "world", false},
		{"Single star only", "*", "", true},
		{"Prefix star with empty parts", "a*", "a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPattern(tt.pattern, tt.value)
			if result != tt.expected {
				t.Errorf("matchesPattern(%q, %q) = %t, want %t", tt.pattern, tt.value, result, tt.expected)
			}
		})
	}
}

func TestSetMode_Invalid(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)
	err := pm.SetMode(PermissionMode("invalid_mode"))
	if err == nil {
		t.Error("Expected error for invalid mode")
	}
}

func TestCheck_ConsecutiveDenialsTracking(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Deny rules increment consecutiveDenials
	pm.Check("shell_run", ShellRunArgs{Command: "rm -rf /"})
	if pm.ConsecutiveDenials() != 1 {
		t.Errorf("Expected 1 consecutive denial after deny, got %d", pm.ConsecutiveDenials())
	}

	// Allow rules reset it
	pm.Check("file_read", FileReadArgs{Path: "main.go"})
	if pm.ConsecutiveDenials() != 0 {
		t.Errorf("Expected 0 consecutive denials after allow, got %d", pm.ConsecutiveDenials())
	}

	// Reset works
	pm.NoteDenial()
	pm.NoteDenial()
	pm.ResetConsecutiveDenials()
	if pm.ConsecutiveDenials() != 0 {
		t.Errorf("Expected 0 after reset, got %d", pm.ConsecutiveDenials())
	}
}

// ---------------------------------------------------------------------------
// matches: path mismatch and content mismatch returning false
// ---------------------------------------------------------------------------

func TestMatches_PathMismatch_ReturnsFalse(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Add a deny rule with a specific path that won't match
	pm.AddRule(PermissionRule{Tool: "file_read", Path: "secret/path.go", Behavior: "deny"})

	// file_read with different path -> deny rule doesn't match -> falls through to allow rule
	decision, _ := pm.Check("file_read", FileReadArgs{Path: "other/path.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' when deny rule path doesn't match, got %q", decision)
	}
}

func TestMatches_ContentMismatch_ReturnsFalse(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Add a deny rule with specific content that won't match
	pm.AddRule(PermissionRule{Tool: "shell_run", Content: "specific_dangerous_cmd", Behavior: "deny"})

	// shell_run with different command -> deny rule doesn't match -> ask
	decision, _ := pm.Check("shell_run", ShellRunArgs{Command: "go build ./..."})
	if decision != "ask" {
		t.Errorf("Expected 'ask' when deny rule content doesn't match, got %q", decision)
	}
}

func TestMatches_ToolMismatch_ReturnsFalse(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	// Deny rule for a specific tool
	pm.AddRule(PermissionRule{Tool: "file_write", Path: "specific.go", Behavior: "deny"})

	// Check a different tool with the same path -> rule should not match
	decision, _ := pm.Check("file_read", FileReadArgs{Path: "specific.go"})
	if decision != "allow" {
		t.Errorf("Expected 'allow' when tool name doesn't match deny rule, got %q", decision)
	}
}


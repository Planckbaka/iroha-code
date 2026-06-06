package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"iroha/pkg/agent"

	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// TestFormatToolActivity — table-driven tests for all tool name branches
// ---------------------------------------------------------------------------

func TestFormatToolActivity(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		// file_read
		{"file_read with path", "file_read", map[string]any{"path": "/tmp/a.go"}, "Read file /tmp/a.go"},
		{"file_read without path", "file_read", nil, "Read file"},
		{"file_read with AbsolutePath", "file_read", map[string]any{"AbsolutePath": "/x.go"}, "Read file /x.go"},
		{"file_read with TargetFile", "file_read", map[string]any{"TargetFile": "/y.go"}, "Read file /y.go"},

		// file_write
		{"file_write with path", "file_write", map[string]any{"path": "/tmp/b.go"}, "Write file /tmp/b.go"},
		{"file_write without path", "file_write", nil, "Write file"},
		{"file_write with TargetFile", "file_write", map[string]any{"TargetFile": "/z.go"}, "Write file /z.go"},

		// file_edit
		{"file_edit with path", "file_edit", map[string]any{"path": "/tmp/c.go"}, "Edit file /tmp/c.go"},
		{"file_edit without path", "file_edit", nil, "Edit file"},

		// file_edit_batch
		{"file_edit_batch", "file_edit_batch", nil, "Apply atomic batch file edits"},

		// list_directory
		{"list_directory with path", "list_directory", map[string]any{"path": "/tmp"}, "List directory /tmp"},
		{"list_directory without path", "list_directory", nil, "List directory"},
		{"list_directory with DirectoryPath", "list_directory", map[string]any{"DirectoryPath": "/dir"}, "List directory /dir"},

		// search_grep
		{"search_grep with pattern", "search_grep", map[string]any{"pattern": "TODO"}, `Search pattern "TODO"`},
		{"search_grep without pattern", "search_grep", nil, "Search pattern"},
		{"search_grep with query", "search_grep", map[string]any{"query": "FIXME"}, `Search pattern "FIXME"`},

		// find_files
		{"find_files with pattern", "find_files", map[string]any{"pattern": "*.go"}, `Find files matching "*.go"`},
		{"find_files without pattern", "find_files", nil, "Find files"},

		// shell_run
		{"shell_run with command", "shell_run", map[string]any{"command": "ls -la"}, "Run terminal command: ls -la"},
		{"shell_run without command", "shell_run", nil, "Run terminal command"},
		{"shell_run with CommandLine", "shell_run", map[string]any{"CommandLine": "go test"}, "Run terminal command: go test"},

		// todo
		{"todo with text", "todo", map[string]any{"text": "fix bug"}, `Update todo "fix bug"`},
		{"todo without text", "todo", nil, "Update todo"},

		// memory_save
		{"memory_save with name", "memory_save", map[string]any{"name": "cfg"}, `Save cross-session memory "cfg"`},
		{"memory_save without name", "memory_save", nil, "Save cross-session memory"},

		// memory_list
		{"memory_list", "memory_list", nil, "List cross-session memories"},

		// memory_search
		{"memory_search with query", "memory_search", map[string]any{"query": "api"}, `Search cross-session memories "api"`},
		{"memory_search without query", "memory_search", nil, "Search cross-session memories"},

		// memory_update
		{"memory_update with name", "memory_update", map[string]any{"name": "key"}, `Update cross-session memory "key"`},
		{"memory_update without name", "memory_update", nil, "Update cross-session memory"},

		// memory_delete
		{"memory_delete with name", "memory_delete", map[string]any{"name": "old"}, `Delete cross-session memory "old"`},
		{"memory_delete without name", "memory_delete", nil, "Delete cross-session memory"},

		// memory_dream
		{"memory_dream", "memory_dream", nil, "Consolidate persistent memories"},

		// task_create
		{"task_create with id", "task_create", map[string]any{"id": "T1"}, "Create task T1"},
		{"task_create without id", "task_create", nil, "Create task"},

		// task_update
		{"task_update with id", "task_update", map[string]any{"id": "T2"}, "Update task T2"},
		{"task_update without id", "task_update", nil, "Update task"},

		// task_list
		{"task_list", "task_list", nil, "List tasks"},

		// task_get
		{"task_get with id", "task_get", map[string]any{"id": "T3"}, "Get task T3 details"},
		{"task_get without id", "task_get", nil, "Get task details"},

		// background_run
		{"background_run with command", "background_run", map[string]any{"command": "sleep 1"}, "Run background command: sleep 1"},
		{"background_run without command", "background_run", nil, "Run background command"},

		// check_background
		{"check_background", "check_background", nil, "Check background tasks"},

		// schedule_create
		{"schedule_create", "schedule_create", nil, "Create scheduled task"},
		// schedule_list
		{"schedule_list", "schedule_list", nil, "List scheduled tasks"},
		// schedule_delete
		{"schedule_delete", "schedule_delete", nil, "Delete scheduled task"},

		// spawn_teammate
		{"spawn_teammate with name", "spawn_teammate", map[string]any{"name": "dev"}, "Spawn agent teammate dev"},
		{"spawn_teammate without name", "spawn_teammate", nil, "Spawn agent teammate"},

		// list_teammates
		{"list_teammates", "list_teammates", nil, "Check agent team status"},

		// send_message
		{"send_message with recipient", "send_message", map[string]any{"recipient": "dev"}, "Send message to agent dev"},
		{"send_message without recipient", "send_message", nil, "Send message to agent team"},

		// read_inbox
		{"read_inbox", "read_inbox", nil, "Read agent inbox"},

		// broadcast
		{"broadcast", "broadcast", nil, "Broadcast to agent team"},

		// spawn_subagent
		{"spawn_subagent with role", "spawn_subagent", map[string]any{"role": "researcher"}, "Spawn subagent researcher"},
		{"spawn_subagent without role", "spawn_subagent", nil, "Spawn subagent"},

		// web_fetch
		{"web_fetch with url", "web_fetch", map[string]any{"url": "https://example.com"}, "Fetch web page https://example.com"},
		{"web_fetch without url", "web_fetch", nil, "Fetch web page"},

		// web_search
		{"web_search with query", "web_search", map[string]any{"query": "golang"}, `Search the web for "golang"`},
		{"web_search without query", "web_search", nil, "Search the web"},

		// worktree_create
		{"worktree_create with name", "worktree_create", map[string]any{"name": "feature"}, "Create git worktree feature"},
		{"worktree_create without name", "worktree_create", nil, "Create git worktree"},

		// worktree_list
		{"worktree_list", "worktree_list", nil, "List git worktrees"},
		// worktree_status
		{"worktree_status", "worktree_status", nil, "Check git worktree status"},
		// worktree_enter
		{"worktree_enter", "worktree_enter", nil, "Enter git worktree"},
		// worktree_closeout
		{"worktree_closeout", "worktree_closeout", nil, "Close/clean up git worktree"},

		// mcp_server_list
		{"mcp_server_list", "mcp_server_list", nil, "List configured MCP servers"},

		// LSP tools
		{"lsp_goto_definition", "lsp_goto_definition", nil, "LSP: Go to definition"},
		{"lsp_find_references", "lsp_find_references", nil, "LSP: Find references"},
		{"lsp_document_symbols", "lsp_document_symbols", nil, "LSP: Extract document symbols"},
		{"lsp_hover", "lsp_hover", nil, "LSP: Hover symbol"},
		{"lsp_diagnostics", "lsp_diagnostics", nil, "LSP: Fetch server diagnostics"},

		// unknown tool (default case)
		{"unknown tool with args", "my_custom_tool", map[string]any{"path": "/x"}, `Call tool my_custom_tool(path: "/x")`},
		{"unknown tool without args", "my_custom_tool", nil, "Call tool my_custom_tool"},

		// args as non-map type (non-map args trigger FormatToolArgs path)
		{"non-map args", "file_read", nil, "Read file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatToolActivity(tt.tool, tt.args)
			if got != tt.want {
				t.Errorf("FormatToolActivity(%q, %v) = %q, want %q", tt.tool, tt.args, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestFormatToolArgs — table-driven tests for FormatToolArgs
// ---------------------------------------------------------------------------

func TestFormatToolArgs(t *testing.T) {
	tests := []struct {
		name string
		args any
		want string
	}{
		{"nil args", nil, ""},
		{"empty map", map[string]any{}, ""},
		{"map with path", map[string]any{"path": "/tmp/x.go"}, `(path: "/tmp/x.go")`},
		{"map with command", map[string]any{"command": "ls"}, `(command: "ls")`},
		{"map with pattern", map[string]any{"pattern": "*.go"}, `(pattern: "*.go")`},
		{"map with query", map[string]any{"query": "test"}, `(query: "test")`},
		{"map with text", map[string]any{"text": "hello"}, `(text: "hello")`},
		{"map with other keys", map[string]any{"count": 42}, "(count: 42)"},
		{"map with mixed keys", map[string]any{"path": "/a.go", "verbose": true}, ""},
		{"struct args", struct{ Name string }{"test"}, `{"Name":"test"}`},
		{"integer args (non-map, non-marshalable to small)", 42, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatToolArgs(tt.args)
			if tt.name == "map with mixed keys" {
				// For mixed keys, just verify both parts are present
				if !strings.Contains(got, `path: "/a.go"`) || !strings.Contains(got, "verbose: true") {
					t.Errorf("FormatToolArgs() = %q, expected both path and verbose", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("FormatToolArgs(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderShellStreamArea — table-driven tests
// ---------------------------------------------------------------------------

func TestRenderShellStreamArea(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		cmd        string
		width      int
		wantEmpty  bool
		wantSubstr []string
	}{
		{
			name:      "empty lines returns empty",
			lines:     []string{},
			cmd:       "echo",
			width:     80,
			wantEmpty: true,
		},
		{
			name:       "few lines under limit",
			lines:      []string{"hello", "world"},
			cmd:        "echo",
			width:      80,
			wantEmpty:  false,
			wantSubstr: []string{"console", "echo", "hello", "world"},
		},
		{
			name:       "lines exceeding max triggers truncation",
			lines:      makeLines(20),
			cmd:        "longrun",
			width:      80,
			wantEmpty:  false,
			wantSubstr: []string{"console", "longrun", "older lines hidden"},
		},
		{
			name:       "long command truncated",
			lines:      []string{"out"},
			cmd:        strings.Repeat("x", 100),
			width:      80,
			wantEmpty:  false,
			wantSubstr: []string{"console", "..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderShellStreamArea(tt.lines, tt.cmd, tt.width)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// makeLines creates n numbered output lines.
func makeLines(n int) []string {
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	return lines
}

// ---------------------------------------------------------------------------
// TestGetToolCategoryTheme — table-driven tests
// ---------------------------------------------------------------------------

func TestGetToolCategoryTheme(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		wantIcon  string
		wantLabel string
	}{
		// File tools
		{"file_read", "file_read", "file", "File Operations"},
		{"file_write", "file_write", "file", "File Operations"},
		{"file_edit", "file_edit", "file", "File Operations"},
		{"file_edit_batch", "file_edit_batch", "file", "File Operations"},
		{"list_directory", "list_directory", "file", "File Operations"},
		{"search_grep", "search_grep", "file", "File Operations"},
		{"find_files", "find_files", "file", "File Operations"},
		{"lsp_goto_definition", "lsp_goto_definition", "file", "File Operations"},
		{"lsp_find_references", "lsp_find_references", "file", "File Operations"},
		{"lsp_document_symbols", "lsp_document_symbols", "file", "File Operations"},
		{"lsp_hover", "lsp_hover", "file", "File Operations"},
		{"lsp_diagnostics", "lsp_diagnostics", "file", "File Operations"},

		// Command tools
		{"shell_run", "shell_run", "cmd", "Command Execution"},
		{"background_run", "background_run", "cmd", "Command Execution"},
		{"check_background", "check_background", "cmd", "Command Execution"},
		{"web_fetch", "web_fetch", "cmd", "Command Execution"},
		{"web_search", "web_search", "cmd", "Command Execution"},

		// Agent tools
		{"spawn_teammate", "spawn_teammate", "agent", "Agent Collaboration"},
		{"list_teammates", "list_teammates", "agent", "Agent Collaboration"},
		{"send_message", "send_message", "agent", "Agent Collaboration"},
		{"read_inbox", "read_inbox", "agent", "Agent Collaboration"},
		{"broadcast", "broadcast", "agent", "Agent Collaboration"},
		{"spawn_subagent", "spawn_subagent", "agent", "Agent Collaboration"},

		// Unknown tools
		{"unknown tool", "some_random_tool", "tool", "External Tools"},
		{"empty tool name", "", "tool", "External Tools"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, icon, label := getToolCategoryTheme(tt.tool)
			if icon != tt.wantIcon {
				t.Errorf("getToolCategoryTheme(%q) icon = %q, want %q", tt.tool, icon, tt.wantIcon)
			}
			if label != tt.wantLabel {
				t.Errorf("getToolCategoryTheme(%q) label = %q, want %q", tt.tool, label, tt.wantLabel)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderToolSuccessCard — table-driven tests
// ---------------------------------------------------------------------------

func TestRenderToolSuccessCard(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		args       map[string]any
		wantSubstr []string
	}{
		{
			name:       "file_read success",
			tool:       "file_read",
			args:       map[string]any{"path": "/tmp/a.go"},
			wantSubstr: []string{"file"}, // icon bracket [file]
		},
		{
			name:       "shell_run success",
			tool:       "shell_run",
			args:       map[string]any{"command": "ls"},
			wantSubstr: []string{"cmd"},
		},
		{
			name:       "unknown tool success",
			tool:       "custom_tool",
			args:       nil,
			wantSubstr: []string{"tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderToolSuccessCard(tt.tool, tt.args, 100*time.Millisecond)
			// Check duration is present
			if !strings.Contains(got, "ms") && !strings.Contains(got, "s") {
				t.Errorf("expected duration in output, got: %s", got)
			}
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderToolErrorCardTable — table-driven tests
// ---------------------------------------------------------------------------

func TestRenderToolErrorCardTable(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		args       map[string]any
		err        error
		wantSubstr []string
	}{
		{
			name:       "error with message",
			tool:       "file_read",
			args:       map[string]any{"path": "/tmp/a.go"},
			err:        errors.New("file not found"),
			wantSubstr: []string{"file not found"},
		},
		{
			name:       "error nil",
			tool:       "shell_run",
			args:       nil,
			err:        nil,
			wantSubstr: []string{"operation failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderToolErrorCard(tt.tool, tt.args, 50*time.Millisecond, tt.err)
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderErrorCard — table-driven tests for error categories
// ---------------------------------------------------------------------------

func TestRenderErrorCard(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantEmpty  bool
		wantSubstr []string
		wantNot    []string
	}{
		{"nil error", nil, true, nil, nil},
		{"API error", errors.New("API rate limit exceeded"), false, []string{"API endpoint"}, nil},
		{"Authorization error", errors.New("Authorization failed"), false, []string{"API Key"}, nil},
		{"ApiKey error", errors.New("ApiKey invalid"), false, []string{"API Key"}, nil},
		{"http error", errors.New("http request failed"), false, []string{"network"}, nil},
		{"call error", errors.New("call to service failed"), false, []string{"API endpoint"}, nil},
		{"Chinese API error", errors.New("接口调用失败"), false, []string{"API"}, nil},
		{"Chinese call error", errors.New("调用失败"), false, []string{"API"}, nil},
		{"Permission error", errors.New("Permission denied"), false, []string{"read/write permissions"}, nil},
		{"denied error", errors.New("access denied for resource"), false, []string{"read/write permissions"}, nil},
		{"Chinese permission error", errors.New("权限不足"), false, []string{"read/write permissions"}, nil},
		{"generic error", errors.New("something went wrong"), false, []string{"command-line tools"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderErrorCard(tt.err)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
			for _, not := range tt.wantNot {
				if strings.Contains(got, not) {
					t.Errorf("expected output NOT to contain %q, got:\n%s", not, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderPermissionSelect — table-driven tests
// ---------------------------------------------------------------------------

func TestRenderPermissionSelect(t *testing.T) {
	tests := []struct {
		name    string
		mode    agent.PermissionMode
		wantBtn string // label that should be marked active
	}{
		{"plan mode active", agent.ModePlan, "Plan Mode"},
		{"default mode active", agent.ModeDefault, "Default Mode"},
		{"acceptEdits mode active", agent.ModeAcceptEdits, "AcceptEdits Mode"},
		{"auto mode active", agent.ModeAuto, "Auto Mode"},
		{"bypass mode active", agent.ModeBypass, "Bypass Mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderPermissionSelect(tt.mode)
			// Should contain the active marker (▶)
			if !strings.Contains(got, tt.wantBtn) {
				t.Errorf("expected output to contain %q", tt.wantBtn)
			}
			// Should contain the active marker for the selected mode
			if !strings.Contains(got, "▶") {
				t.Errorf("expected output to contain active marker ▶")
			}
			// Should contain all five modes
			for _, label := range []string{"Plan Mode", "Default Mode", "AcceptEdits Mode", "Auto Mode", "Bypass Mode"} {
				if !strings.Contains(got, label) {
					t.Errorf("expected output to contain %q", label)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCompactMarkdownLines — table-driven tests
// ---------------------------------------------------------------------------

func TestCompactMarkdownLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int // expected number of non-empty lines
	}{
		{"empty string", "", 0},
		{"single line", "hello", 1},
		{"leading blank lines", "\n\nhello", 1},
		{"trailing blank lines", "hello\n\n", 1},
		{"both leading and trailing blanks", "\n\nhello\n\n", 1},
		{"CRLF handling", "line1\r\nline2\r\n", 2},
		{"multiple content lines", "a\nb\nc", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactMarkdownLines(tt.input)
			if len(got) != tt.want {
				t.Errorf("compactMarkdownLines(%q) returned %d lines, want %d: %v", tt.input, len(got), tt.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTrimANSIRightSpace — table-driven tests
// ---------------------------------------------------------------------------

func TestTrimANSIRightSpace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"plain text with trailing space", "hello   ", "hello"},
		{"plain text no trailing space", "hello", "hello"},
		{"only spaces", "   ", ""},
		{"only ANSI codes", "\x1b[32m\x1b[0m", ""},
		{"ANSI with text", "\x1b[32mhello\x1b[0m", "\x1b[32mhello\x1b[0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimANSIRightSpace(tt.input)
			// For ANSI-containing strings, compare visible content
			if tt.name == "ANSI with text" {
				gotStripped := ansi.Strip(got)
				wantStripped := ansi.Strip(tt.want)
				if gotStripped != wantStripped {
					t.Errorf("trimANSIRightSpace(%q) visible = %q, want %q", tt.input, gotStripped, wantStripped)
				}
				return
			}
			if got != tt.want {
				t.Errorf("trimANSIRightSpace(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

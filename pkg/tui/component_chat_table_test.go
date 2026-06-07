package tui

import (
	"strings"
	"testing"

	"iroha/pkg/agent"
)

// ---------------------------------------------------------------------------
// TestChatComponentOnStateChange
// ---------------------------------------------------------------------------

func TestChatComponentOnStateChange(t *testing.T) {
	tests := []struct {
		name     string
		oldState TuiState
		newState TuiState
	}{
		{"prompt to thinking", statePrompt, stateThinking},
		{"thinking to streaming", stateThinking, stateStreaming},
		{"streaming to confirming", stateStreaming, stateConfirming},
		{"confirming to prompt", stateConfirming, statePrompt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChatComponent(nil)
			// OnStateChange is a no-op for ChatComponent — just ensure no panic
			c.OnStateChange(tt.oldState, tt.newState)
		})
	}
}

// ---------------------------------------------------------------------------
// TestChatComponentRenderTailTable
// ---------------------------------------------------------------------------

func TestChatComponentRenderTailTable(t *testing.T) {
	tests := []struct {
		name         string
		state        TuiState
		streamText   string
		streamRender string
		welcomeLines []string
		confirmLines []string
		wantContains []string
		wantMinLines int
	}{
		{
			name:         "statePrompt with welcome lines",
			state:        statePrompt,
			welcomeLines: []string{"Welcome to Iroha!"},
			wantMinLines: 1,
			wantContains: []string{"Welcome to Iroha!"},
		},
		{
			name:         "statePrompt without welcome lines",
			state:        statePrompt,
			welcomeLines: nil,
			wantMinLines: 0,
		},
		{
			name:         "stateThinking without active tool",
			state:        stateThinking,
			wantMinLines: 1,
			wantContains: []string{"thinking"},
		},
		{
			name:         "stateThinking with active tool",
			state:        stateThinking,
			wantMinLines: 1,
		},
		{
			name:         "stateStreaming with rendered text",
			state:        stateStreaming,
			streamRender: "Hello from agent",
			wantMinLines: 1,
		},
		{
			name:         "stateStreaming with stream text",
			state:        stateStreaming,
			streamText:   "Raw stream text",
			wantMinLines: 1,
		},
		{
			name:         "stateConfirming with confirm lines",
			state:        stateConfirming,
			confirmLines: []string{"Allow this action?", "[Y] Yes [N] No"},
			wantMinLines: 2,
			wantContains: []string{"Allow this action?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChatComponent(nil)
			c.state = tt.state

			// Set active tool for thinking-with-tool test
			if tt.name == "stateThinking with active tool" {
				c.SetActiveTool(agent.ToolStatus{
					Name:    "file_read",
					Running: true,
					Args:    map[string]any{"path": "/tmp/test.go"},
				})
			}

			lines := c.RenderTail(tt.state, 80, tt.streamText, tt.streamRender, tt.welcomeLines, tt.confirmLines)

			if len(lines) < tt.wantMinLines {
				t.Errorf("got %d lines, want at least %d: %v", len(lines), tt.wantMinLines, lines)
			}
			if tt.wantContains != nil {
				joined := strings.Join(lines, "\n")
				for _, substr := range tt.wantContains {
					if !strings.Contains(joined, substr) {
						t.Errorf("expected output to contain %q, got:\n%s", substr, joined)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestChatComponentRenderThinking
// ---------------------------------------------------------------------------

func TestChatComponentRenderThinking(t *testing.T) {
	tests := []struct {
		name         string
		activeTool   agent.ToolStatus
		wantContains []string
		wantLen      int
	}{
		{
			name:         "without active tool shows thinking indicator",
			activeTool:   agent.ToolStatus{},
			wantLen:      2, // empty line + thinking line
			wantContains: []string{"thinking"},
		},
		{
			name: "with active tool delegates to renderToolProgress",
			activeTool: agent.ToolStatus{
				Name:    "shell_run",
				Running: true,
				Args:    map[string]any{"command": "go test"},
			},
			wantContains: []string{"cmd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChatComponent(nil)
			c.activeTool = tt.activeTool

			lines := c.renderThinking(80)

			if tt.wantLen > 0 && len(lines) != tt.wantLen {
				t.Errorf("got %d lines, want %d: %v", len(lines), tt.wantLen, lines)
			}
			if tt.wantContains != nil {
				joined := strings.Join(lines, "\n")
				for _, substr := range tt.wantContains {
					if !strings.Contains(joined, substr) {
						t.Errorf("expected to contain %q, got:\n%s", substr, joined)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestChatComponentRenderToolProgress
// ---------------------------------------------------------------------------

func TestChatComponentRenderToolProgress(t *testing.T) {
	tests := []struct {
		name         string
		activeTool   agent.ToolStatus
		wantNil      bool
		wantContains []string
	}{
		{
			name:       "no running tool returns nil",
			activeTool: agent.ToolStatus{},
			wantNil:    true,
		},
		{
			name: "running tool without stream lines",
			activeTool: agent.ToolStatus{
				Name:    "file_read",
				Running: true,
				Args:    map[string]any{"path": "/tmp/a.go"},
			},
			wantNil:      false,
			wantContains: []string{"file"},
		},
		{
			name: "running tool with stream lines and command args",
			activeTool: agent.ToolStatus{
				Name:        "shell_run",
				Running:     true,
				StreamLines: []string{"line 1", "line 2"},
				Args:        map[string]any{"command": "echo hello"},
			},
			wantNil:      false,
			wantContains: []string{"cmd"},
		},
		{
			name: "running tool with stream lines but no command",
			activeTool: agent.ToolStatus{
				Name:        "shell_run",
				Running:     true,
				StreamLines: []string{"output"},
				Args:        nil,
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChatComponent(nil)
			c.activeTool = tt.activeTool

			lines := c.renderToolProgress(80)

			if tt.wantNil {
				if lines != nil {
					t.Errorf("expected nil, got %v", lines)
				}
				return
			}

			if len(lines) == 0 {
				t.Error("expected non-empty lines")
			}
			if tt.wantContains != nil {
				joined := strings.Join(lines, "\n")
				for _, substr := range tt.wantContains {
					if !strings.Contains(joined, substr) {
						t.Errorf("expected to contain %q, got:\n%s", substr, joined)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestChatComponentRenderTable
// ---------------------------------------------------------------------------

func TestChatComponentRenderTable(t *testing.T) {
	tests := []struct {
		name       string
		state      TuiState
		history    *HistoryStore
		activeTool agent.ToolStatus
		streamText string
		wantMinLen int
	}{
		{
			name:       "empty with no history",
			state:      statePrompt,
			history:    nil,
			wantMinLen: 0,
		},
		{
			name:  "with history renders entries",
			state: statePrompt,
			history: func() *HistoryStore {
				h := NewHistoryStore()
				h.Add(HistoryEntry{Role: RoleUser, Content: "hello"})
				return h
			}(),
			wantMinLen: 1,
		},
		{
			name:  "stateThinking without active tool",
			state: stateThinking,
			history: func() *HistoryStore {
				h := NewHistoryStore()
				h.Add(HistoryEntry{Role: RoleUser, Content: "test"})
				return h
			}(),
			wantMinLen: 1,
		},
		{
			name:  "stateThinking with active tool",
			state: stateThinking,
			activeTool: agent.ToolStatus{
				Name:    "file_read",
				Running: true,
			},
			history:    NewHistoryStore(),
			wantMinLen: 1,
		},
		{
			name:  "stateStreaming with streamed text",
			state: stateStreaming,
			history: func() *HistoryStore {
				h := NewHistoryStore()
				h.Add(HistoryEntry{Role: RoleUser, Content: "prompt"})
				return h
			}(),
			streamText: "Agent response here",
			wantMinLen: 1,
		},
		{
			name:       "stateConfirming renders nothing extra",
			state:      stateConfirming,
			history:    NewHistoryStore(),
			wantMinLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChatComponent(tt.history)
			c.state = tt.state
			c.activeTool = tt.activeTool
			c.streamedText = tt.streamText

			lines := c.Render(80)

			if len(lines) < tt.wantMinLen {
				t.Errorf("got %d lines, want at least %d: %v", len(lines), tt.wantMinLen, lines)
			}
		})
	}
}

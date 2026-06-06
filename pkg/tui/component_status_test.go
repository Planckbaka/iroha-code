package tui

import (
	"strings"
	"testing"
	"time"

	"iroha/pkg/agent"
)

// ---------------------------------------------------------------------------
// StatusBarComponent — table-driven tests
// ---------------------------------------------------------------------------

func TestNewStatusBarComponent(t *testing.T) {
	sb := NewStatusBarComponent()
	if sb == nil {
		t.Fatal("NewStatusBarComponent returned nil")
	}
}

func TestStatusBarActiveAlwaysTrue(t *testing.T) {
	sb := NewStatusBarComponent()
	tests := []struct {
		name  string
		state TuiState
	}{
		{"prompt", statePrompt},
		{"thinking", stateThinking},
		{"streaming", stateStreaming},
		{"confirming", stateConfirming},
		{"permission", statePermissionSelect},
		{"session", stateSessionSelect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !sb.Active(tt.state) {
				t.Errorf("Active(%v) = false, want true", tt.state)
			}
		})
	}
}

func TestStatusBarHandleInputAlwaysFalse(t *testing.T) {
	sb := NewStatusBarComponent()
	keys := []Key{
		{Type: KeyEnter},
		{Type: KeyRune, Rune: 'a'},
		{Type: KeyUp},
		{Type: KeyDown},
	}
	for _, k := range keys {
		if sb.HandleInput(k) {
			t.Errorf("HandleInput(%v) = true, want false", k.Type)
		}
	}
}

func TestStatusBarOnStateChange(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.OnStateChange(statePrompt, stateThinking)
	if sb.state != stateThinking {
		t.Errorf("state = %v, want stateThinking", sb.state)
	}
}

func TestStatusBarSetTokenUsage(t *testing.T) {
	tests := []struct {
		name   string
		tokens int
		cost   float64
	}{
		{"zero tokens zero cost", 0, 0},
		{"some tokens no cost", 500, 0},
		{"some tokens with cost", 1500, 0.05},
		{"large tokens", 10000, 1.50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := NewStatusBarComponent()
			sb.SetTokenUsage(tt.tokens, tt.cost)
			if sb.totalTokens != tt.tokens {
				t.Errorf("totalTokens = %d, want %d", sb.totalTokens, tt.tokens)
			}
			if sb.sessionCost != tt.cost {
				t.Errorf("sessionCost = %f, want %f", sb.sessionCost, tt.cost)
			}
		})
	}
}

func TestStatusBarSetActiveTool(t *testing.T) {
	sb := NewStatusBarComponent()
	tool := agent.ToolStatus{Name: "file_read", Running: true}
	sb.SetActiveTool(tool)
	if sb.activeTool.Name != "file_read" {
		t.Errorf("activeTool.Name = %q, want %q", sb.activeTool.Name, "file_read")
	}
	if !sb.activeTool.Running {
		t.Error("activeTool.Running = false, want true")
	}
}

func TestStatusBarSetRoundStart(t *testing.T) {
	sb := NewStatusBarComponent()
	now := time.Now()
	sb.SetRoundStart(now)
	if !sb.roundStartTime.Equal(now) {
		t.Errorf("roundStartTime = %v, want %v", sb.roundStartTime, now)
	}
}

func TestStatusBarSetGoalMode(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.SetGoalMode(true, "my goal")
	if !sb.isGoalMode {
		t.Error("isGoalMode = false, want true")
	}
	if sb.goalText != "my goal" {
		t.Errorf("goalText = %q, want %q", sb.goalText, "my goal")
	}
	sb.SetGoalMode(false, "")
	if sb.isGoalMode {
		t.Error("isGoalMode = true after clearing, want false")
	}
}

func TestStatusBarSetStatusText(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.SetStatusText("analyzing code")
	if sb.statusText != "analyzing code" {
		t.Errorf("statusText = %q, want %q", sb.statusText, "analyzing code")
	}
}

func TestStatusBarRender(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(sb *StatusBarComponent)
		width     int
		wantLines int
		wantStr   string
	}{
		{
			name:      "default render shows ready",
			setup:     func(sb *StatusBarComponent) {},
			width:     80,
			wantLines: 1,
			wantStr:   "ready",
		},
		{
			name: "thinking state shows thinking",
			setup: func(sb *StatusBarComponent) {
				sb.state = stateThinking
				sb.roundStartTime = time.Now()
			},
			width:     80,
			wantLines: 1,
			wantStr:   "thinking",
		},
		{
			name: "streaming state with status text",
			setup: func(sb *StatusBarComponent) {
				sb.state = stateStreaming
				sb.statusText = "generating response"
				sb.roundStartTime = time.Now()
			},
			width:     80,
			wantLines: 1,
			wantStr:   "thinking",
		},
		{
			name: "active tool shows running",
			setup: func(sb *StatusBarComponent) {
				sb.activeTool = agent.ToolStatus{Name: "shell_run", Running: true}
				sb.roundStartTime = time.Now()
			},
			width:     80,
			wantLines: 1,
			wantStr:   "running",
		},
		{
			name: "token display with cost",
			setup: func(sb *StatusBarComponent) {
				sb.SetTokenUsage(2500, 0.15)
			},
			width:     80,
			wantLines: 1,
			wantStr:   "tokens",
		},
		{
			name: "goal mode active",
			setup: func(sb *StatusBarComponent) {
				sb.SetGoalMode(true, "implement feature")
			},
			width:     80,
			wantLines: 1,
			wantStr:   "goal",
		},
		{
			name: "zero width renders",
			setup:     func(sb *StatusBarComponent) {},
			width:     0,
			wantLines: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := NewStatusBarComponent()
			tt.setup(sb)
			lines := sb.Render(tt.width)
			if len(lines) != tt.wantLines {
				t.Errorf("Render() returned %d lines, want %d", len(lines), tt.wantLines)
			}
			if tt.wantStr != "" {
				joined := strings.Join(lines, "\n")
				if !strings.Contains(joined, tt.wantStr) {
					t.Errorf("Render() output missing %q, got:\n%s", tt.wantStr, joined)
				}
			}
		})
	}
}

func TestStatusBarRenderTokenFormats(t *testing.T) {
	tests := []struct {
		name        string
		tokens      int
		cost        float64
		wantContain string
	}{
		{"small tokens", 500, 0, "500"},
		{"large tokens as k", 2500, 0, "2.5k"},
		{"cost under 1 cent", 100, 0.005, "$0.0050"},
		{"cost over 1 cent", 100, 0.15, "$0.15"},
		{"zero tokens shows dash", 0, 0, "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := NewStatusBarComponent()
			sb.SetTokenUsage(tt.tokens, tt.cost)
			lines := sb.Render(120)
			joined := strings.Join(lines, "\n")
			if !strings.Contains(joined, tt.wantContain) {
				t.Errorf("Render() missing %q in:\n%s", tt.wantContain, joined)
			}
		})
	}
}

func TestStatusBarRenderGoalModeLongText(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.SetGoalMode(true, "this is a very long goal text that exceeds twenty chars")
	lines := sb.Render(120)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "goal") {
		t.Errorf("expected 'goal' in output, got:\n%s", joined)
	}
	// Long goal text should be truncated
	if strings.Contains(joined, "exceeds twenty chars") {
		t.Error("long goal text should be truncated with '...'")
	}
}

package tui

import (
	"strings"
	"testing"
	"time"

	"iroha/pkg/agent"
)

func TestTuiState_String(t *testing.T) {
	tests := []struct {
		state TuiState
		want  string
	}{
		{statePrompt, "Prompt"},
		{stateThinking, "Thinking"},
		{stateStreaming, "Streaming"},
		{stateConfirming, "Confirming"},
		{statePermissionSelect, "PermissionSelect"},
		{stateSessionSelect, "SessionSelect"},
		{TuiState(99), "Unknown"},
	}
	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("TuiState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestSlashMenu_Update(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{"/help", "help"},
		{"/hooks", "hooks"},
		{"/exit", "exit"},
	})

	sm.Update("/h")
	if !sm.active {
		t.Error("expected active after /h")
	}
	if len(sm.items) != 2 { // /help, /hooks
		t.Errorf("expected 2 matches for /h, got %d", len(sm.items))
	}

	sm.Update("/he")
	if len(sm.items) != 1 {
		t.Errorf("expected 1 match for /he, got %d", len(sm.items))
	}

	sm.Update("no-slash")
	if sm.active {
		t.Error("expected inactive for non-slash input")
	}

	sm.Update("/zzz")
	if sm.active {
		t.Error("expected inactive for no matches")
	}
}

func TestSlashMenu_HandleInput(t *testing.T) {
	sm := NewSlashMenuComponent(AllSlashCommands)
	sm.Update("/h")

	if !sm.HandleInput(Key{Type: KeyDown}) {
		t.Error("expected handled for KeyDown")
	}
	if !sm.HandleInput(Key{Type: KeyUp}) {
		t.Error("expected handled for KeyUp")
	}
	if !sm.HandleInput(Key{Type: KeyEsc}) {
		t.Error("expected handled for KeyEsc")
	}
	if sm.active {
		t.Error("expected closed after esc")
	}

	// Not active, should not handle
	sm.active = false
	if sm.HandleInput(Key{Type: KeyDown}) {
		t.Error("expected not handled when inactive")
	}
}

func TestSlashMenu_MoveUpDown(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{"/a", "a"}, {"/b", "b"}, {"/c", "c"},
	})
	sm.Update("/")

	sm.index = 0
	sm.MoveDown()
	if sm.index != 1 {
		t.Errorf("after MoveDown: index=%d, want 1", sm.index)
	}
	sm.MoveDown()
	sm.MoveDown()
	if sm.index != 0 {
		t.Errorf("after wrap-around MoveDown: index=%d, want 0", sm.index)
	}

	sm.MoveUp()
	if sm.index != 2 {
		t.Errorf("after MoveUp wrap: index=%d, want 2", sm.index)
	}
}

func TestSlashMenu_Close(t *testing.T) {
	sm := NewSlashMenuComponent(AllSlashCommands)
	sm.Update("/h")
	sm.Close()
	if sm.active {
		t.Error("expected inactive after Close")
	}
	if sm.index != 0 {
		t.Errorf("expected index 0 after Close, got %d", sm.index)
	}
}

func TestSlashMenu_Render(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{"/help", "Show help"}, {"/hooks", "Show hooks"},
	})
	sm.Update("/h")
	lines := sm.Render(80)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "> ") {
		t.Errorf("first line should be selected (prefix >), got: %s", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("second line should not be selected, got: %s", lines[1])
	}

	sm.Close()
	if sm.Render(80) != nil {
		t.Error("expected nil render when inactive")
	}
}

func TestSlashMenu_Active(t *testing.T) {
	sm := NewSlashMenuComponent(AllSlashCommands)
	if sm.Active(statePrompt) {
		t.Error("expected inactive initially")
	}
	sm.Update("/h")
	if !sm.Active(statePrompt) {
		t.Error("expected active in Prompt state")
	}
	if sm.Active(stateThinking) {
		t.Error("expected inactive in non-Prompt state")
	}
}

func TestStatusBar_SetTokenUsage(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.SetTokenUsage(5000, 1.23)
	if sb.totalTokens != 5000 {
		t.Errorf("totalTokens = %d, want 5000", sb.totalTokens)
	}
	if sb.sessionCost != 1.23 {
		t.Errorf("sessionCost = %f, want 1.23", sb.sessionCost)
	}
}

func TestStatusBar_SetActiveTool(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.SetActiveTool(agent.ToolStatus{Name: "file_read", Running: true})
	if sb.activeTool.Name != "file_read" {
		t.Errorf("tool name = %q, want file_read", sb.activeTool.Name)
	}
}

func TestStatusBar_SetRoundStart(t *testing.T) {
	sb := NewStatusBarComponent()
	now := time.Now()
	sb.SetRoundStart(now)
	if !sb.roundStartTime.Equal(now) {
		t.Errorf("roundStartTime not set correctly")
	}
}

func TestStatusBar_SetGoalMode(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.SetGoalMode(true, "increase coverage")
	if !sb.isGoalMode || sb.goalText != "increase coverage" {
		t.Error("goal mode not set correctly")
	}
}

func TestStatusBar_SetStatusText(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.SetStatusText("analyzing")
	if sb.statusText != "analyzing" {
		t.Errorf("statusText = %q, want 'analyzing'", sb.statusText)
	}
}

func TestStatusBar_Active(t *testing.T) {
	sb := NewStatusBarComponent()
	if !sb.Active(statePrompt) {
		t.Error("status bar should always be active")
	}
}

func TestStatusBar_HandleInput(t *testing.T) {
	sb := NewStatusBarComponent()
	if sb.HandleInput(Key{Type: KeyRune}) {
		t.Error("status bar should not handle input")
	}
}

func TestHistoryManager_Add(t *testing.T) {
	hm := NewHistoryManager()
	hm.Add("first")
	hm.Add("second")
	if len(hm.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(hm.Items))
	}

	hm.Add("")
	if len(hm.Items) != 2 {
		t.Error("empty string should not be added")
	}

	hm.Add("second")
	if len(hm.Items) != 2 {
		t.Error("duplicate consecutive should not be added")
	}
}

func TestHistoryManager_Up(t *testing.T) {
	hm := NewHistoryManager()
	if hm.Up() != "" {
		t.Error("empty history should return empty string")
	}

	hm.Add("a")
	hm.Add("b")
	hm.Add("c")

	if hm.Up() != "c" {
		t.Errorf("first Up = %q, want 'c'", hm.Up())
	}
	if hm.Up() != "b" {
		t.Errorf("second Up = %q, want 'b'", hm.Up())
	}
	if hm.Up() != "a" {
		t.Errorf("third Up = %q, want 'a'", hm.Up())
	}
	if hm.Up() != "a" {
		t.Error("should stay at oldest")
	}
}

func TestHistoryManager_Down(t *testing.T) {
	hm := NewHistoryManager()
	if hm.Down() != "" {
		t.Error("empty history should return empty string")
	}

	hm.Add("a")
	hm.Add("b")
	hm.Add("c")

	// Navigate up first, then down
	hm.Up() // c
	hm.Up() // b
	if hm.Down() != "c" {
		t.Errorf("Down after Up = %q, want 'c'", hm.Down())
	}
	if hm.Down() != "" {
		t.Errorf("Down at end = %q, want empty", hm.Down())
	}
}

func TestWordWrap_EdgeCases(t *testing.T) {
	if got := WordWrap("", 10); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
	if got := WordWrap("hello", 0); got != "hello" {
		t.Errorf("zero width should return unchanged, got %q", got)
	}
	wrapped := WordWrap("abcdefghij", 5)
	if !strings.Contains(wrapped, "\n") {
		t.Errorf("expected wrapping for long text, got %q", wrapped)
	}
}

func TestWrapInput(t *testing.T) {
	lines := WrapInput("hi", 2, 80)
	if len(lines) != 1 || lines[0] != "hi" {
		t.Errorf("short input should be single line, got %v", lines)
	}

	lines = WrapInput("ab", 80, 80)
	if len(lines) < 1 {
		t.Errorf("expected at least 1 line, got %v", lines)
	}
}

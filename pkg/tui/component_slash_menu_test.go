package tui

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SlashMenuComponent — table-driven tests
// ---------------------------------------------------------------------------

func TestNewSlashMenuComponent(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit program"},
	}
	sm := NewSlashMenuComponent(commands)
	if sm == nil {
		t.Fatal("NewSlashMenuComponent returned nil")
	}
	if len(sm.all) != 2 {
		t.Errorf("len(all) = %d, want 2", len(sm.all))
	}
	if sm.active {
		t.Error("new slash menu should not be active")
	}
}

func TestSlashMenuActive(t *testing.T) {
	tests := []struct {
		name   string
		active bool
		state  TuiState
		want   bool
	}{
		{"active and prompt state", true, statePrompt, true},
		{"active but not prompt state", true, stateThinking, false},
		{"inactive and prompt state", false, statePrompt, false},
		{"inactive and not prompt state", false, stateStreaming, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSlashMenuComponent(nil)
			sm.active = tt.active
			if got := sm.Active(tt.state); got != tt.want {
				t.Errorf("Active(%v) with active=%v = %v, want %v", tt.state, tt.active, got, tt.want)
			}
		})
	}
}

func TestSlashMenuOnStateChange(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	// OnStateChange is a no-op, should not panic
	sm.OnStateChange(statePrompt, stateThinking)
}

func TestSlashMenuUpdate(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit program"},
		{Command: "/history", Description: "Show history"},
	}

	tests := []struct {
		name       string
		input      string
		wantActive bool
		wantCount  int
	}{
		{"slash prefix activates", "/he", true, 1},
		{"slash with no match deactivates", "/xyz", false, 0},
		{"empty slash matches all", "/", true, 3},
		{"non-slash input deactivates", "hello", false, 0},
		{"empty input deactivates", "", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSlashMenuComponent(commands)
			sm.Update(tt.input)
			if sm.active != tt.wantActive {
				t.Errorf("active = %v, want %v", sm.active, tt.wantActive)
			}
			if tt.wantActive && len(sm.items) != tt.wantCount {
				t.Errorf("len(items) = %d, want %d", len(sm.items), tt.wantCount)
			}
		})
	}
}

func TestSlashMenuUpdateCaseInsensitive(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/Help", Description: "Show help"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.Update("/HELP")
	if !sm.active {
		t.Error("expected case-insensitive match to be active")
	}
	if len(sm.items) != 1 {
		t.Errorf("expected 1 match, got %d", len(sm.items))
	}
}

func TestSlashMenuUpdateResetsIndex(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/history", Description: "Show history"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.index = 5
	sm.Update("/")
	if sm.index != 0 {
		t.Errorf("index = %d, want 0 after Update resets out-of-bounds index", sm.index)
	}
}

func TestSlashMenuUpdateKeepsIndexWhenValid(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/history", Description: "Show history"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.Update("/")
	sm.index = 1
	sm.Update("/") // re-filter with same results
	if sm.index != 1 {
		t.Errorf("index = %d, want 1 (kept within bounds)", sm.index)
	}
}

func TestSlashMenuMoveUp(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
		{Command: "/history", Description: "History"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.items = commands
	sm.index = 1

	sm.MoveUp()
	if sm.index != 0 {
		t.Errorf("index = %d, want 0", sm.index)
	}

	// Wrap around
	sm.MoveUp()
	if sm.index != 2 {
		t.Errorf("index = %d, want 2 (wrap around)", sm.index)
	}
}

func TestSlashMenuMoveUpEmpty(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	sm.items = nil
	sm.MoveUp() // should not panic
}

func TestSlashMenuMoveDown(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
		{Command: "/history", Description: "History"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.items = commands
	sm.index = 0

	sm.MoveDown()
	if sm.index != 1 {
		t.Errorf("index = %d, want 1", sm.index)
	}

	// Wrap around
	sm.index = 2
	sm.MoveDown()
	if sm.index != 0 {
		t.Errorf("index = %d, want 0 (wrap around)", sm.index)
	}
}

func TestSlashMenuMoveDownEmpty(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	sm.items = nil
	sm.MoveDown() // should not panic
}

func TestSlashMenuClose(t *testing.T) {
	sm := NewSlashMenuComponent(AllSlashCommands)
	sm.active = true
	sm.items = AllSlashCommands[:3]
	sm.index = 2

	sm.Close()

	if sm.active {
		t.Error("active should be false after Close")
	}
	if sm.items != nil {
		t.Error("items should be nil after Close")
	}
	if sm.index != 0 {
		t.Errorf("index = %d, want 0 after Close", sm.index)
	}
}

func TestSlashMenuHandleInputInactive(t *testing.T) {
	sm := NewSlashMenuComponent(AllSlashCommands)
	sm.active = false
	got := sm.HandleInput(Key{Type: KeyUp})
	if got {
		t.Error("HandleInput should return false when inactive")
	}
}

func TestSlashMenuHandleInputKeys(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
	}

	tests := []struct {
		name     string
		key      Key
		wantIdx  int
		wantAct  bool
	}{
		{"up moves selection", Key{Type: KeyUp}, 1, true},  // wraps from 0 to 1
		{"down moves selection", Key{Type: KeyDown}, 1, true},
		{"escape closes", Key{Type: KeyEsc}, 0, false},
		{"enter closes with items", Key{Type: KeyEnter}, 0, false},
		{"tab closes with items", Key{Type: KeyTab}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSlashMenuComponent(commands)
			sm.active = true
			sm.items = commands
			sm.index = 0

			if tt.name == "up moves selection" {
				sm.index = 0
			}

			sm.HandleInput(tt.key)

			if sm.active != tt.wantAct {
				t.Errorf("active = %v, want %v", sm.active, tt.wantAct)
			}
		})
	}
}

func TestSlashMenuHandleInputEnterNoItems(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	sm.active = true
	sm.items = nil

	got := sm.HandleInput(Key{Type: KeyEnter})
	// With no items, Enter should not close (stays active)
	if !sm.active {
		t.Error("Enter with no items should not close menu")
	}
	if !got {
		t.Error("HandleInput should return true (consumed)")
	}
}

func TestSlashMenuHandleInputUnknownKey(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	sm.active = true
	got := sm.HandleInput(Key{Type: KeyRune, Rune: 'a'})
	if got {
		t.Error("unknown key should return false")
	}
}

func TestSlashMenuRender(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.active = true
	sm.items = commands
	sm.index = 0

	lines := sm.Render(80)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "> ") {
		t.Errorf("selected item should have '> ' prefix, got: %q", lines[0])
	}
	if strings.HasPrefix(lines[1], "> ") {
		t.Errorf("non-selected item should not have '> ' prefix, got: %q", lines[1])
	}
}

func TestSlashMenuRenderInactive(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	sm.active = false
	lines := sm.Render(80)
	if lines != nil {
		t.Errorf("expected nil for inactive menu, got %d lines", len(lines))
	}
}

func TestSlashMenuRenderNoItems(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	sm.active = true
	sm.items = nil
	lines := sm.Render(80)
	if lines != nil {
		t.Errorf("expected nil for active menu with no items, got %d lines", len(lines))
	}
}

func TestSlashMenuRenderContainsCommandAndDescription(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.active = true
	sm.items = commands
	sm.index = 0

	lines := sm.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "/help") {
		t.Error("expected '/help' in rendered output")
	}
	if !strings.Contains(joined, "Show help") {
		t.Error("expected 'Show help' in rendered output")
	}
}

func TestSlashMenuRenderSecondItemSelected(t *testing.T) {
	commands := []SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
	}
	sm := NewSlashMenuComponent(commands)
	sm.active = true
	sm.items = commands
	sm.index = 1

	lines := sm.Render(80)
	if !strings.HasPrefix(lines[1], "> ") {
		t.Errorf("second item should have '> ' prefix when selected, got: %q", lines[1])
	}
	if strings.HasPrefix(lines[0], "> ") {
		t.Errorf("first item should not have '> ' prefix, got: %q", lines[0])
	}
}

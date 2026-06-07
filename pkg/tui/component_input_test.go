package tui

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// InputComponent — table-driven tests
// ---------------------------------------------------------------------------

func newTestInputComponent() *InputComponent {
	focus := &FocusModel{Owner: FocusPrompt, Buffer: []rune{}, CursorIndex: 0}
	hm := NewHistoryManager()
	return NewInputComponent(focus, hm)
}

func TestNewInputComponent(t *testing.T) {
	ic := newTestInputComponent()
	if ic == nil {
		t.Fatal("NewInputComponent returned nil")
	}
}

func TestInputActive(t *testing.T) {
	tests := []struct {
		name  string
		state TuiState
		want  bool
	}{
		{"prompt state", statePrompt, true},
		{"thinking state", stateThinking, false},
		{"streaming state", stateStreaming, false},
		{"confirming state", stateConfirming, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ic := newTestInputComponent()
			if got := ic.Active(tt.state); got != tt.want {
				t.Errorf("Active(%v) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestInputOnStateChange(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Owner = FocusNone
	ic.OnStateChange(stateThinking, statePrompt)
	if !ic.focus.Is(FocusPrompt) {
		t.Error("expected focus to be FocusPrompt after state change to prompt")
	}
}

func TestInputOnStateChangeNotToPrompt(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Owner = FocusNone
	ic.OnStateChange(statePrompt, stateThinking)
	if ic.focus.Is(FocusPrompt) {
		t.Error("focus should not change when transitioning away from prompt")
	}
}

func TestInputHandleInputNotFocused(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Owner = FocusNone
	got := ic.HandleInput(Key{Type: KeyRune, Rune: 'a'})
	if got {
		t.Error("HandleInput should return false when not focused on prompt")
	}
}

func TestInputHandleInputRune(t *testing.T) {
	ic := newTestInputComponent()
	ic.HandleInput(Key{Type: KeyRune, Rune: 'h'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'i'})

	if string(ic.focus.Buffer) != "hi" {
		t.Errorf("Buffer = %q, want %q", string(ic.focus.Buffer), "hi")
	}
	if ic.focus.CursorIndex != 2 {
		t.Errorf("CursorIndex = %d, want 2", ic.focus.CursorIndex)
	}
}

func TestInputHandleInputBackspace(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("abc")
	ic.focus.CursorIndex = 2

	ic.HandleInput(Key{Type: KeyBackspace})

	if string(ic.focus.Buffer) != "ac" {
		t.Errorf("Buffer = %q, want %q", string(ic.focus.Buffer), "ac")
	}
	if ic.focus.CursorIndex != 1 {
		t.Errorf("CursorIndex = %d, want 1", ic.focus.CursorIndex)
	}
}

func TestInputHandleInputBackspaceAtStart(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("abc")
	ic.focus.CursorIndex = 0

	ic.HandleInput(Key{Type: KeyBackspace})

	if string(ic.focus.Buffer) != "abc" {
		t.Errorf("Buffer should not change at cursor 0, got %q", string(ic.focus.Buffer))
	}
}

func TestInputHandleInputLeftRight(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("abc")
	ic.focus.CursorIndex = 2

	ic.HandleInput(Key{Type: KeyLeft})
	if ic.focus.CursorIndex != 1 {
		t.Errorf("after Left: CursorIndex = %d, want 1", ic.focus.CursorIndex)
	}

	ic.HandleInput(Key{Type: KeyRight})
	if ic.focus.CursorIndex != 2 {
		t.Errorf("after Right: CursorIndex = %d, want 2", ic.focus.CursorIndex)
	}
}

func TestInputHandleInputLeftAtZero(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("abc")
	ic.focus.CursorIndex = 0

	ic.HandleInput(Key{Type: KeyLeft})
	if ic.focus.CursorIndex != 0 {
		t.Errorf("Left at 0: CursorIndex = %d, want 0", ic.focus.CursorIndex)
	}
}

func TestInputHandleInputRightAtEnd(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("abc")
	ic.focus.CursorIndex = 3

	ic.HandleInput(Key{Type: KeyRight})
	if ic.focus.CursorIndex != 3 {
		t.Errorf("Right at end: CursorIndex = %d, want 3", ic.focus.CursorIndex)
	}
}

func TestInputHandleInputAltEnter(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("ab")
	ic.focus.CursorIndex = 1

	ic.HandleInput(Key{Type: KeyAltEnter})

	if string(ic.focus.Buffer) != "a\nb" {
		t.Errorf("Buffer = %q, want %q", string(ic.focus.Buffer), "a\nb")
	}
	if ic.focus.CursorIndex != 2 {
		t.Errorf("CursorIndex = %d, want 2", ic.focus.CursorIndex)
	}
}

func TestInputHandleInputEnterEmpty(t *testing.T) {
	ic := newTestInputComponent()
	got := ic.HandleInput(Key{Type: KeyEnter})
	if !got {
		t.Error("Enter on empty input should still return true (consumed)")
	}
}

func TestInputHandleInputEnterSubmit(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("hello")
	ic.focus.CursorIndex = 5

	var captured string
	ic.OnSubmit = func(prompt string) { captured = prompt }
	ic.HandleInput(Key{Type: KeyEnter})

	if captured != "hello" {
		t.Errorf("OnSubmit called with %q, want %q", captured, "hello")
	}
	if len(ic.focus.Buffer) != 0 {
		t.Error("Buffer should be cleared after submit")
	}
	if ic.focus.CursorIndex != 0 {
		t.Error("CursorIndex should be 0 after submit")
	}
}

func TestInputHandleInputEnterSlashCmd(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("/help")
	ic.focus.CursorIndex = 5

	slashResult := false
	ic.OnSlashCmd = func(cmd string) bool {
		slashResult = true
		return true
	}
	ic.HandleInput(Key{Type: KeyEnter})

	if !slashResult {
		t.Error("expected OnSlashCmd to be called for /help input")
	}
}

func TestInputHandleInputEnterSlashCmdNilCallback(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("/test")
	ic.focus.CursorIndex = 5
	// OnSlashCmd is nil — should not panic, returns false
	got := ic.HandleInput(Key{Type: KeyEnter})
	if got {
		t.Error("Enter with slash input and nil callback should return false")
	}
}

func TestInputHandleInputHistoryUp(t *testing.T) {
	ic := newTestInputComponent()
	ic.history.Add("previous command 1")
	ic.history.Add("previous command 2")

	ic.HandleInput(Key{Type: KeyUp})

	if string(ic.focus.Buffer) != "previous command 2" {
		t.Errorf("Buffer = %q, want %q", string(ic.focus.Buffer), "previous command 2")
	}
}

func TestInputHandleInputHistoryDown(t *testing.T) {
	ic := newTestInputComponent()
	ic.history.Add("cmd1")
	ic.history.Add("cmd2")

	ic.HandleInput(Key{Type: KeyUp})   // now showing "cmd2"
	ic.HandleInput(Key{Type: KeyUp})   // now showing "cmd1"
	ic.HandleInput(Key{Type: KeyDown}) // back to "cmd2"

	if string(ic.focus.Buffer) != "cmd2" {
		t.Errorf("Buffer = %q, want %q", string(ic.focus.Buffer), "cmd2")
	}
}

func TestInputHandleInputUnknownKey(t *testing.T) {
	ic := newTestInputComponent()
	got := ic.HandleInput(Key{Type: KeyCtrlC})
	if got {
		t.Error("unknown key type should return false")
	}
}

func TestInputHandleInputNilHistory(t *testing.T) {
	ic := newTestInputComponent()
	ic.history = nil
	// Should not panic
	ic.HandleInput(Key{Type: KeyUp})
	ic.HandleInput(Key{Type: KeyDown})
}

func TestInputRender(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("hello")

	lines := ic.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render output")
	}
	if !strings.Contains(lines[0], "hello") {
		t.Errorf("expected 'hello' in first line, got: %q", lines[0])
	}
}

func TestInputRenderEmpty(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune{}

	lines := ic.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected at least one line even for empty input")
	}
}

func TestInputRenderPromptPrefix(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("test")

	lines := ic.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render output")
	}
	if !strings.HasPrefix(lines[0], "┃ ") {
		t.Errorf("expected prompt prefix '┃ ', got: %q", lines[0])
	}
}

func TestInputSetSlashMenu(t *testing.T) {
	ic := newTestInputComponent()
	sm := NewSlashMenuComponent(AllSlashCommands)
	ic.SetSlashMenu(sm)
	if ic.slashMenu == nil {
		t.Error("expected slashMenu to be set")
	}
}

func TestInputClear(t *testing.T) {
	ic := newTestInputComponent()
	ic.focus.Buffer = []rune("some text")
	ic.focus.CursorIndex = 9

	ic.Clear()

	if len(ic.focus.Buffer) != 0 {
		t.Errorf("Buffer = %q, want empty", string(ic.focus.Buffer))
	}
	if ic.focus.CursorIndex != 0 {
		t.Errorf("CursorIndex = %d, want 0", ic.focus.CursorIndex)
	}
}

func TestInputSlashMenuIntegration(t *testing.T) {
	ic := newTestInputComponent()
	sm := NewSlashMenuComponent(AllSlashCommands)
	ic.SetSlashMenu(sm)

	// Type "/" to activate slash menu
	ic.HandleInput(Key{Type: KeyRune, Rune: '/'})
	if !sm.active {
		t.Error("slash menu should be active after typing '/'")
	}

	// Navigate up/down
	ic.HandleInput(Key{Type: KeyDown})
	curIdx := sm.index
	ic.HandleInput(Key{Type: KeyUp})
	if sm.index >= curIdx {
		t.Error("expected index to decrease after Up")
	}

	// Tab to select
	sm.index = 0
	ic.HandleInput(Key{Type: KeyTab})
	if sm.active {
		t.Error("slash menu should close after Tab selection")
	}
}

func TestInputSlashMenuEscape(t *testing.T) {
	ic := newTestInputComponent()
	sm := NewSlashMenuComponent(AllSlashCommands)
	ic.SetSlashMenu(sm)
	sm.active = true
	sm.items = AllSlashCommands
	sm.index = 0

	ic.HandleInput(Key{Type: KeyEsc})
	if sm.active {
		t.Error("slash menu should close after Esc")
	}
}

func TestInputSlashMenuEnterSelect(t *testing.T) {
	ic := newTestInputComponent()
	sm := NewSlashMenuComponent(AllSlashCommands)
	ic.SetSlashMenu(sm)
	sm.active = true
	sm.items = AllSlashCommands[:3]
	sm.index = 0

	ic.HandleInput(Key{Type: KeyEnter})
	if sm.active {
		t.Error("slash menu should close after Enter selection")
	}
	// The command should be in the buffer (without submit since it starts with /)
	if !strings.HasPrefix(string(ic.focus.Buffer), "/") {
		t.Errorf("expected buffer to start with /, got %q", string(ic.focus.Buffer))
	}
}

func TestInputSlashMenuUpOverridesHistoryUp(t *testing.T) {
	ic := newTestInputComponent()
	sm := NewSlashMenuComponent(AllSlashCommands)
	ic.SetSlashMenu(sm)
	ic.history.Add("old command")

	sm.active = true
	sm.items = AllSlashCommands[:5]
	sm.index = 2

	ic.HandleInput(Key{Type: KeyUp})
	// Should move slash menu up, not load history
	if sm.index != 1 {
		t.Errorf("slash menu index = %d, want 1", sm.index)
	}
}

func TestInputSlashMenuDownOverridesHistoryDown(t *testing.T) {
	ic := newTestInputComponent()
	sm := NewSlashMenuComponent(AllSlashCommands)
	ic.SetSlashMenu(sm)
	ic.history.Add("old command")

	sm.active = true
	sm.items = AllSlashCommands[:5]
	sm.index = 1

	ic.HandleInput(Key{Type: KeyDown})
	if sm.index != 2 {
		t.Errorf("slash menu index = %d, want 2", sm.index)
	}
}

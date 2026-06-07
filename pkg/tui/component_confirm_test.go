package tui

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ConfirmComponent — table-driven tests
// ---------------------------------------------------------------------------

func TestNewConfirmComponent(t *testing.T) {
	cc := NewConfirmComponent()
	if cc == nil {
		t.Fatal("NewConfirmComponent returned nil")
	}
}

func TestConfirmActive(t *testing.T) {
	tests := []struct {
		name  string
		state TuiState
		want  bool
	}{
		{"confirming state", stateConfirming, true},
		{"prompt state", statePrompt, false},
		{"thinking state", stateThinking, false},
		{"streaming state", stateStreaming, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc := NewConfirmComponent()
			if got := cc.Active(tt.state); got != tt.want {
				t.Errorf("Active(%v) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestConfirmOnStateChange(t *testing.T) {
	cc := NewConfirmComponent()
	// OnStateChange is a no-op, should not panic
	cc.OnStateChange(statePrompt, stateConfirming)
}

func TestConfirmSetPromptNoDiff(t *testing.T) {
	cc := NewConfirmComponent()
	cc.SetPrompt("Allow running command?")
	if cc.prompt != "Allow running command?" {
		t.Errorf("prompt = %q, want %q", cc.prompt, "Allow running command?")
	}
	if cc.diffText != "" {
		t.Errorf("diffText = %q, want empty", cc.diffText)
	}
	if cc.selectIndex != 0 {
		t.Errorf("selectIndex = %d, want 0", cc.selectIndex)
	}
	if cc.diffActive {
		t.Error("diffActive should be false after SetPrompt")
	}
}

func TestConfirmSetPromptWithDiff(t *testing.T) {
	cc := NewConfirmComponent()
	diffContent := "+added\n-removed"
	fullPrompt := "Allow write?\n\n\x1b[1;34m[File Changes (Diff)]:\x1b[0m\n" + diffContent
	cc.SetPrompt(fullPrompt)

	if cc.prompt != "Allow write?" {
		t.Errorf("prompt = %q, want %q", cc.prompt, "Allow write?")
	}
	if cc.diffText != diffContent {
		t.Errorf("diffText = %q, want %q", cc.diffText, diffContent)
	}
}

func TestConfirmHandleInputNavigation(t *testing.T) {
	tests := []struct {
		name      string
		selectIdx int
		key       Key
		wantIdx   int
	}{
		{"right from 0", 0, Key{Type: KeyRight}, 1},
		{"right from 4 wraps to 0", 4, Key{Type: KeyRight}, 0},
		{"left from 0 wraps to 4", 0, Key{Type: KeyLeft}, 4},
		{"tab moves right", 0, Key{Type: KeyTab}, 1},
		{"shift-tab moves left", 2, Key{Type: KeyShiftTab}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc := NewConfirmComponent()
			cc.selectIndex = tt.selectIdx
			cc.HandleInput(tt.key)
			if cc.selectIndex != tt.wantIdx {
				t.Errorf("selectIndex = %d, want %d", cc.selectIndex, tt.wantIdx)
			}
		})
	}
}

func TestConfirmHandleInputEnterY(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 0 // Y option
	var captured string
	cc.OnRespond = func(resp string) { captured = resp }
	cc.HandleInput(Key{Type: KeyEnter})
	if captured != "y" {
		t.Errorf("response = %q, want %q", captured, "y")
	}
}

func TestConfirmHandleInputEnterN(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 1 // N option
	var captured string
	cc.OnRespond = func(resp string) { captured = resp }
	cc.HandleInput(Key{Type: KeyEnter})
	if captured != "n" {
		t.Errorf("response = %q, want %q", captured, "n")
	}
}

func TestConfirmHandleInputEnterAlways(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 2 // Always option
	var captured string
	cc.OnRespond = func(resp string) { captured = resp }
	cc.HandleInput(Key{Type: KeyEnter})
	if captured != "always" {
		t.Errorf("response = %q, want %q", captured, "always")
	}
}

func TestConfirmHandleInputEnterEdit(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 3 // Edit option
	cc.HandleInput(Key{Type: KeyEnter})
	if !cc.editActive {
		t.Error("expected editActive to be true after entering edit mode via Enter")
	}
}

func TestConfirmHandleInputEnterExplain(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 4 // Explain option
	var captured string
	cc.OnRespond = func(resp string) { captured = resp }
	cc.HandleInput(Key{Type: KeyEnter})
	if captured != "explain" {
		t.Errorf("response = %q, want %q", captured, "explain")
	}
}

func TestConfirmHandleInputEnterNilCallback(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 0
	// OnRespond is nil — should not panic
	cc.HandleInput(Key{Type: KeyEnter})
}

func TestConfirmHandleInputRuneKeys(t *testing.T) {
	tests := []struct {
		name     string
		rune     rune
		wantResp string
	}{
		{"y responds yes", 'y', "y"},
		{"Y responds yes", 'Y', "y"},
		{"n responds no", 'n', "n"},
		{"N responds no", 'N', "n"},
		{"a responds always", 'a', "always"},
		{"A responds always", 'A', "always"},
		{"? responds explain", '?', "explain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc := NewConfirmComponent()
			var captured string
			cc.OnRespond = func(resp string) { captured = resp }
			cc.HandleInput(Key{Type: KeyRune, Rune: tt.rune})
			if captured != tt.wantResp {
				t.Errorf("response = %q, want %q", captured, tt.wantResp)
			}
		})
	}
}

func TestConfirmHandleInputRuneE(t *testing.T) {
	cc := NewConfirmComponent()
	cc.HandleInput(Key{Type: KeyRune, Rune: 'e'})
	if !cc.editActive {
		t.Error("expected editActive = true after pressing 'e'")
	}
}

func TestConfirmHandleInputRuneD(t *testing.T) {
	cc := NewConfirmComponent()
	cc.diffText = "+added"
	cc.HandleInput(Key{Type: KeyRune, Rune: 'd'})
	if !cc.diffActive {
		t.Error("expected diffActive = true after pressing 'd' with diff text")
	}
}

func TestConfirmHandleInputRuneDNoDiff(t *testing.T) {
	cc := NewConfirmComponent()
	cc.diffText = ""
	cc.HandleInput(Key{Type: KeyRune, Rune: 'd'})
	if cc.diffActive {
		t.Error("diffActive should stay false when diffText is empty")
	}
}

func TestConfirmHandleInputUnknownKey(t *testing.T) {
	cc := NewConfirmComponent()
	got := cc.HandleInput(Key{Type: KeyCtrlC})
	if got {
		t.Error("unhandled key type should return false")
	}
}

func TestConfirmHandleInputUnknownRuneStillConsumed(t *testing.T) {
	cc := NewConfirmComponent()
	got := cc.HandleInput(Key{Type: KeyRune, Rune: 'z'})
	if !got {
		t.Error("KeyRune is consumed even when rune doesn't match specific handlers")
	}
}

func TestConfirmEditModeEnter(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("modified command")
	cc.editCursor = len(cc.editBuffer)

	var captured string
	cc.OnRespond = func(resp string) { captured = resp }
	cc.HandleInput(Key{Type: KeyEnter})

	if cc.editActive {
		t.Error("editActive should be false after Enter")
	}
	if captured != "edit:modified command" {
		t.Errorf("response = %q, want %q", captured, "edit:modified command")
	}
}

func TestConfirmEditModeEsc(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("test")
	cc.editCursor = 4

	cc.HandleInput(Key{Type: KeyEsc})

	if cc.editActive {
		t.Error("editActive should be false after Esc")
	}
	if cc.editBuffer != nil {
		t.Error("editBuffer should be nil after Esc")
	}
}

func TestConfirmEditModeBackspace(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("abc")
	cc.editCursor = 2

	cc.HandleInput(Key{Type: KeyBackspace})

	// Backspace at cursor=2 removes rune at index 1 ('b'), leaving "ac"
	if string(cc.editBuffer) != "ac" {
		t.Errorf("editBuffer = %q, want %q", string(cc.editBuffer), "ac")
	}
	if cc.editCursor != 1 {
		t.Errorf("editCursor = %d, want 1", cc.editCursor)
	}
}

func TestConfirmEditModeBackspaceAtStart(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("abc")
	cc.editCursor = 0

	cc.HandleInput(Key{Type: KeyBackspace})

	if string(cc.editBuffer) != "abc" {
		t.Errorf("editBuffer = %q, should not change at cursor 0", string(cc.editBuffer))
	}
}

func TestConfirmEditModeLeftRight(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("abc")
	cc.editCursor = 2

	cc.HandleInput(Key{Type: KeyLeft})
	if cc.editCursor != 1 {
		t.Errorf("after Left: editCursor = %d, want 1", cc.editCursor)
	}

	cc.HandleInput(Key{Type: KeyRight})
	if cc.editCursor != 2 {
		t.Errorf("after Right: editCursor = %d, want 2", cc.editCursor)
	}
}

func TestConfirmEditModeLeftAtZero(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("abc")
	cc.editCursor = 0

	cc.HandleInput(Key{Type: KeyLeft})
	if cc.editCursor != 0 {
		t.Errorf("Left at 0: editCursor = %d, want 0", cc.editCursor)
	}
}

func TestConfirmEditModeRightAtEnd(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("abc")
	cc.editCursor = 3

	cc.HandleInput(Key{Type: KeyRight})
	if cc.editCursor != 3 {
		t.Errorf("Right at end: editCursor = %d, want 3", cc.editCursor)
	}
}

func TestConfirmEditModeRune(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("ac")
	cc.editCursor = 1

	cc.HandleInput(Key{Type: KeyRune, Rune: 'b'})

	if string(cc.editBuffer) != "abc" {
		t.Errorf("editBuffer = %q, want %q", string(cc.editBuffer), "abc")
	}
	if cc.editCursor != 2 {
		t.Errorf("editCursor = %d, want 2", cc.editCursor)
	}
}

func TestConfirmEditModeUnknownKey(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	got := cc.HandleInput(Key{Type: KeyTab})
	if got {
		t.Error("unknown key in edit mode should return false")
	}
}

func TestConfirmRenderNormal(t *testing.T) {
	cc := NewConfirmComponent()
	cc.prompt = "Allow running command?"
	cc.selectIndex = 0

	lines := cc.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render output")
	}
}

func TestConfirmRenderEditMode(t *testing.T) {
	cc := NewConfirmComponent()
	cc.editActive = true
	cc.editBuffer = []rune("some args")
	cc.editCursor = 9

	lines := cc.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render output in edit mode")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Editing Tool Arguments") {
		t.Errorf("expected edit mode header, got:\n%s", joined)
	}
}

func TestConfirmRenderWithDiff(t *testing.T) {
	cc := NewConfirmComponent()
	cc.prompt = "Allow write?"
	cc.diffText = "+added line\n-removed line"
	cc.diffActive = true
	cc.selectIndex = 0

	lines := cc.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "added line") {
		t.Errorf("expected diff content when diffActive, got:\n%s", joined)
	}
}

func TestConfirmGetEditableValuePath(t *testing.T) {
	cc := NewConfirmComponent()
	cc.activeToolArgs = map[string]any{"path": "/tmp/file.txt"}
	if val := cc.getEditableValue(); val != "/tmp/file.txt" {
		t.Errorf("getEditableValue() = %q, want %q", val, "/tmp/file.txt")
	}
}

func TestConfirmGetEditableValueInvalidType(t *testing.T) {
	cc := NewConfirmComponent()
	cc.activeToolArgs = "not a map"
	if val := cc.getEditableValue(); val != "" {
		t.Errorf("getEditableValue() = %q, want empty for non-map args", val)
	}
}

func TestConfirmGetEditableValueMapNoKnownKey(t *testing.T) {
	cc := NewConfirmComponent()
	cc.activeToolArgs = map[string]any{"unknown_key": "value"}
	if val := cc.getEditableValue(); val != "" {
		t.Errorf("getEditableValue() = %q, want empty for unknown keys", val)
	}
}

func TestConfirmEnterEditMode(t *testing.T) {
	cc := NewConfirmComponent()
	cc.activeToolArgs = map[string]any{"command": "echo hello"}
	cc.enterEditMode()

	if !cc.editActive {
		t.Error("editActive should be true")
	}
	if string(cc.editBuffer) != "echo hello" {
		t.Errorf("editBuffer = %q, want %q", string(cc.editBuffer), "echo hello")
	}
	if cc.editCursor != len("echo hello") {
		t.Errorf("editCursor = %d, want %d", cc.editCursor, len("echo hello"))
	}
}

func TestConfirmEnterEditModeNilArgs(t *testing.T) {
	cc := NewConfirmComponent()
	cc.activeToolArgs = nil
	cc.enterEditMode()

	if !cc.editActive {
		t.Error("editActive should be true")
	}
	if len(cc.editBuffer) != 0 {
		t.Errorf("editBuffer should be empty for nil args, got %q", string(cc.editBuffer))
	}
}

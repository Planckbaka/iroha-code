package tui

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ScreenComponent — table-driven tests
// ---------------------------------------------------------------------------

func TestNewScreenComponent(t *testing.T) {
	sc := NewScreenComponent()
	if sc == nil {
		t.Fatal("NewScreenComponent returned nil")
	}
	if sc.permSelectIndex != 1 {
		t.Errorf("default permSelectIndex = %d, want 1", sc.permSelectIndex)
	}
	if sc.sessionListIndex != 0 {
		t.Errorf("default sessionListIndex = %d, want 0", sc.sessionListIndex)
	}
}

func TestScreenActive(t *testing.T) {
	tests := []struct {
		name  string
		state TuiState
		want  bool
	}{
		{"prompt state inactive", statePrompt, false},
		{"thinking state inactive", stateThinking, false},
		{"permission select active", statePermissionSelect, true},
		{"session select active", stateSessionSelect, true},
		{"confirming inactive", stateConfirming, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewScreenComponent()
			if got := sc.Active(tt.state); got != tt.want {
				t.Errorf("Active(%v) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestScreenOnStateChange(t *testing.T) {
	tests := []struct {
		name       string
		oldState   TuiState
		newState   TuiState
		wantScreen string
	}{
		{"to permission select", statePrompt, statePermissionSelect, "permission"},
		{"to session select", statePrompt, stateSessionSelect, "session"},
		{"to prompt does not set screen", statePermissionSelect, statePrompt, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewScreenComponent()
			sc.OnStateChange(tt.oldState, tt.newState)
			if sc.screenType != tt.wantScreen {
				t.Errorf("screenType = %q, want %q", sc.screenType, tt.wantScreen)
			}
		})
	}
}

func TestScreenSetPermIndex(t *testing.T) {
	sc := NewScreenComponent()
	sc.SetPermIndex(3)
	if sc.permSelectIndex != 3 {
		t.Errorf("permSelectIndex = %d, want 3", sc.permSelectIndex)
	}
}

func TestScreenSetSessions(t *testing.T) {
	sessions := []SessionEntry{
		{ID: "s1", LastUpdateStr: "2024-01-01", TotalTokens: 100, TotalCost: 0.5, LastMsg: "hello"},
		{ID: "s2", LastUpdateStr: "2024-01-02", TotalTokens: 200, TotalCost: 1.0, LastMsg: "world"},
	}
	sc := NewScreenComponent()
	sc.SetSessions(sessions)
	if len(sc.sessionsList) != 2 {
		t.Fatalf("len(sessionsList) = %d, want 2", len(sc.sessionsList))
	}
	if sc.sessionsList[0].ID != "s1" {
		t.Errorf("sessionsList[0].ID = %q, want %q", sc.sessionsList[0].ID, "s1")
	}
}

func TestScreenHandleInputPermissionUp(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	sc.permSelectIndex = 2

	sc.HandleInput(Key{Type: KeyUp})
	if sc.permSelectIndex != 1 {
		t.Errorf("after KeyUp: permSelectIndex = %d, want 1", sc.permSelectIndex)
	}

	// Up at 0 should stay at 0
	sc.permSelectIndex = 0
	sc.HandleInput(Key{Type: KeyUp})
	if sc.permSelectIndex != 0 {
		t.Errorf("KeyUp at 0: permSelectIndex = %d, want 0", sc.permSelectIndex)
	}
}

func TestScreenHandleInputPermissionDown(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	sc.permSelectIndex = 0

	sc.HandleInput(Key{Type: KeyDown})
	if sc.permSelectIndex != 1 {
		t.Errorf("after KeyDown: permSelectIndex = %d, want 1", sc.permSelectIndex)
	}
}

func TestScreenHandleInputPermissionEnter(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	sc.permSelectIndex = 0

	var captured string
	sc.OnPermSelect = func(mode string) { captured = mode }
	sc.HandleInput(Key{Type: KeyEnter})

	if captured == "" {
		t.Error("expected OnPermSelect callback to fire")
	}
}

func TestScreenHandleInputPermissionEnterNilCallback(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	sc.permSelectIndex = 0
	// OnPermSelect is nil — should not panic
	sc.HandleInput(Key{Type: KeyEnter})
}

func TestScreenHandleInputSessionUp(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.sessionListIndex = 2

	sc.HandleInput(Key{Type: KeyUp})
	if sc.sessionListIndex != 1 {
		t.Errorf("after KeyUp: sessionListIndex = %d, want 1", sc.sessionListIndex)
	}

	// Up at 0 stays at 0
	sc.sessionListIndex = 0
	sc.HandleInput(Key{Type: KeyUp})
	if sc.sessionListIndex != 0 {
		t.Errorf("KeyUp at 0: sessionListIndex = %d, want 0", sc.sessionListIndex)
	}
}

func TestScreenHandleInputSessionDown(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.SetSessions([]SessionEntry{
		{ID: "s1"},
		{ID: "s2"},
	})
	sc.sessionListIndex = 0

	sc.HandleInput(Key{Type: KeyDown})
	if sc.sessionListIndex != 1 {
		t.Errorf("after KeyDown: sessionListIndex = %d, want 1", sc.sessionListIndex)
	}
}

func TestScreenHandleInputSessionEnterNewSession(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.sessionListIndex = 0 // "new session" entry

	called := false
	sc.OnNewSession = func() { called = true }
	sc.HandleInput(Key{Type: KeyEnter})

	if !called {
		t.Error("expected OnNewSession callback to fire for index 0")
	}
}

func TestScreenHandleInputSessionEnterExistingSession(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.SetSessions([]SessionEntry{
		{ID: "s1"},
		{ID: "s2"},
	})
	sc.sessionListIndex = 1 // first real session

	var captured string
	sc.OnSessionSelect = func(id string) { captured = id }
	sc.HandleInput(Key{Type: KeyEnter})

	if captured != "s1" {
		t.Errorf("OnSessionSelect called with %q, want %q", captured, "s1")
	}
}

func TestScreenHandleInputSessionEnterNilCallback(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.sessionListIndex = 0
	// OnNewSession is nil — should not panic
	sc.HandleInput(Key{Type: KeyEnter})
}

func TestScreenHandleInputUnknownKey(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	got := sc.HandleInput(Key{Type: KeyRune, Rune: 'x'})
	if got {
		t.Error("HandleInput for unknown key should return false")
	}
}

func TestScreenRenderPermission(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	sc.permSelectIndex = 0

	lines := sc.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render output")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Select Agent Permission Mode") {
		t.Errorf("expected permission screen header, got:\n%s", joined)
	}
}

func TestScreenRenderSession(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.SetSessions([]SessionEntry{
		{ID: "s1", LastUpdateStr: "2024-01-01", LastMsg: "hello"},
	})

	lines := sc.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render output")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Session History Manager") {
		t.Errorf("expected session screen header, got:\n%s", joined)
	}
	if !strings.Contains(joined, "Start New Session") {
		t.Errorf("expected 'Start New Session' option, got:\n%s", joined)
	}
}

func TestScreenRenderZeroWidth(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	lines := sc.Render(0)
	// sanitizedWidth should handle 0 — at minimum returns lines
	if lines == nil {
		t.Error("Render(0) should not return nil")
	}
}

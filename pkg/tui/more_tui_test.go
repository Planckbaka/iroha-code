package tui

import (
	"testing"

	"iroha/pkg/agent"
)

func TestChatComponent_Active(t *testing.T) {
	c := NewChatComponent(nil)
	states := []TuiState{statePrompt, stateThinking, stateStreaming, stateConfirming}
	for _, s := range states {
		if !c.Active(s) {
			t.Errorf("expected active in state %v", s)
		}
	}
	if c.Active(statePermissionSelect) {
		t.Error("should not be active in PermissionSelect")
	}
}

func TestChatComponent_HandleInput(t *testing.T) {
	c := NewChatComponent(nil)
	if c.HandleInput(Key{Type: KeyRune}) {
		t.Error("chat component should not handle input")
	}
}

func TestChatComponent_ResetStream(t *testing.T) {
	c := NewChatComponent(nil)
	c.streamedText = "hello"
	c.renderedText = "rendered"
	c.activeTool = agent.ToolStatus{Running: true, Name: "test"}
	c.ResetStream()
	if c.streamedText != "" {
		t.Error("streamedText should be empty after reset")
	}
	if c.activeTool.Running {
		t.Error("activeTool should be cleared after reset")
	}
}

func TestChatComponent_SetActiveTool(t *testing.T) {
	c := NewChatComponent(nil)
	c.SetActiveTool(agent.ToolStatus{Running: true, Name: "file_read"})
	if !c.activeTool.Running {
		t.Error("should be running")
	}
	c.SetActiveTool(agent.ToolStatus{Running: false, Name: ""})
	if c.activeTool.Running {
		t.Error("should be cleared when not running")
	}
}

func TestChatComponent_SetHistory(t *testing.T) {
	c := NewChatComponent(nil)
	h := NewHistoryStore()
	c.SetHistory(h)
	if c.history != h {
		t.Error("history not set")
	}
}

func TestConfirmComponent_Active(t *testing.T) {
	cc := NewConfirmComponent()
	if !cc.Active(stateConfirming) {
		t.Error("should be active in Confirming state")
	}
	if cc.Active(statePrompt) {
		t.Error("should not be active in Prompt state")
	}
}

func TestConfirmComponent_HandleInput_Navigation(t *testing.T) {
	cc := NewConfirmComponent()
	cc.SetPrompt("test")
	idx := cc.selectIndex
	cc.HandleInput(Key{Type: KeyRight})
	if cc.selectIndex == idx {
		t.Error("right key should change selection")
	}
	cc.HandleInput(Key{Type: KeyLeft})
}

func TestConfirmComponent_SetPrompt(t *testing.T) {
	cc := NewConfirmComponent()
	cc.SetPrompt("Allow file write?")
	if cc.prompt != "Allow file write?" {
		t.Errorf("prompt = %q, want 'Allow file write?'", cc.prompt)
	}
}

func TestScreenComponent_Active(t *testing.T) {
	sc := NewScreenComponent()
	if !sc.Active(statePermissionSelect) {
		t.Error("should be active in PermissionSelect")
	}
	if !sc.Active(stateSessionSelect) {
		t.Error("should be active in SessionSelect")
	}
	if sc.Active(statePrompt) {
		t.Error("should not be active in Prompt")
	}
}

func TestScreenComponent_OnStateChange(t *testing.T) {
	sc := NewScreenComponent()
	sc.OnStateChange(statePrompt, statePermissionSelect)
	if sc.screenType != "permission" {
		t.Errorf("screenType = %q, want 'permission'", sc.screenType)
	}
	sc.OnStateChange(statePermissionSelect, stateSessionSelect)
	if sc.screenType != "session" {
		t.Errorf("screenType = %q, want 'session'", sc.screenType)
	}
}

func TestScreenComponent_SetPermIndex(t *testing.T) {
	sc := NewScreenComponent()
	sc.SetPermIndex(2)
	if sc.permSelectIndex != 2 {
		t.Errorf("permSelectIndex = %d, want 2", sc.permSelectIndex)
	}
}

func TestScreenComponent_SetSessions(t *testing.T) {
	sc := NewScreenComponent()
	sessions := []SessionEntry{{ID: "s1", LastMsg: "hello"}}
	sc.SetSessions(sessions)
	if len(sc.sessionsList) != 1 {
		t.Errorf("sessionsList len = %d, want 1", len(sc.sessionsList))
	}
}

func TestScreenComponent_HandleInput_Nav(t *testing.T) {
	sc := NewScreenComponent()
	sc.OnStateChange(statePrompt, statePermissionSelect)
	sc.HandleInput(Key{Type: KeyDown})
	if sc.permSelectIndex != 2 {
		t.Errorf("after KeyDown permSelectIndex = %d, want 2", sc.permSelectIndex)
	}
	sc.HandleInput(Key{Type: KeyUp})
	if sc.permSelectIndex != 1 {
		t.Errorf("after KeyUp permSelectIndex = %d, want 1", sc.permSelectIndex)
	}
}

func TestStatusBar_OnStateChange(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.OnStateChange(statePrompt, stateThinking)
	if sb.state != stateThinking {
		t.Error("state not updated")
	}
}

func TestNewSlashMenu_DefaultCommands(t *testing.T) {
	if len(AllSlashCommands) == 0 {
		t.Error("AllSlashCommands should not be empty")
	}
	for _, cmd := range AllSlashCommands {
		if cmd.Command == "" || cmd.Description == "" {
			t.Errorf("invalid command entry: %+v", cmd)
		}
	}
}

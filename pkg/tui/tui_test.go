package tui

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"iroha/pkg/agent"
)

func TestRenderConfirmCard(t *testing.T) {
	p := "Allow running command?"

	s0 := RenderConfirmCard(p, 0)
	if !strings.Contains(s0, "Y Allow") {
		t.Error("RenderConfirmCard should render option Y")
	}

	s1 := RenderConfirmCard(p, 1)
	if !strings.Contains(s1, "N Deny") {
		t.Error("RenderConfirmCard should render option N")
	}

	s2 := RenderConfirmCard(p, 2)
	if !strings.Contains(s2, "A Always Allow") {
		t.Error("RenderConfirmCard should render option A")
	}
}

func TestModelConfirmNavigation(t *testing.T) {
	m := SetupRawTui(nil, "test-session", false, "", "")
	m.State = stateConfirming
	m.ConfirmSelectIndex = 0

	// Move right
	m.HandleEvent(Key{Type: KeyRight})
	if m.ConfirmSelectIndex != 1 {
		t.Errorf("expected ConfirmSelectIndex = 1 after KeyRight, got %d", m.ConfirmSelectIndex)
	}

	// Move tab
	m.HandleEvent(Key{Type: KeyTab})
	if m.ConfirmSelectIndex != 2 {
		t.Errorf("expected ConfirmSelectIndex = 2 after KeyTab, got %d", m.ConfirmSelectIndex)
	}

	// Move shift-tab (left)
	m.HandleEvent(Key{Type: KeyShiftTab})
	if m.ConfirmSelectIndex != 1 {
		t.Errorf("expected ConfirmSelectIndex = 1 after KeyShiftTab, got %d", m.ConfirmSelectIndex)
	}
}

func TestRenderToolErrorCard(t *testing.T) {
	// 1. With a non-nil error
	errVal := errors.New("something went wrong")
	res := RenderToolErrorCard("test_tool", "arg1", 100*time.Millisecond, errVal)
	if !strings.Contains(res, "something went wrong") {
		t.Errorf("expected error message in output, got:\n%s", res)
	}

	// 2. With a nil error
	resNil := RenderToolErrorCard("test_tool", "arg1", 100*time.Millisecond, nil)
	if !strings.Contains(resNil, "operation failed") {
		t.Errorf("expected fallback message in output when error is nil, got:\n%s", resNil)
	}
}

func TestNewModelBypassPermission(t *testing.T) {
	mAuto := SetupRawTui(nil, "test-session", false, "auto", "hello")
	if mAuto.State != statePrompt {
		t.Errorf("expected State to be statePrompt when initialMode is set, got %s", mAuto.State.String())
	}
	if mAuto.StartupPrompt != "hello" {
		t.Errorf("expected StartupPrompt to be 'hello', got '%s'", mAuto.StartupPrompt)
	}

	mNone := SetupRawTui(nil, "test-session", false, "", "")
	if mNone.State != statePermissionSelect {
		t.Errorf("expected State to be statePermissionSelect when initialMode is empty, got %s", mNone.State.String())
	}
}

func TestRenderHelpAndCancel(t *testing.T) {
	h := RenderHelpDashboard()
	if !strings.Contains(h, "Iroha Code") || !strings.Contains(h, "Keyboard Shortcuts") {
		t.Errorf("expected help dashboard to render help text, got:\n%s", h)
	}

	c := RenderCancelCard(1500 * time.Millisecond)
	if !strings.Contains(c, "Session aborted by user") || !strings.Contains(c, "1.5s") {
		t.Errorf("expected cancellation card to render elapsed duration, got:\n%s", c)
	}
}

func TestMatchLocalPathsAndSafety(t *testing.T) {
	oldCwd, err := os.Getwd()
	if err == nil {
		if strings.HasSuffix(oldCwd, "pkg/tui") {
			_ = os.Chdir("../../")
			defer func() { _ = os.Chdir(oldCwd) }()
		}
	}

	m := SetupRawTui(nil, "test-session", false, "", "")

	// 1. Valid local matching
	matches := m.matchLocalPaths("go.m")
	if len(matches) == 0 {
		t.Error("expected to match go.mod or go.sum under workspace root, got 0 matches")
	}
	matchedMod := false
	for _, match := range matches {
		if match == "go.mod" {
			matchedMod = true
		}
	}
	if !matchedMod {
		t.Error("expected to match 'go.mod'")
	}

	// 2. Traversal escape safety check
	escapedMatches := m.matchLocalPaths("../../../")
	if len(escapedMatches) != 0 {
		t.Errorf("safety boundary failure: expected 0 matches for traversal escape '../../..', got %d", len(escapedMatches))
	}

	// 3. Absolute path safety check
	absMatches := m.matchLocalPaths("/etc/passwd")
	if len(absMatches) != 0 {
		t.Errorf("safety boundary failure: expected 0 matches for absolute path '/etc/passwd', got %d", len(absMatches))
	}
}

func TestConfirmationPromptAndDiffSplitting(t *testing.T) {
	m := SetupRawTui(nil, "test-session", false, "", "")

	// 1. Prompt without diff marker
	plainPrompt := "Allow writing file test.txt?"
	m.HandleEvent(ConfirmationRequiredMsg{Prompt: plainPrompt})

	if m.ConfirmationPrompt != plainPrompt {
		t.Errorf("expected ConfirmationPrompt to be '%s', got '%s'", plainPrompt, m.ConfirmationPrompt)
	}
	if m.ConfirmDiffText != "" {
		t.Errorf("expected empty ConfirmDiffText, got '%s'", m.ConfirmDiffText)
	}
	if m.ConfirmDiffActive {
		t.Error("expected ConfirmDiffActive to be false initially")
	}

	// 2. Prompt with diff marker
	diffContent := "+ added line\n- deleted line"
	fullPromptWithDiff := "Allow writing file test.txt?\n\n\x1b[1;34m[File Changes (Diff)]:\x1b[0m\n" + diffContent

	m.HandleEvent(ConfirmationRequiredMsg{Prompt: fullPromptWithDiff})

	if m.ConfirmationPrompt != "Allow writing file test.txt?" {
		t.Errorf("expected extracted ConfirmationPrompt to be 'Allow writing file test.txt?', got '%s'", m.ConfirmationPrompt)
	}
	if m.ConfirmDiffText != diffContent {
		t.Errorf("expected extracted ConfirmDiffText to be '%s', got '%s'", diffContent, m.ConfirmDiffText)
	}
	if m.ConfirmDiffActive {
		t.Error("expected ConfirmDiffActive to be false initially")
	}
}

func TestModelDiffToggleKeyAction(t *testing.T) {
	m := SetupRawTui(nil, "test-session", false, "", "")
	m.State = stateConfirming
	m.ConfirmationPrompt = "Allow writing file test.txt?"
	m.ConfirmDiffText = "+ added line\n- deleted line"
	m.ConfirmDiffActive = false

	// Press 'D' to toggle active state
	m.HandleEvent(Key{Type: KeyRune, Rune: 'd'})

	if !m.ConfirmDiffActive {
		t.Error("expected ConfirmDiffActive to be true after pressing 'd'")
	}

	// Press 'D' again to toggle off
	m.HandleEvent(Key{Type: KeyRune, Rune: 'd'})

	if m.ConfirmDiffActive {
		t.Error("expected ConfirmDiffActive to toggle back to false")
	}
}

func TestGetEditableValue(t *testing.T) {
	m := Model{}

	// 1. Nil ActiveTool Args
	if val := m.getEditableValue(); val != "" {
		t.Errorf("expected empty string when active tool args is nil, got '%s'", val)
	}

	// 2. shell_run command extraction
	m.ActiveTool = agent.ToolStatus{
		Name: "shell_run",
		Args: map[string]any{"command": "echo hello"},
	}
	if val := m.getEditableValue(); val != "echo hello" {
		t.Errorf("expected extracted command to be 'echo hello', got '%s'", val)
	}

	// 3. file_write content extraction
	m.ActiveTool = agent.ToolStatus{
		Name: "file_write",
		Args: map[string]any{"content": "print('hello')"},
	}
	if val := m.getEditableValue(); val != "print('hello')" {
		t.Errorf("expected extracted content to be 'print(\\'hello\\')', got '%s'", val)
	}
}

func TestConfirmationFiveOptions(t *testing.T) {
	m := SetupRawTui(nil, "test-session", false, "auto", "hello")
	m.State = stateConfirming
	m.ConfirmSelectIndex = 0

	// 1. Cycle right (Y -> N -> Always -> Edit -> Explain)
	m.HandleEvent(Key{Type: KeyRight})
	if m.ConfirmSelectIndex != 1 {
		t.Errorf("expected cycling right once to select index 1, got %d", m.ConfirmSelectIndex)
	}

	// 2. Cycle right 4 times (wrapping around back to Y)
	for i := 0; i < 4; i++ {
		m.HandleEvent(Key{Type: KeyRight})
	}
	if m.ConfirmSelectIndex != 0 {
		t.Errorf("expected wrapping around to 0, got %d", m.ConfirmSelectIndex)
	}

	// 3. RenderConfirmCardWithDiff rendering check for E Edit and ? Explain buttons
	card := RenderConfirmCardWithDiff("Authorize writing file?", 3, false, false)
	if !strings.Contains(card, "E Edit") || !strings.Contains(card, "? Explain") {
		t.Error("expected RenderConfirmCardWithDiff to contain E Edit and ? Explain buttons")
	}
}

func TestStatsSlashCommand(t *testing.T) {
	m := SetupRawTui(nil, "test-session", false, "auto", "hello")
	m.State = statePrompt

	m.HandleEvent(Key{Type: KeyRune, Rune: '/'})
	m.HandleEvent(Key{Type: KeyRune, Rune: 's'})
	m.HandleEvent(Key{Type: KeyRune, Rune: 't'})
	m.HandleEvent(Key{Type: KeyRune, Rune: 'a'})
	m.HandleEvent(Key{Type: KeyRune, Rune: 't'})
	m.HandleEvent(Key{Type: KeyRune, Rune: 's'})
	m.HandleEvent(Key{Type: KeyEnter})

	if len(m.History) == 0 {
		t.Fatal("expected slash command execution to add logs to History")
	}

	lastLog := m.History[len(m.History)-1]
	if !strings.Contains(lastLog, "Session Statistics & Telemetry") || !strings.Contains(lastLog, "Interaction Rounds") {
		t.Errorf("expected History to contain telemetry details, got:\n%s", lastLog)
	}
}

func TestRawRendererFlickerFree(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRawRenderer(&buf)

	lines1 := []string{"hello", "world"}
	renderer.Draw(lines1, -1, 0)
	out1 := buf.String()

	if !strings.Contains(out1, "hello") || !strings.Contains(out1, "world") {
		t.Error("expected first Draw to render all lines sequentially")
	}

	buf.Reset()
	lines2 := []string{"hello", "there"}
	renderer.Draw(lines2, -1, 0)
	out2 := buf.String()

	// Differential redraw should only update line 2
	if strings.Contains(out2, "hello") {
		t.Error("differential redraw should NOT redraw identical lines like 'hello'")
	}
	if !strings.Contains(out2, "there") {
		t.Error("differential redraw should redraw differing lines like 'there'")
	}
}

func TestToolStreamLinesAccumulation(t *testing.T) {
	// Test legacy TUI Model accumulation
	m := &Model{}
	m.ActiveTool = agent.ToolStatus{
		Name:    "shell_run",
		Running: true,
		StreamLines: []string{"line1"},
	}

	// Trigger dynamic accumulation
	msg1 := ToolStatusMsg{
		Status: agent.ToolStatus{
			Name:    "shell_run",
			Running: true,
			StreamLines: []string{"line2"},
		},
	}
	m.HandleEvent(msg1)

	if len(m.ActiveTool.StreamLines) != 2 || m.ActiveTool.StreamLines[0] != "line1" || m.ActiveTool.StreamLines[1] != "line2" {
		t.Errorf("expected StreamLines to accumulate, got: %v", m.ActiveTool.StreamLines)
	}

	// Test modern App TUI components accumulation
	app := NewApp(nil, "", false, "")
	app.chat.SetActiveTool(agent.ToolStatus{
		Name:    "shell_run",
		Running: true,
		StreamLines: []string{"line1"},
	})

	app.handleToolStatus(agent.ToolStatus{
		Name:    "shell_run",
		Running: true,
		StreamLines: []string{"line2"},
	})

	if len(app.chat.activeTool.StreamLines) != 2 || app.chat.activeTool.StreamLines[0] != "line1" || app.chat.activeTool.StreamLines[1] != "line2" {
		t.Errorf("expected App activeTool StreamLines to accumulate, got: %v", app.chat.activeTool.StreamLines)
	}
}

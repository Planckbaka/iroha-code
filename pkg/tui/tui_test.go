package tui

import (
	"bytes"
	"errors"
	"fmt"
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

func TestConfirmComponentNavigation(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 0

	// Move right
	cc.HandleInput(Key{Type: KeyRight})
	if cc.selectIndex != 1 {
		t.Errorf("expected selectIndex = 1 after KeyRight, got %d", cc.selectIndex)
	}

	// Move tab
	cc.HandleInput(Key{Type: KeyTab})
	if cc.selectIndex != 2 {
		t.Errorf("expected selectIndex = 2 after KeyTab, got %d", cc.selectIndex)
	}

	// Move shift-tab (left)
	cc.HandleInput(Key{Type: KeyShiftTab})
	if cc.selectIndex != 1 {
		t.Errorf("expected selectIndex = 1 after KeyShiftTab, got %d", cc.selectIndex)
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

func TestConfirmComponentPromptAndDiffSplitting(t *testing.T) {
	cc := NewConfirmComponent()

	// 1. Prompt without diff marker
	plainPrompt := "Allow writing file test.txt?"
	cc.SetPrompt(plainPrompt)

	if cc.prompt != plainPrompt {
		t.Errorf("expected prompt to be '%s', got '%s'", plainPrompt, cc.prompt)
	}
	if cc.diffText != "" {
		t.Errorf("expected empty diffText, got '%s'", cc.diffText)
	}
	if cc.diffActive {
		t.Error("expected diffActive to be false initially")
	}

	// 2. Prompt with diff marker
	diffContent := "+ added line\n- deleted line"
	fullPromptWithDiff := "Allow writing file test.txt?\n\n\x1b[1;34m[File Changes (Diff)]:\x1b[0m\n" + diffContent

	cc.SetPrompt(fullPromptWithDiff)

	if cc.prompt != "Allow writing file test.txt?" {
		t.Errorf("expected extracted prompt to be 'Allow writing file test.txt?', got '%s'", cc.prompt)
	}
	if cc.diffText != diffContent {
		t.Errorf("expected extracted diffText to be '%s', got '%s'", diffContent, cc.diffText)
	}
	if cc.diffActive {
		t.Error("expected diffActive to be false after SetPrompt")
	}
}

func TestConfirmComponentDiffToggleKeyAction(t *testing.T) {
	cc := NewConfirmComponent()
	cc.prompt = "Allow writing file test.txt?"
	cc.diffText = "+ added line\n- deleted line"
	cc.diffActive = false

	// Press 'd' to toggle active state
	cc.HandleInput(Key{Type: KeyRune, Rune: 'd'})

	if !cc.diffActive {
		t.Error("expected diffActive to be true after pressing 'd'")
	}

	// Press 'd' again to toggle off
	cc.HandleInput(Key{Type: KeyRune, Rune: 'd'})

	if cc.diffActive {
		t.Error("expected diffActive to toggle back to false")
	}
}

func TestGetEditableValue(t *testing.T) {
	cc := NewConfirmComponent()

	// 1. Nil active tool args
	if val := cc.getEditableValue(); val != "" {
		t.Errorf("expected empty string when active tool args is nil, got '%s'", val)
	}

	// 2. shell_run command extraction
	cc.activeToolArgs = map[string]any{"command": "echo hello"}
	if val := cc.getEditableValue(); val != "echo hello" {
		t.Errorf("expected extracted command to be 'echo hello', got '%s'", val)
	}

	// 3. file_write content extraction
	cc.activeToolArgs = map[string]any{"content": "print('hello')"}
	if val := cc.getEditableValue(); val != "print('hello')" {
		t.Errorf("expected extracted content to be 'print(\\'hello\\')', got '%s'", val)
	}
}

func TestConfirmationFiveOptions(t *testing.T) {
	cc := NewConfirmComponent()
	cc.selectIndex = 0

	// 1. Cycle right (Y -> N -> Always -> Edit -> Explain)
	cc.HandleInput(Key{Type: KeyRight})
	if cc.selectIndex != 1 {
		t.Errorf("expected cycling right once to select index 1, got %d", cc.selectIndex)
	}

	// 2. Cycle right 4 times (wrapping around back to Y)
	for i := 0; i < 4; i++ {
		cc.HandleInput(Key{Type: KeyRight})
	}
	if cc.selectIndex != 0 {
		t.Errorf("expected wrapping around to 0, got %d", cc.selectIndex)
	}

	// 3. RenderConfirmCardWithDiff rendering check for E Edit and ? Explain buttons
	card := RenderConfirmCardWithDiff("Authorize writing file?", 3, false, false)
	if !strings.Contains(card, "E Edit") || !strings.Contains(card, "? Explain") {
		t.Error("expected RenderConfirmCardWithDiff to contain E Edit and ? Explain buttons")
	}
}

func TestStatsSlashCommand(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	app.state = statePrompt

	// handleRawSlashCommand returns true only to signal program exit (e.g. /exit);
	// /stats should not request exit.
	if shouldExit := app.handleRawSlashCommand("/stats"); shouldExit {
		t.Fatal("expected /stats slash command not to request exit")
	}

	if app.history.Len() == 0 {
		t.Fatal("expected slash command execution to add logs to history")
	}

	rendered := strings.Join(app.history.Render(120, 10000), "\n")
	if !strings.Contains(rendered, "Session Statistics & Telemetry") || !strings.Contains(rendered, "Interaction Rounds") {
		t.Errorf("expected history to contain telemetry details, got:\n%s", rendered)
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
	// App TUI components accumulate streamed stdout across status updates.
	app := NewApp(nil, "", false, "")
	app.chat.SetActiveTool(agent.ToolStatus{
		Name:        "shell_run",
		Running:     true,
		StreamLines: []string{"line1"},
	})

	app.handleToolStatus(agent.ToolStatus{
		Name:        "shell_run",
		Running:     true,
		StreamLines: []string{"line2"},
	})

	if len(app.chat.activeTool.StreamLines) != 2 || app.chat.activeTool.StreamLines[0] != "line1" || app.chat.activeTool.StreamLines[1] != "line2" {
		t.Errorf("expected App activeTool StreamLines to accumulate, got: %v", app.chat.activeTool.StreamLines)
	}
}

func TestAppRenderUsesTerminalHeightViewport(t *testing.T) {
	app := NewApp(nil, "", false, "")
	app.state = statePrompt
	app.width = 80
	app.height = 12
	for i := 0; i < 30; i++ {
		app.history.Add(HistoryEntry{Role: RoleSystem, Content: fmt.Sprintf("line-%02d", i)})
	}

	lines := app.Render()
	if len(lines) > app.height {
		t.Fatalf("rendered %d lines for terminal height %d", len(lines), app.height)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "line-00") {
		t.Fatal("viewport rendered oldest content while positioned at bottom")
	}

	app.HandleEvent(Key{Type: KeyPgUp})
	scrolled := strings.Join(app.Render(), "\n")
	if scrolled == joined {
		t.Fatal("PageUp did not change the visible App frame")
	}
}

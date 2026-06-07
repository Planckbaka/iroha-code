package tui

import (
	"strings"
	"testing"
)

// --- Component interface compliance ---

func TestChatComponentImplementsComponent(t *testing.T) {
	var _ Component = (*ChatComponent)(nil)
}

func TestInputComponentImplementsComponent(t *testing.T) {
	var _ Component = (*InputComponent)(nil)
}

func TestConfirmComponentImplementsComponent(t *testing.T) {
	var _ Component = (*ConfirmComponent)(nil)
}

func TestStatusBarComponentImplementsComponent(t *testing.T) {
	var _ Component = (*StatusBarComponent)(nil)
}

func TestSlashMenuComponentImplementsComponent(t *testing.T) {
	var _ Component = (*SlashMenuComponent)(nil)
}

func TestScreenComponentImplementsComponent(t *testing.T) {
	var _ Component = (*ScreenComponent)(nil)
}

// --- ChatComponent ---

func TestChatComponentActive(t *testing.T) {
	c := NewChatComponent(nil)
	if !c.Active(statePrompt) {
		t.Error("should be active in statePrompt")
	}
	if !c.Active(stateThinking) {
		t.Error("should be active in stateThinking")
	}
	if !c.Active(stateStreaming) {
		t.Error("should be active in stateStreaming")
	}
	if !c.Active(stateConfirming) {
		t.Error("should be active in stateConfirming")
	}
}

func TestChatComponentHandleInputReturnsFalse(t *testing.T) {
	c := NewChatComponent(nil)
	if c.HandleInput(Key{Type: KeyRune, Rune: 'a'}) {
		t.Error("ChatComponent should not handle input")
	}
}

// SetStreamedText and ResetStream removed — streaming state is managed by App

func TestChatComponentRenderEmpty(t *testing.T) {
	c := NewChatComponent(nil)
	lines := c.Render(80)
	if len(lines) != 0 {
		t.Error("empty chat with no history should return empty")
	}
}

func TestChatComponentRenderWithHistory(t *testing.T) {
	h := NewHistoryStore()
	h.Add(HistoryEntry{Role: RoleUser, Content: "hello"})
	c := NewChatComponent(h)
	lines := c.Render(80)
	if len(lines) == 0 {
		t.Error("should render history entries")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "hello") {
		t.Error("should contain history content")
	}
}

// --- ConfirmComponent ---

func TestConfirmComponentActive(t *testing.T) {
	c := NewConfirmComponent()
	if c.Active(statePrompt) {
		t.Error("should not be active in statePrompt")
	}
	if !c.Active(stateConfirming) {
		t.Error("should be active in stateConfirming")
	}
}

func TestConfirmComponentSetPrompt(t *testing.T) {
	c := NewConfirmComponent()
	c.SetPrompt("Do you want to proceed?")
	if c.prompt != "Do you want to proceed?" {
		t.Error("prompt should be set")
	}
	if c.selectIndex != 0 {
		t.Error("selectIndex should reset to 0")
	}
}

func TestConfirmComponentSetPromptWithDiff(t *testing.T) {
	c := NewConfirmComponent()
	fullPrompt := "Do you want to proceed?\n\n\x1b[1;34m[File Changes (Diff)]:\x1b[0m\n+added line"
	c.SetPrompt(fullPrompt)
	if c.prompt != "Do you want to proceed?" {
		t.Errorf("prompt should be stripped of diff, got %q", c.prompt)
	}
	if c.diffText == "" {
		t.Error("diffText should be extracted")
	}
}

func TestConfirmComponentHandleYKey(t *testing.T) {
	c := NewConfirmComponent()
	var captured string
	c.OnRespond = func(r string) { captured = r }
	c.HandleInput(Key{Type: KeyRune, Rune: 'y'})
	if captured != "y" {
		t.Errorf("expected 'y', got %q", captured)
	}
}

func TestConfirmComponentHandleNKey(t *testing.T) {
	c := NewConfirmComponent()
	var captured string
	c.OnRespond = func(r string) { captured = r }
	c.HandleInput(Key{Type: KeyRune, Rune: 'n'})
	if captured != "n" {
		t.Errorf("expected 'n', got %q", captured)
	}
}

func TestConfirmComponentHandleEnterYes(t *testing.T) {
	c := NewConfirmComponent()
	c.selectIndex = 0
	var captured string
	c.OnRespond = func(r string) { captured = r }
	c.HandleInput(Key{Type: KeyEnter})
	if captured != "y" {
		t.Errorf("expected 'y', got %q", captured)
	}
}

func TestConfirmComponentHandleEnterNo(t *testing.T) {
	c := NewConfirmComponent()
	c.selectIndex = 1
	var captured string
	c.OnRespond = func(r string) { captured = r }
	c.HandleInput(Key{Type: KeyEnter})
	if captured != "n" {
		t.Errorf("expected 'n', got %q", captured)
	}
}

func TestConfirmComponentTabNavigation(t *testing.T) {
	c := NewConfirmComponent()
	c.HandleInput(Key{Type: KeyRight})
	if c.selectIndex != 1 {
		t.Errorf("expected 1, got %d", c.selectIndex)
	}
	c.HandleInput(Key{Type: KeyRight})
	c.HandleInput(Key{Type: KeyRight})
	c.HandleInput(Key{Type: KeyRight})
	c.HandleInput(Key{Type: KeyRight}) // wrap to 0
	if c.selectIndex != 0 {
		t.Errorf("expected wrap to 0, got %d", c.selectIndex)
	}
}

func TestConfirmComponentEditMode(t *testing.T) {
	c := NewConfirmComponent()
	c.activeToolArgs = map[string]any{"command": "ls -la"}
	c.HandleInput(Key{Type: KeyRune, Rune: 'e'})
	if !c.editActive {
		t.Error("should enter edit mode")
	}
	if string(c.editBuffer) != "ls -la" {
		t.Errorf("edit buffer should be 'ls -la', got %q", string(c.editBuffer))
	}
	if rendered := strings.Join(c.Render(80), "\n"); !strings.Contains(rendered, "ls -la") {
		t.Errorf("edit mode should show the editable value, got %q", rendered)
	}
}

func TestConfirmComponentEditKeys(t *testing.T) {
	c := NewConfirmComponent()
	c.activeToolArgs = map[string]any{"command": "test"}
	c.enterEditMode()

	// Type a character
	c.HandleInput(Key{Type: KeyRune, Rune: 'X'})
	if string(c.editBuffer) != "testX" {
		t.Errorf("expected 'testX', got %q", string(c.editBuffer))
	}

	// Backspace
	c.HandleInput(Key{Type: KeyBackspace})
	if string(c.editBuffer) != "test" {
		t.Errorf("expected 'test', got %q", string(c.editBuffer))
	}

	// Escape
	c.HandleInput(Key{Type: KeyEsc})
	if c.editActive {
		t.Error("Esc should exit edit mode")
	}
}

func TestConfirmComponentEditSubmit(t *testing.T) {
	c := NewConfirmComponent()
	c.activeToolArgs = map[string]any{"command": "original"}
	c.enterEditMode()

	// Modify buffer
	c.HandleInput(Key{Type: KeyRune, Rune: '2'})

	var captured string
	c.OnRespond = func(r string) { captured = r }
	c.HandleInput(Key{Type: KeyEnter})
	if captured != "edit:original2" {
		t.Errorf("expected 'edit:original2', got %q", captured)
	}
}

func TestConfirmComponentRender(t *testing.T) {
	c := NewConfirmComponent()
	c.SetPrompt("Proceed?")
	lines := c.Render(80)
	if len(lines) == 0 {
		t.Error("should render confirmation card")
	}
}

// --- InputComponent ---

func TestInputComponentActive(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt}
	ic := NewInputComponent(focus, nil)
	if !ic.Active(statePrompt) {
		t.Error("should be active in statePrompt")
	}
	if ic.Active(stateThinking) {
		t.Error("should not be active in stateThinking")
	}
}

func TestInputComponentTyping(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt}
	ic := NewInputComponent(focus, nil)
	ic.HandleInput(Key{Type: KeyRune, Rune: 'h'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'i'})
	if string(ic.focus.Buffer) != "hi" {
		t.Errorf("expected 'hi', got %q", string(ic.focus.Buffer))
	}
}

func TestInputComponentBackspace(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt}
	ic := NewInputComponent(focus, nil)
	ic.HandleInput(Key{Type: KeyRune, Rune: 'a'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'b'})
	ic.HandleInput(Key{Type: KeyBackspace})
	if string(ic.focus.Buffer) != "a" {
		t.Errorf("expected 'a', got %q", string(ic.focus.Buffer))
	}
}

func TestInputComponentCursor(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt}
	ic := NewInputComponent(focus, nil)
	ic.HandleInput(Key{Type: KeyRune, Rune: 'a'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'b'})
	ic.HandleInput(Key{Type: KeyLeft})
	if focus.CursorIndex != 1 {
		t.Errorf("expected cursor at 1, got %d", focus.CursorIndex)
	}
	ic.HandleInput(Key{Type: KeyRight})
	if focus.CursorIndex != 2 {
		t.Errorf("expected cursor at 2, got %d", focus.CursorIndex)
	}
}

func TestInputComponentSubmit(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt}
	ic := NewInputComponent(focus, nil)
	var captured string
	ic.OnSubmit = func(p string) { captured = p }
	ic.HandleInput(Key{Type: KeyRune, Rune: 'h'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'i'})
	ic.HandleInput(Key{Type: KeyEnter})
	if captured != "hi" {
		t.Errorf("expected 'hi', got %q", captured)
	}
}

func TestInputComponentSubmitEmpty(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt}
	ic := NewInputComponent(focus, nil)
	var captured string
	ic.OnSubmit = func(p string) { captured = p }
	ic.HandleInput(Key{Type: KeyEnter})
	if captured != "" {
		t.Error("empty input should not trigger submit")
	}
}

func TestInputComponentSlashMenuItem(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt}
	ic := NewInputComponent(focus, nil)
	var captured string
	ic.OnSlashCmd = func(cmd string) bool { captured = cmd; return true }
	ic.HandleInput(Key{Type: KeyRune, Rune: '/'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'h'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'e'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'l'})
	ic.HandleInput(Key{Type: KeyRune, Rune: 'p'})
	ic.HandleInput(Key{Type: KeyEnter})
	if captured != "/help" {
		t.Errorf("expected '/help', got %q", captured)
	}
}

func TestInputComponentRender(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt, Buffer: []rune("hi")}
	ic := NewInputComponent(focus, nil)
	lines := ic.Render(80)
	if len(lines) == 0 {
		t.Error("should render input")
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "hi") {
		t.Error("should contain input text")
	}
	if !strings.Contains(joined, "┃") {
		t.Error("should contain prompt prefix")
	}
}

func TestInputComponentRenderWrapsLongInput(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt, Buffer: []rune("abcdefghijklmnop")}
	ic := NewInputComponent(focus, nil)
	lines := ic.Render(10)

	if len(lines) < 2 {
		t.Fatalf("expected wrapped input, got %q", lines)
	}
	for _, line := range lines {
		if width := visualWidth(line); width > 10 {
			t.Fatalf("input line width = %d, want <= 10: %q", width, line)
		}
	}
	if !strings.HasPrefix(lines[0], "┃ ") {
		t.Fatalf("first line should keep prompt prefix, got %q", lines[0])
	}
	if strings.HasPrefix(lines[1], "┃ ") {
		t.Fatalf("continuation line should not repeat prompt glyph, got %q", lines[1])
	}
}

func TestInputComponentClear(t *testing.T) {
	focus := &FocusModel{Owner: FocusPrompt, Buffer: []rune("test"), CursorIndex: 4}
	ic := NewInputComponent(focus, nil)
	ic.Clear()
	if string(ic.focus.Buffer) != "" {
		t.Error("buffer should be empty after clear")
	}
	if focus.CursorIndex != 0 {
		t.Error("cursor should be at 0 after clear")
	}
}

func TestInputComponentNotFocused(t *testing.T) {
	focus := &FocusModel{Owner: FocusNone}
	ic := NewInputComponent(focus, nil)
	handled := ic.HandleInput(Key{Type: KeyRune, Rune: 'a'})
	if handled {
		t.Error("should not handle input when not focused")
	}
}

// --- SlashMenuComponent ---

func TestSlashMenuComponentUpdate(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
		{Command: "/stats", Description: "Statistics"},
	})
	sm.Update("/h")
	if !sm.active {
		t.Error("menu should be active after matching input")
	}
	if len(sm.items) != 1 {
		t.Errorf("expected 1 match, got %d", len(sm.items))
	}
	if sm.items[0].Command != "/help" {
		t.Errorf("expected /help, got %s", sm.items[0].Command)
	}
}

func TestSlashMenuComponentNoMatch(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{Command: "/help", Description: "Show help"},
	})
	sm.Update("/zzz")
	if sm.active {
		t.Error("menu should not be active with no match")
	}
}

func TestSlashMenuComponentNonSlashInput(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{Command: "/help", Description: "Show help"},
	})
	sm.Update("hello")
	if sm.active {
		t.Error("menu should not activate for non-slash input")
	}
}

func TestSlashMenuComponentNavigation(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
	})
	sm.Update("/")
	sm.MoveDown()
	if sm.index != 1 {
		t.Errorf("expected index 1, got %d", sm.index)
	}
	sm.MoveUp()
	if sm.index != 0 {
		t.Errorf("expected index 0, got %d", sm.index)
	}
}

func TestSlashMenuComponentClose(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{Command: "/help", Description: "Show help"},
	})
	sm.Update("/")
	sm.Close()
	if sm.active {
		t.Error("menu should be inactive after close")
	}
}

func TestSlashMenuComponentRender(t *testing.T) {
	sm := NewSlashMenuComponent([]SlashMenuItem{
		{Command: "/help", Description: "Show help"},
		{Command: "/exit", Description: "Exit"},
	})
	sm.Update("/")
	lines := sm.Render(80)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "/help") {
		t.Error("should contain /help")
	}
}

func TestSlashMenuComponentRenderInactive(t *testing.T) {
	sm := NewSlashMenuComponent(nil)
	lines := sm.Render(80)
	if lines != nil {
		t.Error("inactive menu should return nil")
	}
}

// --- StatusBarComponent ---

func TestStatusBarComponentAlwaysActive(t *testing.T) {
	sb := NewStatusBarComponent()
	if !sb.Active(statePrompt) {
		t.Error("status bar should always be active")
	}
	if !sb.Active(stateThinking) {
		t.Error("status bar should always be active")
	}
}

func TestStatusBarComponentRender(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.mode = "default"
	sb.SetTokenUsage(1000, 0.05)
	lines := sb.Render(80)
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "default") {
		t.Error("should contain mode name")
	}
}

func TestStatusBarComponentSetGoalMode(t *testing.T) {
	sb := NewStatusBarComponent()
	sb.mode = "default"
	sb.SetGoalMode(true, "my objective")
	lines := sb.Render(80)
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "goal") {
		t.Error("should show goal indicator")
	}
}

// --- ScreenComponent ---

func TestScreenComponentActive(t *testing.T) {
	sc := NewScreenComponent()
	if sc.Active(statePrompt) {
		t.Error("should not be active in statePrompt")
	}
	sc.screenType = "permission"
	if !sc.Active(statePermissionSelect) {
		t.Error("should be active in statePermissionSelect")
	}
}

func TestScreenComponentPermNavigation(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	sc.HandleInput(Key{Type: KeyDown})
	if sc.permSelectIndex != 2 {
		t.Errorf("expected 2, got %d", sc.permSelectIndex)
	}
	sc.HandleInput(Key{Type: KeyUp})
	if sc.permSelectIndex != 1 {
		t.Errorf("expected 1, got %d", sc.permSelectIndex)
	}
}

func TestScreenComponentPermSelect(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	sc.permSelectIndex = 0
	var captured string
	sc.OnPermSelect = func(m string) { captured = m }
	sc.HandleInput(Key{Type: KeyEnter})
	if captured == "" {
		t.Error("should trigger perm select callback")
	}
}

func TestScreenComponentSessionNavigation(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.SetSessions([]SessionEntry{
		{ID: "s1", LastMsg: "session 1"},
		{ID: "s2", LastMsg: "session 2"},
	})
	sc.HandleInput(Key{Type: KeyDown})
	if sc.sessionListIndex != 1 {
		t.Errorf("expected 1, got %d", sc.sessionListIndex)
	}
}

func TestScreenComponentNewSession(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	called := false
	sc.OnNewSession = func() { called = true }
	sc.HandleInput(Key{Type: KeyEnter})
	if !called {
		t.Error("index 0 should trigger new session")
	}
}

func TestScreenComponentOnStateChange(t *testing.T) {
	sc := NewScreenComponent()
	sc.OnStateChange(statePrompt, statePermissionSelect)
	if sc.screenType != "permission" {
		t.Error("should set screen type to permission")
	}
	sc.OnStateChange(statePrompt, stateSessionSelect)
	if sc.screenType != "session" {
		t.Error("should set screen type to session")
	}
}

func TestScreenComponentRenderPermission(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "permission"
	lines := sc.Render(80)
	if len(lines) == 0 {
		t.Error("should render permission screen")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Permission") {
		t.Error("should contain permission text")
	}
}

func TestScreenComponentRenderSession(t *testing.T) {
	sc := NewScreenComponent()
	sc.screenType = "session"
	sc.SetSessions([]SessionEntry{
		{ID: "abc123", LastUpdateStr: "2026-01-01", LastMsg: "hello"},
	})
	lines := sc.Render(80)
	if len(lines) == 0 {
		t.Error("should render session screen")
	}
}

// --- Word Wrap ---

func TestWordWrap(t *testing.T) {
	result := WordWrap("hello world", 80)
	if result != "hello world" {
		t.Error("short text should not be wrapped")
	}
}

func TestWordWrapLongLine(t *testing.T) {
	text := "this is a very long line that should be wrapped at some point when it exceeds the width"
	result := WordWrap(text, 20)
	if result == text {
		t.Error("long text should be wrapped")
	}
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Error("should produce multiple lines")
	}
}

func TestWordWrapEmpty(t *testing.T) {
	if WordWrap("", 80) != "" {
		t.Error("empty text should return empty")
	}
}

func TestWordWrapZeroWidth(t *testing.T) {
	text := "hello"
	if WordWrap(text, 0) != text {
		t.Error("zero width should return original")
	}
}

func TestWordWrapMultiLine(t *testing.T) {
	text := "line one\nline two"
	result := WordWrap(text, 80)
	if result != text {
		t.Error("short multi-line text should not be modified")
	}
}

// --- HistoryStore PageUp/PageDown ---

func TestHistoryStorePageUp(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 30; i++ {
		s.Add(HistoryEntry{Role: RoleUser, Content: "entry"})
	}
	s.PageUp(10)
	if s.ScrollOffset() != 10 {
		t.Errorf("expected scroll 10, got %d", s.ScrollOffset())
	}
}

func TestHistoryStorePageDown(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 30; i++ {
		s.Add(HistoryEntry{Role: RoleUser, Content: "entry"})
	}
	s.PageUp(20)
	s.PageDown(10)
	if s.ScrollOffset() != 10 {
		t.Errorf("expected scroll 10, got %d", s.ScrollOffset())
	}
}

// --- App ---

func TestNewApp(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	if app == nil {
		t.Fatal("app should not be nil")
	}
	if app.state != statePermissionSelect {
		t.Error("default state should be permissionSelect")
	}
	if app.screens.screenType != "permission" {
		t.Errorf("permission screen should be initialized, got %q", app.screens.screenType)
	}
}

func TestAppNewSessionReplacesHistoryEverywhere(t *testing.T) {
	app := NewApp(nil, "old-session", false, "")
	app.history.Add(HistoryEntry{Role: RoleUser, Content: "old conversation"})

	app.handleNewSession()

	if app.history.Len() != 0 {
		t.Fatal("new session should clear existing history")
	}
	if app.chat.history != app.history {
		t.Fatal("chat component should reference the replacement history store")
	}
	if app.sessionID == "old-session" || app.sessionID == "" {
		t.Fatalf("new session should receive a fresh id, got %q", app.sessionID)
	}
}

func TestAppWidth(t *testing.T) {
	app := NewApp(nil, "", false, "")
	if app.Width() != 80 {
		t.Error("default width should be 80")
	}
	app.SetWidth(120)
	if app.Width() != 120 {
		t.Error("width should be 120 after set")
	}
}

func TestAppHandleTick(t *testing.T) {
	app := NewApp(nil, "", false, "")
	shouldExit := app.HandleEvent("tick")
	if shouldExit {
		t.Error("tick should not exit")
	}
}

func TestAppHandleStreamText(t *testing.T) {
	app := NewApp(nil, "", false, "")
	app.HandleEvent(StreamTextMsg{Text: "hello"})
	if app.streamedText != "hello" {
		t.Errorf("expected 'hello', got %q", app.streamedText)
	}
	if app.state != stateStreaming {
		t.Error("state should be streaming")
	}
}

func TestAppHandleConfirmation(t *testing.T) {
	app := NewApp(nil, "", false, "")
	app.HandleEvent(ConfirmationRequiredMsg{Prompt: "Allow?"})
	if app.state != stateConfirming {
		t.Error("state should be confirming")
	}
}

func TestAppHandleCtrlCExit(t *testing.T) {
	app := NewApp(nil, "", false, "")
	app.state = statePermissionSelect
	shouldExit := app.handleKey(Key{Type: KeyCtrlC})
	if !shouldExit {
		t.Error("Ctrl+C in permission select should exit")
	}
}

func TestAppCtrlCCreatesFreshExecutionContext(t *testing.T) {
	app := NewApp(nil, "", false, "")
	app.state = stateThinking
	oldCtx := app.ctx

	if shouldExit := app.handleKey(Key{Type: KeyCtrlC}); shouldExit {
		t.Fatal("Ctrl+C during execution should cancel the round, not exit the TUI")
	}
	if oldCtx.Err() == nil {
		t.Fatal("Ctrl+C should cancel the active execution context")
	}
	if app.ctx == oldCtx || app.ctx.Err() != nil {
		t.Fatal("Ctrl+C should prepare a fresh context for the next prompt")
	}
}

func TestAppCursorTracksWrappedInput(t *testing.T) {
	app := NewApp(nil, "", false, "")
	app.state = statePrompt
	app.width = 10
	app.height = 20
	app.focus.Take(FocusPrompt)
	app.focus.Buffer = []rune("abcdefghijklmnop")
	app.focus.CursorIndex = len(app.focus.Buffer)

	lines := app.Render()

	if app.cursorRow < 0 || app.cursorRow >= len(lines) {
		t.Fatalf("cursor row %d outside rendered frame of %d lines", app.cursorRow, len(lines))
	}
	if app.cursorCol < 1 || app.cursorCol > app.width {
		t.Fatalf("cursor col %d outside terminal width %d", app.cursorCol, app.width)
	}
}

func TestAppModeToPermMode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"plan", "plan"},
		{"auto", "auto"},
		{"default", "default"},
		{"Plan Mode (Read-only)", "plan"},
		{"Auto Mode (Automated)", "auto"},
	}
	for _, tt := range tests {
		result := modeToPermMode(tt.input)
		if string(result) != tt.expected {
			t.Errorf("modeToPermMode(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestAppActiveComponents(t *testing.T) {
	app := NewApp(nil, "", false, "")
	comps := app.activeComponents()
	if len(comps) != 4 {
		t.Errorf("expected 4 active components, got %d", len(comps))
	}
}

func TestAppHandleEventStartupPrompt(t *testing.T) {
	app := NewApp(nil, "", false, "")
	// StartupPromptMsg with empty prompt should do nothing
	shouldExit := app.HandleEvent(StartupPromptMsg{Prompt: ""})
	if shouldExit {
		t.Error("should not exit")
	}
}

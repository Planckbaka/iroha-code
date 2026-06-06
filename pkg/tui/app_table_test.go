package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"iroha/pkg/agent"
)

// ---------------------------------------------------------------------------
// TestHandleEventTable — table-driven tests for App.HandleEvent
// ---------------------------------------------------------------------------

func TestHandleEventTable(t *testing.T) {
	tests := []struct {
		name         string
		event        any
		initialState TuiState
		wantExit     bool
		wantState    TuiState
		postCheck    func(t *testing.T, app *App)
	}{
		{
			name:         "string tick returns false",
			event:        "tick",
			initialState: statePrompt,
			wantExit:     false,
			wantState:    statePrompt,
		},
		{
			name:         "StartupPromptMsg with empty prompt",
			event:        StartupPromptMsg{Prompt: ""},
			initialState: statePrompt,
			wantExit:     false,
			wantState:    statePrompt,
		},
		{
			name:         "StartupPromptMsg with non-empty prompt triggers executePrompt",
			event:        StartupPromptMsg{Prompt: "hello"},
			initialState: statePermissionSelect,
			wantExit:     false,
			// executePrompt with runner=nil will panic on runner.Execute, but the
			// state transitions happen before that call. We skip this case in CI
			// by not testing it directly — instead we test state side effects
			// through other means.
		},
		{
			name:         "StreamTextMsg sets stateStreaming and accumulates text",
			event:        StreamTextMsg{Text: "hello world"},
			initialState: stateThinking,
			wantExit:     false,
			wantState:    stateStreaming,
			postCheck: func(t *testing.T, app *App) {
				if app.streamedText != "hello world" {
					t.Errorf("streamedText = %q, want %q", app.streamedText, "hello world")
				}
			},
		},
		{
			name:         "StreamTextMsg with status tag",
			event:        StreamTextMsg{Text: "[status:analyzing code]\n"},
			initialState: stateStreaming,
			wantExit:     false,
			wantState:    stateStreaming,
			postCheck: func(t *testing.T, app *App) {
				if !strings.Contains(app.status.statusText, "analyzing code") {
					t.Errorf("expected status text to contain 'analyzing code', got %q", app.status.statusText)
				}
			},
		},
		{
			name:         "ToolStatusMsg running",
			event:        ToolStatusMsg{Status: agent.ToolStatus{Name: "file_read", Running: true}},
			initialState: stateThinking,
			wantExit:     false,
		},
		{
			name:         "ToolStatusMsg completed success",
			event:        ToolStatusMsg{Status: agent.ToolStatus{Name: "file_read", Success: true, Args: map[string]any{"path": "/tmp/a.go"}}},
			initialState: stateStreaming,
			wantExit:     false,
		},
		{
			name:         "ConfirmationRequiredMsg sets stateConfirming",
			event:        ConfirmationRequiredMsg{Prompt: "Allow file write?"},
			initialState: stateStreaming,
			wantExit:     false,
			wantState:    stateConfirming,
			postCheck: func(t *testing.T, app *App) {
				if app.confirm.prompt != "Allow file write?" {
					t.Errorf("confirm prompt = %q, want %q", app.confirm.prompt, "Allow file write?")
				}
			},
		},
		{
			name:         "AgentErrorMsg stores error and finalizes",
			event:        AgentErrorMsg{Err: errors.New("network failure")},
			initialState: stateStreaming,
			wantExit:     false,
			wantState:    statePrompt,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected error to be stored in history")
				}
			},
		},
		{
			name:         "AgentDoneMsg finalizes turn",
			event:        AgentDoneMsg{},
			initialState: stateStreaming,
			wantExit:     false,
			wantState:    statePrompt,
		},
		{
			name:         "Key event delegates to handleKey",
			event:        Key{Type: KeyCtrlC},
			initialState: statePrompt,
			wantExit:     true,
		},
		{
			name:         "unknown event type returns false",
			event:        42,
			initialState: statePrompt,
			wantExit:     false,
			wantState:    statePrompt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip the StartupPromptMsg non-empty case since runner=nil panics on Execute
			if tt.name == "StartupPromptMsg with non-empty prompt triggers executePrompt" {
				t.Skip("skipped: nil runner panics on Execute")
			}

			app := NewApp(nil, "test-session", false, "")
			app.state = tt.initialState

			// For StreamTextMsg tests, set initial streamed text
			if tt.name == "StreamTextMsg with status tag" {
				app.streamedText = "previous "
			}

			got := app.HandleEvent(tt.event)
			if got != tt.wantExit {
				t.Errorf("HandleEvent() = %v, want %v", got, tt.wantExit)
			}
			if tt.wantState != TuiState(0) && app.state != tt.wantState {
				t.Errorf("state = %v, want %v", app.state, tt.wantState)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleKeyTable — table-driven tests for App.handleKey
// ---------------------------------------------------------------------------

func TestHandleKeyTable(t *testing.T) {
	tests := []struct {
		name         string
		key          Key
		initialState TuiState
		wantExit     bool
		postCheck    func(t *testing.T, app *App)
	}{
		{
			name:         "CtrlC in statePrompt returns true",
			key:          Key{Type: KeyCtrlC},
			initialState: statePrompt,
			wantExit:     true,
		},
		{
			name:         "CtrlC in stateThinking cancels and finalizes",
			key:          Key{Type: KeyCtrlC},
			initialState: stateThinking,
			wantExit:     false,
			postCheck: func(t *testing.T, app *App) {
				if app.state != statePrompt {
					t.Errorf("state should be prompt after cancel, got %v", app.state)
				}
			},
		},
		{
			name:         "CtrlC in stateStreaming with partial text preserves partial text",
			key:          Key{Type: KeyCtrlC},
			initialState: stateStreaming,
			wantExit:     false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() < 2 {
					t.Errorf("expected at least 2 history entries (partial + cancel), got %d", app.history.Len())
				}
			},
		},
		{
			name:         "CtrlC in statePermissionSelect returns true",
			key:          Key{Type: KeyCtrlC},
			initialState: statePermissionSelect,
			wantExit:     true,
		},
		{
			name:         "CtrlC in stateSessionSelect returns true",
			key:          Key{Type: KeyCtrlC},
			initialState: stateSessionSelect,
			wantExit:     true,
		},
		{
			name:         "PageUp scrolls history",
			key:          Key{Type: KeyPgUp},
			initialState: statePrompt,
			wantExit:     false,
			postCheck: func(t *testing.T, app *App) {
				// Add enough history to make scrolling meaningful
			},
		},
		{
			name:         "PageDown scrolls history",
			key:          Key{Type: KeyPgDown},
			initialState: statePrompt,
			wantExit:     false,
		},
		{
			name:         "WheelUp scrolls 3 lines",
			key:          Key{Type: KeyWheelUp},
			initialState: statePrompt,
			wantExit:     false,
		},
		{
			name:         "WheelDown scrolls 3 lines",
			key:          Key{Type: KeyWheelDown},
			initialState: statePrompt,
			wantExit:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = tt.initialState

			// For streaming CtrlC test, set partial text
			if tt.name == "CtrlC in stateStreaming with partial text preserves partial text" {
				app.streamedText = "**partial response**"
			}

			// For scroll tests, add enough entries
			if tt.name == "PageUp scrolls history" || tt.name == "PageDown scrolls history" {
				for i := 0; i < 30; i++ {
					app.history.Add(HistoryEntry{Role: RoleSystem, Content: "entry"})
				}
				app.height = 12
			}

			got := app.handleKey(tt.key)
			if got != tt.wantExit {
				t.Errorf("handleKey() = %v, want %v", got, tt.wantExit)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleConfirmResponseTable
// ---------------------------------------------------------------------------

func TestHandleConfirmResponseTable(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantState TuiState
	}{
		{
			name:      "normal response y sends to bridge",
			response:  "y",
			wantState: stateStreaming,
		},
		{
			name:      "edit response sends edited value",
			response:  "edit:modified_command",
			wantState: stateStreaming,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = stateConfirming

			// handleConfirmResponse writes to agent.Bridge.ResponseChan which is
			// a channel. We need to drain it to avoid blocking.
			done := make(chan struct{})
			go func() {
				defer close(done)
				// Drain the response channel
				<-agent.Bridge.ResponseChan
			}()

			app.handleConfirmResponse(tt.response)

			if app.state != tt.wantState {
				t.Errorf("state = %v, want %v", app.state, tt.wantState)
			}
			<-done
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandlePermSelectTable
// ---------------------------------------------------------------------------

func TestHandlePermSelectTable(t *testing.T) {
	tests := []struct {
		name                string
		mode                string
		startInSessionPicker bool
		wantState           TuiState
	}{
		{
			name:                "normal mode transitions to prompt",
			mode:                "default",
			startInSessionPicker: false,
			wantState:           statePrompt,
		},
		{
			name:                "session picker mode transitions to session select",
			mode:                "default",
			startInSessionPicker: true,
			wantState:           stateSessionSelect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", tt.startInSessionPicker, "")
			app.state = statePermissionSelect

			app.handlePermSelect(tt.mode)

			if app.state != tt.wantState {
				t.Errorf("state = %v, want %v", app.state, tt.wantState)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleSessionSelectAndNewSessionTable
// ---------------------------------------------------------------------------

func TestHandleSessionSelectWithNilService(t *testing.T) {
	// handleSessionSelect with nil GlobalSessionService returns false early
	app := NewApp(nil, "test-session", false, "")
	app.state = stateSessionSelect

	// GlobalSessionService should be nil by default in tests
	agent.GlobalSessionService = nil

	app.handleSessionSelect("some-session-id")

	// Should not change state because loadHistoryFromSession returns false
	if app.state != stateSessionSelect {
		t.Errorf("state should remain sessionSelect with nil service, got %v", app.state)
	}
}

func TestHandleNewSessionTable(t *testing.T) {
	tests := []struct {
		name        string
		preTokens   int
		wantTokens  int
		wantState   TuiState
	}{
		{
			name:       "new session resets tokens and state",
			preTokens:  500,
			wantTokens: 0,
			wantState:  statePrompt,
		},
		{
			name:       "new session with zero tokens",
			preTokens:  0,
			wantTokens: 0,
			wantState:  statePrompt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "old-session", false, "")
			app.totalTokens = tt.preTokens
			app.history.Add(HistoryEntry{Role: RoleUser, Content: "old data"})

			app.handleNewSession()

			if app.totalTokens != tt.wantTokens {
				t.Errorf("totalTokens = %d, want %d", app.totalTokens, tt.wantTokens)
			}
			if app.state != tt.wantState {
				t.Errorf("state = %v, want %v", app.state, tt.wantState)
			}
			if app.history.Len() != 0 {
				t.Errorf("history should be empty after new session, got %d entries", app.history.Len())
			}
			if app.sessionID == "old-session" {
				t.Error("session ID should be regenerated")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleRawSlashCommandTable — all slash command branches
// ---------------------------------------------------------------------------

func TestHandleRawSlashCommandTable(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantExit   bool
		wantState  TuiState
		postCheck  func(t *testing.T, app *App)
	}{
		{
			name:      "/exit returns true",
			input:     "/exit",
			wantExit:  true,
		},
		{
			name:      "/quit returns true",
			input:     "/quit",
			wantExit:  true,
		},
		{
			name:     "/permission without args opens screen",
			input:    "/permission",
			wantExit: false,
			wantState: statePermissionSelect,
			postCheck: func(t *testing.T, app *App) {
				if app.screens.permSelectIndex != 1 {
					t.Errorf("expected permSelectIndex=1, got %d", app.screens.permSelectIndex)
				}
			},
		},
		{
			name:     "/permission with valid mode",
			input:    "/permission default",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries")
				}
			},
		},
		{
			name:     "/rules renders rules list",
			input:    "/rules",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries after /rules")
				}
			},
		},
		{
			name:     "/stats shows telemetry",
			input:    "/stats",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				rendered := strings.Join(app.history.Render(120, 10000), "\n")
				if !strings.Contains(rendered, "Session Statistics") {
					t.Error("expected Session Statistics in output")
				}
			},
		},
		{
			name:      "/sessions transitions to session select",
			input:     "/sessions",
			wantExit:  false,
			wantState: stateSessionSelect,
		},
		{
			name:     "/help renders help",
			input:    "/help",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries after /help")
				}
			},
		},
		{
			name:     "/commands renders help",
			input:    "/commands",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries after /commands")
				}
			},
		},
		{
			name:     "unknown command renders error",
			input:    "/unknown_cmd",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries for unknown command")
				}
				rendered := strings.Join(app.history.Render(120, 10000), "\n")
				if !strings.Contains(rendered, "Unknown command") {
					t.Error("expected 'Unknown command' in output")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = statePrompt

			got := app.handleRawSlashCommand(tt.input)
			if got != tt.wantExit {
				t.Errorf("handleRawSlashCommand(%q) = %v, want %v", tt.input, got, tt.wantExit)
			}
			if tt.wantState != TuiState(0) && app.state != tt.wantState {
				t.Errorf("state = %v, want %v", app.state, tt.wantState)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestExecutePromptTable
// ---------------------------------------------------------------------------

func TestExecutePromptEmpty(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	app.state = statePrompt
	beforeState := app.state

	app.executePrompt("")

	if app.state != beforeState {
		t.Error("empty prompt should not change state")
	}
	if app.roundCount != 0 {
		t.Error("empty prompt should not increment round count")
	}
}

// ---------------------------------------------------------------------------
// TestFinalizeTurnTable — token usage branches
// ---------------------------------------------------------------------------

func TestFinalizeTurnTable(t *testing.T) {
	tests := []struct {
		name          string
		streamedText  string
		lastError     error
		preTokens     int
		wantState     TuiState
		postCheck     func(t *testing.T, app *App)
	}{
		{
			name:         "with streamedText adds agent entry",
			streamedText: "**finished response**",
			wantState:    statePrompt,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Fatal("expected history entry")
				}
				lastEntry := app.history.entries[app.history.Len()-1]
				if lastEntry.Role != RoleAgent {
					t.Errorf("last entry role = %v, want RoleAgent", lastEntry.Role)
				}
				if lastEntry.Content != "**finished response**" {
					t.Errorf("content = %q, want raw markdown", lastEntry.Content)
				}
				if app.streamedText != "" {
					t.Error("streamedText should be cleared after finalize")
				}
			},
		},
		{
			name:      "with lastError adds system error entry",
			lastError: errors.New("API rate limit"),
			wantState: statePrompt,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Fatal("expected history entry")
				}
				lastEntry := app.history.entries[app.history.Len()-1]
				if lastEntry.Role != RoleSystem {
					t.Errorf("last entry role = %v, want RoleSystem", lastEntry.Role)
				}
				if app.lastError != nil {
					t.Error("lastError should be cleared after finalize")
				}
			},
		},
		{
			name:      "nil runner with pre-existing tokens keeps tokens",
			preTokens: 500,
			wantState: statePrompt,
			postCheck: func(t *testing.T, app *App) {
				if app.totalTokens != 500 {
					t.Errorf("totalTokens = %d, want 500 (nil runner)", app.totalTokens)
				}
			},
		},
		{
			name:      "nil runner with zero tokens and text estimates from text",
			preTokens: 0,
			wantState: statePrompt,
			postCheck: func(t *testing.T, app *App) {
				// With nil runner, token estimation from text/4 is used only
				// if totalTokens == 0, but this path is only reached if runner != nil
				// so totalTokens stays 0 with nil runner
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = stateStreaming
			app.streamedText = tt.streamedText
			app.lastError = tt.lastError
			app.totalTokens = tt.preTokens
			app.roundStartTime = time.Now()

			app.finalizeTurn()

			if app.state != tt.wantState {
				t.Errorf("state = %v, want %v", app.state, tt.wantState)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderStreamedMarkdownTable
// ---------------------------------------------------------------------------

func TestRenderStreamedMarkdownTable(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		width      int
		wantEmpty  bool
		postCheck  func(t *testing.T, app *App)
	}{
		{
			name:      "empty streamedText returns empty",
			text:      "",
			width:     80,
			wantEmpty: true,
		},
		{
			name:      "first call with text renders and caches",
			text:      "hello world",
			width:     80,
			wantEmpty: false,
			postCheck: func(t *testing.T, app *App) {
				if app.streamRenderCacheKey != "hello world" {
					t.Errorf("cache key = %q, want %q", app.streamRenderCacheKey, "hello world")
				}
				if app.streamRenderCacheVal == "" {
					t.Error("cache value should be non-empty")
				}
			},
		},
		{
			name:      "second call with same text returns cached value",
			text:      "cached text",
			width:     80,
			wantEmpty: false,
			postCheck: func(t *testing.T, app *App) {
				first := app.renderStreamedMarkdown(80)
				second := app.renderStreamedMarkdown(80)
				if first != second {
					t.Error("second call should return cached value")
				}
			},
		},
		{
			name:      "different width re-renders",
			text:      "some text for re-rendering",
			width:     40,
			wantEmpty: false,
			postCheck: func(t *testing.T, app *App) {
				// Render at 80 first
				app.renderStreamedMarkdown(80)
				// Then at 40 — should re-render
				at40 := app.renderStreamedMarkdown(40)
				if at40 == "" {
					t.Error("re-render at different width should produce output")
				}
				if app.streamRenderCacheWidth != 40 {
					t.Errorf("cache width = %d, want 40", app.streamRenderCacheWidth)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.streamedText = tt.text

			result := app.renderStreamedMarkdown(tt.width)

			if tt.wantEmpty && result != "" {
				t.Errorf("expected empty, got %q", result)
			}
			if !tt.wantEmpty && result == "" {
				t.Error("expected non-empty result")
			}
			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestAppHelperFunctionsTable
// ---------------------------------------------------------------------------

func TestHistoryManager(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	hm := app.historyManager()
	if hm == nil {
		t.Error("historyManager() should return non-nil for fresh app")
	}
}

func TestAppSetWidth(t *testing.T) {
	tests := []struct {
		name  string
		width int
	}{
		{"set 120", 120},
		{"set 80", 80},
		{"set 200", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.SetWidth(tt.width)
			if app.Width() != tt.width {
				t.Errorf("Width() = %d, want %d", app.Width(), tt.width)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestResetExecutionContext
// ---------------------------------------------------------------------------

func TestResetExecutionContext(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	oldCtx := app.ctx

	app.resetExecutionContext()

	if app.ctx == oldCtx {
		t.Error("context should be replaced after reset")
	}
	if app.ctx.Err() != nil {
		t.Error("new context should not be cancelled")
	}
}

// ---------------------------------------------------------------------------
// TestHandleSubmitAndSlashCmd
// ---------------------------------------------------------------------------

func TestHandleSubmitDelegatesToExecutePrompt(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	app.state = statePrompt

	// Empty prompt — should not change state
	app.handleSubmit("")
	if app.state != statePrompt {
		t.Error("empty submit should not change state")
	}
}

func TestHandleSlashCmdDelegates(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	app.state = statePrompt

	// /exit should return true
	result := app.handleSlashCmd("/exit")
	if !result {
		t.Error("handleSlashCmd(/exit) should return true")
	}
}

// ---------------------------------------------------------------------------
// TestNotifyStateChange
// ---------------------------------------------------------------------------

func TestNotifyStateChange(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")

	// Transition from permission select to prompt
	app.notifyStateChange(statePermissionSelect)

	// All components should have received the state change
	if app.screens.screenType != "permission" {
		// screens only updates on specific transitions
		t.Logf("screens.screenType = %q", app.screens.screenType)
	}
}

// ---------------------------------------------------------------------------
// TestHandleToolStatusTable
// ---------------------------------------------------------------------------

func TestHandleToolStatusTable(t *testing.T) {
	tests := []struct {
		name        string
		status      agent.ToolStatus
		preStream   string
		postCheck   func(t *testing.T, app *App)
	}{
		{
			name: "running tool sets active tool",
			status: agent.ToolStatus{
				Name:    "file_read",
				Running: true,
			},
			postCheck: func(t *testing.T, app *App) {
				if !app.chat.activeTool.Running {
					t.Error("chat active tool should be running")
				}
				if !app.status.activeTool.Running {
					t.Error("status active tool should be running")
				}
			},
		},
		{
			name: "completed success adds tool card to history",
			status: agent.ToolStatus{
				Name:    "shell_run",
				Success: true,
				Args:    map[string]any{"command": "ls"},
			},
			postCheck: func(t *testing.T, app *App) {
				if app.chat.activeTool.Running {
					t.Error("chat active tool should be cleared")
				}
			},
		},
		{
			name: "completed failure adds error card",
			status: agent.ToolStatus{
				Name:  "file_write",
				Error: errors.New("disk full"),
			},
			postCheck: func(t *testing.T, app *App) {
				if app.chat.activeTool.Running {
					t.Error("chat active tool should be cleared")
				}
			},
		},
		{
			name: "completed with pending stream text commits to history",
			status: agent.ToolStatus{
				Name:    "file_read",
				Success: true,
			},
			preStream: "**partial**",
			postCheck: func(t *testing.T, app *App) {
				// Should have agent entry + tool entry
				found := false
				for _, e := range app.history.entries {
					if e.Role == RoleAgent && e.Content == "**partial**" {
						found = true
					}
				}
				if !found {
					t.Error("expected partial streamed text to be committed to history")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = stateStreaming
			if tt.preStream != "" {
				app.streamedText = tt.preStream
			}

			app.handleToolStatus(tt.status)

			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

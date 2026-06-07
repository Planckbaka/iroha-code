package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"iroha/pkg/agent"
	"iroha/pkg/config"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"golang.org/x/term"
	"google.golang.org/adk/session"
)

// App orchestrates all TUI components, dispatches events, and collects renders.
type App struct {
	state  TuiState
	width  int
	height int

	// Core components
	chat    *ChatComponent
	input   *InputComponent
	confirm *ConfirmComponent
	status  *StatusBarComponent
	slash   *SlashMenuComponent
	screens *ScreenComponent

	// Supporting services
	focus   *FocusModel
	history *HistoryStore

	// External interfaces
	runner *agent.CustomRunner

	// Session context
	ctx       context.Context
	cancel    context.CancelFunc
	sessionID string

	// Bridge callbacks (wired for agent communication)
	OnEvent func(*session.Event)
	OnError func(error)
	OnDone  func()

	// Telemetry
	totalTokens      int
	totalSessionCost float64
	roundCount       int
	roundStartTime   time.Time
	sessionStartTime time.Time

	// Stream state
	streamedText string
	renderedText string
	// streamRenderCache memoizes the Glamour render of streamedText so the
	// expensive CommonMark parse only runs when the text actually changes,
	// not on every tick/keystroke during streaming.
	streamRenderCacheKey   string
	streamRenderCacheWidth int
	streamRenderCacheVal   string
	currentPrompt          string
	lastError              error
	lastRawResp            string

	// Startup
	startInSessionPicker bool
	startupPrompt        string

	// Cursor coordinates
	cursorRow int
	cursorCol int
}

// NewApp creates and wires all components.
func NewApp(runner *agent.CustomRunner, sessionID string, startInSessionPicker bool, startupPrompt string) *App {
	ctx, cancel := context.WithCancel(context.Background())

	focus := &FocusModel{Owner: FocusNone}
	history := NewHistoryStore()
	histMgr := NewHistoryManager()

	app := &App{
		state:                statePermissionSelect,
		width:                80,
		height:               24,
		runner:               runner,
		ctx:                  ctx,
		cancel:               cancel,
		sessionID:            sessionID,
		startInSessionPicker: startInSessionPicker,
		startupPrompt:        startupPrompt,
		sessionStartTime:     time.Now(),
		focus:                focus,
		history:              history,
	}

	// Create components
	app.chat = NewChatComponent(history)
	app.input = NewInputComponent(focus, histMgr)
	app.confirm = NewConfirmComponent()
	app.status = NewStatusBarComponent()
	app.slash = NewSlashMenuComponent(AllSlashCommands)
	app.screens = NewScreenComponent()

	// Wire slash menu into input
	app.input.SetSlashMenu(app.slash)

	// Wire callbacks
	app.input.OnSubmit = app.handleSubmit
	app.input.OnSlashCmd = app.handleSlashCmd
	app.confirm.OnRespond = app.handleConfirmResponse
	app.screens.OnPermSelect = app.handlePermSelect
	app.screens.OnSessionSelect = app.handleSessionSelect
	app.screens.OnNewSession = app.handleNewSession

	// Initialize components that derive internal state from the App state.
	app.notifyStateChange(app.state)

	return app
}

// HandleEvent dispatches events to the appropriate handler.
func (a *App) HandleEvent(event any) bool {
	switch msg := event.(type) {
	case string:
		return false // tick — just redraw
	case StartupPromptMsg:
		a.executePrompt(msg.Prompt)
		return false
	case StreamTextMsg:
		a.state = stateStreaming
		a.streamedText += msg.Text
		// Only scan the new chunk for status tags to avoid O(n) regex on the
		// full accumulated text on every streaming tick.
		matches := statusTagRe.FindAllStringSubmatch(msg.Text, -1)
		if len(matches) == 0 {
			// Fallback: check a small tail window for tags that may span
			// chunk boundaries.
			checkStart := len(a.streamedText) - len(msg.Text) - 50
			if checkStart < 0 {
				checkStart = 0
			}
			matches = statusTagRe.FindAllStringSubmatch(a.streamedText[checkStart:], 1)
		}
		if len(matches) > 0 {
			a.status.SetStatusText(matches[len(matches)-1][1])
		}
		return false
	case ToolStatusMsg:
		a.handleToolStatus(msg.Status)
		return false
	case ConfirmationRequiredMsg:
		old := a.state
		a.state = stateConfirming
		a.confirm.SetPrompt(msg.Prompt)
		a.confirm.activeToolArgs = a.chat.activeTool.Args
		a.notifyStateChange(old)
		return false
	case AgentErrorMsg:
		a.lastError = msg.Err
		a.finalizeTurn()
		return false
	case AgentDoneMsg:
		a.finalizeTurn()
		return false
	case Key:
		return a.handleKey(msg)
	}
	return false
}

// handleKey dispatches key events to the active component.
func (a *App) handleKey(k Key) bool {
	if k.Type == KeyCtrlC {
		if a.state == statePermissionSelect || a.state == stateSessionSelect {
			return true
		}
		if a.state != statePrompt {
			a.cancel()
			a.resetExecutionContext()
			elapsed := time.Duration(0)
			if !a.roundStartTime.IsZero() {
				elapsed = time.Since(a.roundStartTime)
			}
			if a.streamedText != "" {
				a.history.Add(HistoryEntry{Role: RoleAgent, Content: a.streamedText})
				a.streamedText = ""
			}
			a.history.Add(HistoryEntry{Role: RoleSystem, Content: RenderCancelCard(elapsed)})
			a.finalizeTurn()
			return false
		}
		return true
	}

	// Viewport scrolling (PageUp/PageDown)
	if k.Type == KeyPgUp {
		pageLines := a.height - 6 // reserve lines for chrome
		if pageLines <= 0 {
			pageLines = 20
		}
		a.history.PageUp(pageLines)
		return false
	}
	if k.Type == KeyPgDown {
		pageLines := a.height - 6
		if pageLines <= 0 {
			pageLines = 20
		}
		a.history.PageDown(pageLines)
		return false
	}
	if k.Type == KeyWheelUp {
		a.history.ScrollUp(3)
		return false
	}
	if k.Type == KeyWheelDown {
		a.history.ScrollDown(3)
		return false
	}

	// Dispatch to active component
	for _, comp := range a.activeComponents() {
		if comp.Active(a.state) && comp.HandleInput(k) {
			return false
		}
	}
	return false
}

// activeComponents returns components in priority order for input dispatch.
func (a *App) activeComponents() []Component {
	return []Component{
		a.confirm,
		a.input,
		a.slash,
		a.screens,
	}
}

// renderStreamedMarkdown returns the Glamour-rendered form of the current
// streamedText, memoized so the parse only runs when the text changes. During
// streaming this is called on every tick, so caching avoids redundant CPU work.
func (a *App) renderStreamedMarkdown(width int) string {
	if a.streamedText == "" {
		return ""
	}
	if a.streamRenderCacheKey == a.streamedText && a.streamRenderCacheWidth == width {
		return a.streamRenderCacheVal
	}
	rendered := RenderMarkdownWithWidth(a.streamedText, width)
	a.streamRenderCacheKey = a.streamedText
	a.streamRenderCacheWidth = width
	a.streamRenderCacheVal = rendered
	return rendered
}

// Render collects output from all components.
func (a *App) Render() []string {
	a.cursorRow = -1
	a.cursorCol = 0

	// Full-screen overlays
	if a.screens.Active(a.state) {
		return a.screens.Render(a.width)
	}

	var topLines []string

	// 1. Dashboards
	if todo := RenderTodoDashboard(); todo != "" {
		topLines = append(topLines, strings.Split(strings.TrimRight(todo, "\n"), "\n")...)
	}
	if task := RenderTaskDashboard(); task != "" {
		topLines = append(topLines, strings.Split(strings.TrimRight(task, "\n"), "\n")...)
	}

	var welcomeLines []string
	if a.history.Len() == 0 && a.state == statePrompt {
		welcomeLines = strings.Split(strings.TrimRight(RenderWelcomeCard(a.runner), "\n"), "\n")
	}

	streamRendered := a.renderedText
	if a.streamedText != "" {
		streamRendered = a.renderStreamedMarkdown(max(1, a.width-2))
	}
	activeLines := a.chat.RenderTail(a.state, a.width, "", streamRendered, welcomeLines, a.confirm.Render(a.width))

	// 3. Fixed input chrome
	var bottomLines []string
	bottomLines = append(bottomLines, lipgloss.NewStyle().Foreground(ColorSecondary).Render(strings.Repeat("─", max(1, a.width))))

	if slashLines := a.slash.Render(a.width); len(slashLines) > 0 {
		inputLines := a.input.Render(a.width)
		statusLines := a.status.Render(a.width)
		menuBudget := max(0, a.height-len(topLines)-len(bottomLines)-len(inputLines)-len(statusLines)-1)
		if len(slashLines) > menuBudget {
			start := min(max(0, a.slash.index-menuBudget+1), len(slashLines)-menuBudget)
			slashLines = slashLines[start : start+menuBudget]
		}
		bottomLines = append(bottomLines, slashLines...)
	}

	inputStartRow := len(bottomLines)
	bottomLines = append(bottomLines, a.input.Render(a.width)...)

	if a.state == statePrompt {
		promptPrefix := "┃ "
		prefixWidth := lipgloss.Width(promptPrefix)
		cursorIdx := a.input.focus.CursorIndex
		if cursorIdx > len(a.input.focus.Buffer) {
			cursorIdx = len(a.input.focus.Buffer)
		}
		if cursorIdx < 0 {
			cursorIdx = 0
		}
		beforeCursor := a.input.focus.Buffer[:cursorIdx]
		linesBefore := WrapInput(string(beforeCursor), prefixWidth, a.width)
		if len(linesBefore) == 0 {
			linesBefore = []string{""}
		}
		cursorLineIdx := len(linesBefore) - 1

		a.cursorCol = min(a.width, prefixWidth+lipgloss.Width(linesBefore[cursorLineIdx])+1)
		a.cursorRow = inputStartRow + cursorLineIdx
	}

	bottomLines = append(bottomLines, a.status.Render(a.width)...)

	viewportLines := a.height - len(topLines) - len(bottomLines)
	if viewportLines < 1 {
		viewportLines = 1
	}
	timeline := a.history.RenderWithTail(a.width, viewportLines, activeLines)

	lines := make([]string, 0, len(topLines)+len(timeline)+len(bottomLines))
	lines = append(lines, topLines...)
	lines = append(lines, timeline...)
	if a.cursorRow >= 0 {
		a.cursorRow += len(topLines) + len(timeline)
	}
	lines = append(lines, bottomLines...)

	return lines
}

// notifyStateChange propagates state transitions to all components.
// Callers must pass the state BEFORE the transition so components can
// detect the actual change (e.g. InputComponent only grabs focus when
// transitioning INTO statePrompt).
func (a *App) notifyStateChange(oldState TuiState) {
	for _, comp := range []Component{a.chat, a.input, a.confirm, a.status, a.slash, a.screens} {
		comp.OnStateChange(oldState, a.state)
	}
}

// Callback implementations

func (a *App) handleSubmit(prompt string) {
	a.executePrompt(prompt)
}

func (a *App) handleSlashCmd(cmd string) bool {
	return a.handleRawSlashCommand(cmd)
}

func (a *App) handleConfirmResponse(response string) {
	if strings.HasPrefix(response, "edit:") {
		editedVal := strings.TrimPrefix(response, "edit:")
		agent.Bridge.ResponseChan <- editedVal
	} else {
		agent.Bridge.ResponseChan <- response
	}
	old := a.state
	a.state = stateStreaming
	a.notifyStateChange(old)
}

func (a *App) handlePermSelect(mode string) {
	if err := agent.GlobalPermissionManager.SetMode(modeToPermMode(mode)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to set permission mode: %v\n", err)
	}
	old := a.state
	if a.startInSessionPicker {
		a.state = stateSessionSelect
		a.loadSessionsList()
	} else {
		a.state = statePrompt
	}
	a.notifyStateChange(old)
}

func (a *App) handleSessionSelect(sessionID string) {
	if !a.loadHistoryFromSession(sessionID) {
		return
	}
	a.sessionID = sessionID
	old := a.state
	a.state = statePrompt
	a.notifyStateChange(old)
}

func (a *App) handleNewSession() {
	a.sessionID = uuid.New().String()
	a.replaceHistory(NewHistoryStore())
	a.totalTokens = 0
	old := a.state
	a.state = statePrompt
	a.notifyStateChange(old)
}

// executePrompt starts an agent round.
func (a *App) executePrompt(prompt string) {
	if prompt == "" {
		return
	}
	a.currentPrompt = prompt
	a.streamedText = ""
	a.renderedText = ""
	a.streamRenderCacheKey = ""
	a.streamRenderCacheWidth = 0
	a.streamRenderCacheVal = ""
	a.lastError = nil
	a.state = stateThinking
	a.roundCount++
	a.roundStartTime = time.Now()
	a.chat.ResetStream()
	a.status.SetRoundStart(time.Now())

	// Add user message to history
	a.history.Add(HistoryEntry{Role: RoleUser, Content: prompt})
	if a.input.history != nil {
		a.input.history.Add(prompt)
	}

	a.notifyStateChange(statePrompt)

	a.runner.Execute(a.ctx, "user-dev", a.sessionID, a.currentPrompt,
		a.OnEvent, a.OnError, a.OnDone,
	)
}

// handleToolStatus processes tool status updates.
func (a *App) handleToolStatus(status agent.ToolStatus) {
	if status.Running {
		// Preserve and accumulate streamed stdout history
		if a.chat.activeTool.Running && a.chat.activeTool.Name == status.Name {
			status.StreamLines = append(a.chat.activeTool.StreamLines, status.StreamLines...)
		}
		a.chat.SetActiveTool(status)
		a.status.SetActiveTool(status)
		if a.roundStartTime.IsZero() {
			a.roundStartTime = time.Now()
		}
	} else {
		a.chat.SetActiveTool(agent.ToolStatus{})
		a.status.SetActiveTool(agent.ToolStatus{})
		var logLine string
		if status.Success {
			logLine = "\n" + RenderToolSuccessCard(status.Name, status.Args, status.Duration)
		} else {
			logLine = "\n\n" + RenderToolErrorCard(status.Name, status.Args, status.Duration, status.Error)
		}

		if a.streamedText != "" {
			a.history.Add(HistoryEntry{Role: RoleAgent, Content: a.streamedText})
			a.streamedText = ""
		}
		a.history.Add(HistoryEntry{Role: RoleTool, Content: logLine})
	}
}

// finalizeTurn completes an agent round.
func (a *App) finalizeTurn() {
	if !a.roundStartTime.IsZero() {
		a.roundStartTime = time.Time{}
	}
	a.status.SetActiveTool(agent.ToolStatus{})
	a.status.SetStatusText("")
	a.renderedText = ""
	a.status.SetGoalMode(false, "")

	if a.runner != nil {
		usage := a.runner.GetTokenUsage()
		if usage > 0 {
			a.totalTokens = usage
		} else if a.totalTokens == 0 {
			a.totalTokens = len(a.streamedText) / 4
		}
		a.totalSessionCost = config.EstimateCost(a.runner.ModelName(), a.totalTokens)
	}
	a.status.SetTokenUsage(a.totalTokens, a.totalSessionCost)

	// Add agent response to history
	if a.lastError != nil {
		a.history.Add(HistoryEntry{Role: RoleSystem, Content: RenderErrorCard(a.lastError)})
		a.lastError = nil
	} else if a.streamedText != "" {
		a.lastRawResp = a.streamedText
		a.history.Add(HistoryEntry{Role: RoleAgent, Content: a.streamedText})
		a.streamedText = ""
	}

	a.input.Clear()
	old := a.state
	a.state = statePrompt
	a.notifyStateChange(old)
}

// loadSessionsList loads sessions for the picker screen.
func (a *App) loadSessionsList() {
	if agent.GlobalSessionService == nil {
		return
	}
	list, err := agent.GlobalSessionService.ListSavedSessions()
	if err != nil {
		return
	}
	var entries []SessionEntry
	for _, s := range list {
		summary := s.FirstPrompt
		if len(summary) > 40 {
			summary = summary[:37] + "..."
		}
		entries = append(entries, SessionEntry{
			ID:            s.ID,
			LastUpdateStr: s.LastUpdateTime.Format("2006-01-02 15:04:05"),
			TotalTokens:   s.TotalTokens,
			TotalCost:     s.TotalCost,
			LastMsg:       summary,
		})
	}
	a.screens.SetSessions(entries)
}

// loadHistoryFromSession replaces the timeline with a previous session.
func (a *App) loadHistoryFromSession(sessionID string) bool {
	if agent.GlobalSessionService == nil {
		return false
	}
	resp, err := agent.GlobalSessionService.Get(context.Background(), &session.GetRequest{
		SessionID: sessionID,
	})
	if err != nil || resp.Session == nil {
		return false
	}

	var events []*session.Event
	if resp.Session.Events() != nil {
		for ev := range resp.Session.Events().All() {
			events = append(events, ev)
		}
	}

	type turn struct {
		prompt   string
		response string
	}
	var turns []turn
	var currentTurn *turn

	for _, ev := range events {
		if ev == nil {
			continue
		}
		if ev.Content != nil {
			var promptParts []string
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					promptParts = append(promptParts, part.Text)
				}
			}
			if len(promptParts) > 0 {
				pText := strings.Join(promptParts, "\n")
				if currentTurn != nil {
					turns = append(turns, *currentTurn)
				}
				currentTurn = &turn{prompt: pText}
			}
		}

		if ev.LLMResponse.Content != nil {
			var respParts []string
			for _, part := range ev.LLMResponse.Content.Parts {
				if part.Text != "" {
					respParts = append(respParts, part.Text)
				}
			}
			if len(respParts) > 0 {
				rText := strings.Join(respParts, "")
				if currentTurn == nil {
					currentTurn = &turn{}
				}
				currentTurn.response += rText
			}
		}
	}
	if currentTurn != nil {
		turns = append(turns, *currentTurn)
	}

	loaded := NewHistoryStore()
	for _, t := range turns {
		loaded.Add(HistoryEntry{Role: RoleUser, Content: t.prompt})
		if t.response != "" {
			loaded.Add(HistoryEntry{Role: RoleAgent, Content: t.response})
		}
	}
	a.replaceHistory(loaded)
	return true
}

func (a *App) replaceHistory(history *HistoryStore) {
	a.history = history
	a.chat.SetHistory(history)
}

func (a *App) resetExecutionContext() {
	a.ctx, a.cancel = context.WithCancel(context.Background())
}

// Width returns current terminal width.
func (a *App) Width() int { return a.width }

// SetWidth updates the terminal width.
func (a *App) SetWidth(w int) { a.width = w }

// historyManager returns the legacy history manager (used by slash commands).
// This is a temporary bridge during migration.
func (a *App) historyManager() *HistoryManager {
	return a.input.history
}

// Helper functions

func modeToPermMode(label string) agent.PermissionMode {
	switch strings.ToLower(label) {
	case "plan mode (read-only)", "plan":
		return agent.ModePlan
	case "auto mode (automated)", "auto":
		return agent.ModeAuto
	default:
		return agent.ModeDefault
	}
}

// UpdateWidth refreshes terminal dimensions.
func (a *App) UpdateWidth() {
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		a.width = w
		a.height = h
	} else {
		a.width = 80
		a.height = 24
	}
}

// RunApp is the new entry point that uses App instead of Model.
func RunApp(runner *agent.CustomRunner, sessionID string, startInSessionPicker bool, initialMode agent.PermissionMode, startupPrompt string) error {
	app := NewApp(runner, sessionID, startInSessionPicker, startupPrompt)
	defer app.cancel()
	renderer := NewRawRenderer(os.Stdout)
	if mouseTrackingEnabled() {
		enableMouseTracking(os.Stdout)
		defer disableMouseTracking(os.Stdout)
	}
	eventChan := make(chan any, 256)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Apply initial mode
	if initialMode != "" {
		if err := agent.GlobalPermissionManager.SetMode(initialMode); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to set permission mode: %v\n", err)
		}
		old := app.state
		if startInSessionPicker {
			app.state = stateSessionSelect
		} else {
			app.state = statePrompt
		}
		app.notifyStateChange(old)
	}

	// Load session history
	if sessionID != "" && !startInSessionPicker {
		app.loadHistoryFromSession(sessionID)
	}

	// Thread-safe callbacks
	app.OnEvent = func(ev *session.Event) {
		if ev != nil && ev.LLMResponse.Content != nil {
			for _, part := range ev.LLMResponse.Content.Parts {
				if part.Text != "" {
					eventChan <- StreamTextMsg{Text: part.Text}
				}
			}
		}
	}
	app.OnError = func(err error) {
		eventChan <- AgentErrorMsg{Err: err}
	}
	app.OnDone = func() {
		eventChan <- AgentDoneMsg{}
	}

	// Keyboard input
	go func() {
		_ = ReadRawKeys(ctx, func(k Key) bool {
			eventChan <- k
			return true
		})
	}()

	// Bridge channels
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case prompt := <-agent.Bridge.PromptChan:
				eventChan <- ConfirmationRequiredMsg{Prompt: prompt}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case status := <-agent.ToolBridge.StatusChan:
				eventChan <- ToolStatusMsg{Status: status}
			}
		}
	}()

	// Spinner ticker
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				eventChan <- "tick"
			}
		}
	}()

	app.UpdateWidth()
	renderer.Draw(app.Render(), app.cursorRow, app.cursorCol)

	if app.startupPrompt != "" {
		eventChan <- StartupPromptMsg{Prompt: app.startupPrompt}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-eventChan:
			shouldExit := app.HandleEvent(ev)
			if shouldExit {
				renderer.Reset()
				return nil
			}
			app.UpdateWidth()
			renderer.Draw(app.Render(), app.cursorRow, app.cursorCol)
		}
	}
}

func enableMouseTracking(out io.Writer) {
	_, _ = fmt.Fprint(out, "\x1b[?1000h\x1b[?1006h")
}

func disableMouseTracking(out io.Writer) {
	_, _ = fmt.Fprint(out, "\x1b[?1006l\x1b[?1000l")
}

func mouseTrackingEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("IROHA_ENABLE_MOUSE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// handleRawSlashCommand processes slash commands.
func (a *App) handleRawSlashCommand(inputVal string) bool {
	parts := strings.Fields(inputVal)
	cmdName := parts[0]

	if cmdName == "/exit" || cmdName == "/quit" {
		return true
	}

	if a.input.history != nil {
		a.input.history.Add(inputVal)
	}
	a.input.Clear()

	var replyLog string
	switch cmdName {
	case "/permission":
		if len(parts) < 2 {
			old := a.state
			a.state = statePermissionSelect
			a.screens.SetPermIndex(1)
			a.notifyStateChange(old)
			return false
		}
		modeArg := agent.PermissionMode(strings.ToLower(parts[1]))
		err := agent.GlobalPermissionManager.SetMode(modeArg)
		if err != nil {
			replyLog = StyleToolError.Render(fmt.Sprintf("[error] Invalid permission: %s", parts[1]))
		} else {
			replyLog = StyleToolSuccess.Render(fmt.Sprintf("Permission level switched to: %s", modeArg))
		}

	case "/rules":
		var sb strings.Builder
		sb.WriteString(StyleKeyActive.Render("Permission Rules") + "\n")
		rules := agent.GlobalPermissionManager.GetRules()
		for i, r := range rules {
			behavior := lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render("ALLOW")
			if r.Behavior != "allow" {
				behavior = lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render("DENY")
			}
			sb.WriteString(fmt.Sprintf("  %d. [%s] tool: %s\n", i+1, behavior, r.Tool))
		}
		replyLog = sb.String()

	case "/stats":
		var sb strings.Builder
		sb.WriteString(StyleKeyActive.Render("📈 Session Statistics & Telemetry") + "\n")
		sb.WriteString(strings.Repeat("─", 60) + "\n")
		modelName := "Unknown"
		if a.runner != nil {
			modelName = a.runner.ModelName()
		}
		sessionDuration := time.Since(a.sessionStartTime).Round(time.Second)
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Session ID", StylePrompt.Render(a.sessionID)))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Active LLM Model", StylePrompt.Render(modelName)))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Permission Mode", StylePrompt.Render(string(agent.GlobalPermissionManager.GetMode()))))
		sb.WriteString(fmt.Sprintf("  %-22s :  %d\n", "Interaction Rounds", a.roundCount))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Session Running Time", sessionDuration))

		tokStr, costStr, velocityStr := "-", "-", "-"
		if a.totalTokens > 0 {
			tokStr = fmt.Sprintf("%d tokens", a.totalTokens)
			costStr = fmt.Sprintf("$%.4f USD", a.totalSessionCost)
			sec := time.Since(a.sessionStartTime).Seconds()
			if sec > 0.5 {
				velocityStr = fmt.Sprintf("%.2f tokens/sec", float64(a.totalTokens)/sec)
			}
		}
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Tokens Consumed", tokStr))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Estimated Session Cost", costStr))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Token Velocity", velocityStr))

		cardStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorPrimary).Padding(0, 1)
		replyLog = cardStyle.Render(sb.String()) + "\n"

	case "/sessions":
		old := a.state
		a.state = stateSessionSelect
		a.loadSessionsList()
		a.notifyStateChange(old)
		return false

	case "/help", "/commands":
		replyLog = RenderHelpDashboard()

	case "/mcp":
		if len(parts) >= 2 && parts[1] == "reload" {
			toolCount, err := agent.RebuildToolPool()
			if err != nil {
				replyLog = StyleToolError.Render(fmt.Sprintf("[error] MCP reload failed: %v", err))
			} else {
				servers := agent.GlobalMCPRouter.ListServers()
				var sb strings.Builder
				sb.WriteString(StyleToolSuccess.Render(fmt.Sprintf("MCP tool pool rebuilt (v%d): %d tools, %d servers",
					agent.ToolPoolVersion(), toolCount, len(servers))))
				for name, status := range servers {
					sb.WriteString(fmt.Sprintf("\n  %-20s %s", name, status))
				}
				replyLog = sb.String()
			}
		} else {
			servers := agent.GlobalMCPRouter.ListServers()

			var sb strings.Builder
			sb.WriteString(StyleKeyActive.Render(fmt.Sprintf("MCP Plugin Status: %d servers", len(servers))) + "\n")
			sb.WriteString(strings.Repeat("-", 40) + "\n")
			for name, status := range servers {
				tag := StyleToolSuccess.Render(status)
				if status != "connected" {
					tag = StyleToolError.Render(status)
				}
				sb.WriteString(fmt.Sprintf("  %-20s %s\n", name, tag))
			}
			if len(servers) == 0 {
				sb.WriteString("  (no MCP servers configured)\n")
			}
			sb.WriteString("\n  Use /mcp reload to rescan plugins")
			replyLog = sb.String()
		}

	default:
		replyLog = StyleToolError.Render(fmt.Sprintf("[error] Unknown command: %s", cmdName))
	}

	a.history.Add(HistoryEntry{Role: RoleUser, Content: inputVal})
	a.history.Add(HistoryEntry{Role: RoleSystem, Content: replyLog})
	return false
}

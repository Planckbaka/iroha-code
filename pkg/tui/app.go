package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"iroha/pkg/agent"
	"iroha/pkg/config"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"google.golang.org/adk/session"
	"golang.org/x/term"
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
	runner  *agent.CustomRunner
	respon  BridgeResponder

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
	currentPrompt string
	lastError    error
	lastRawResp  string

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
	app.slash = NewSlashMenuComponent(allSlashCommandsAsEntries())
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
		matches := statusTagRe.FindAllStringSubmatch(a.streamedText, -1)
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
			elapsed := time.Duration(0)
			if !a.roundStartTime.IsZero() {
				elapsed = time.Since(a.roundStartTime)
			}
			a.streamedText += "\n" + RenderCancelCard(elapsed)
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

// Render collects output from all components.
func (a *App) Render() []string {
	a.cursorRow = -1
	a.cursorCol = 0

	// Full-screen overlays
	if a.screens.Active(a.state) {
		return a.screens.Render(a.width)
	}

	var lines []string

	// 1. Dashboards
	if todo := RenderTodoDashboard(); todo != "" {
		lines = append(lines, strings.Split(strings.TrimRight(todo, "\n"), "\n")...)
	}
	if task := RenderTaskDashboard(); task != "" {
		lines = append(lines, strings.Split(strings.TrimRight(task, "\n"), "\n")...)
	}

	// 2. Chat history
	if a.history.Len() > 0 {
		lines = append(lines, a.history.Render(a.width, 10000)...)
	} else if a.state == statePrompt {
		lines = append(lines, strings.Split(strings.TrimRight(RenderWelcomeCard(a.runner), "\n"), "\n")...)
	}

	// 3. Active stream / thinking / confirming
	switch a.state {
	case stateThinking:
		spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerFrame := spinnerFrames[(time.Now().UnixNano()/100000000)%int64(len(spinnerFrames))]
		spinnerStyled := lipgloss.NewStyle().Foreground(ColorSecondary).Render(spinnerFrame)

		if a.chat.activeTool.Running {
			color, icon, _ := getToolCategoryTheme(a.chat.activeTool.Name)
			activity := FormatToolActivity(a.chat.activeTool.Name, a.chat.activeTool.Args)
			iconStyled := lipgloss.NewStyle().Foreground(color).Render(icon)
			textStyled := lipgloss.NewStyle().Foreground(color).Render("running " + strings.ToLower(activity) + "...")

			lines = append(lines, "", "  "+spinnerStyled+" "+iconStyled+" "+textStyled)

			if len(a.chat.activeTool.StreamLines) > 0 {
				cmdDisplay := ""
				if argMap, ok := a.chat.activeTool.Args.(map[string]any); ok {
					if cmd, ok := argMap["command"].(string); ok {
						cmdDisplay = cmd
					}
				}
				streamArea := RenderShellStreamArea(a.chat.activeTool.StreamLines, cmdDisplay, a.width)
				if streamArea != "" {
					lines = append(lines, strings.Split(strings.TrimRight(streamArea, "\n"), "\n")...)
				}
			}
		} else {
			textStyled := lipgloss.NewStyle().Foreground(ColorPrimary).Italic(true).Render("thinking...")
			lines = append(lines, "", "  "+spinnerStyled+" "+textStyled)
		}
	case stateStreaming:
		fullText := a.renderedText
		if a.streamedText != "" {
			fullText = RenderMarkdown(a.streamedText)
		}
		if fullText != "" {
			rendered := StyleAgentMsg.Render(fullText)
			lines = append(lines, "")
			lines = append(lines, strings.Split(rendered, "\n")...)
		}
		if a.chat.activeTool.Running {
			spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			spinnerFrame := spinnerFrames[(time.Now().UnixNano()/100000000)%int64(len(spinnerFrames))]
			spinnerStyled := lipgloss.NewStyle().Foreground(ColorSecondary).Render(spinnerFrame)

			color, icon, _ := getToolCategoryTheme(a.chat.activeTool.Name)
			activity := FormatToolActivity(a.chat.activeTool.Name, a.chat.activeTool.Args)
			iconStyled := lipgloss.NewStyle().Foreground(color).Render(icon)
			textStyled := lipgloss.NewStyle().Foreground(color).Render("running " + strings.ToLower(activity) + "...")

			lines = append(lines, "", "  "+spinnerStyled+" "+iconStyled+" "+textStyled)

			if len(a.chat.activeTool.StreamLines) > 0 {
				cmdDisplay := ""
				if argMap, ok := a.chat.activeTool.Args.(map[string]any); ok {
					if cmd, ok := argMap["command"].(string); ok {
						cmdDisplay = cmd
					}
				}
				streamArea := RenderShellStreamArea(a.chat.activeTool.StreamLines, cmdDisplay, a.width)
				if streamArea != "" {
					lines = append(lines, strings.Split(strings.TrimRight(streamArea, "\n"), "\n")...)
				}
			}
		}
	case stateConfirming:
		lines = append(lines, a.confirm.Render(a.width)...)
	}

	// 4. Separator
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorSecondary).Render(strings.Repeat("─", 80)))

	// 5. Slash menu
	if slashLines := a.slash.Render(a.width); len(slashLines) > 0 {
		lines = append(lines, slashLines...)
	}

	// 6. Input area
	inputStartRow := len(lines)
	lines = append(lines, a.input.Render(a.width)...)

	if a.state == statePrompt {
		promptPrefix := "┃ "
		cursorIdx := a.input.focus.CursorIndex
		if cursorIdx > len(a.input.focus.Buffer) {
			cursorIdx = len(a.input.focus.Buffer)
		}
		if cursorIdx < 0 {
			cursorIdx = 0
		}
		beforeCursor := a.input.focus.Buffer[:cursorIdx]
		linesBefore := strings.Split(string(beforeCursor), "\n")
		cursorLineIdx := len(linesBefore) - 1

		prefixWidth := lipgloss.Width(promptPrefix)
		linePrefixWidth := 0
		if cursorLineIdx == 0 {
			linePrefixWidth = prefixWidth
		}

		a.cursorCol = linePrefixWidth + lipgloss.Width(linesBefore[cursorLineIdx]) + 1
		a.cursorRow = inputStartRow + cursorLineIdx
	}

	// 7. Status bar
	lines = append(lines, a.status.Render(a.width)...)

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
	_ = agent.GlobalPermissionManager.SetMode(modeToPermMode(mode))
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
	a.sessionID = sessionID
	a.loadHistoryFromSession(sessionID)
	old := a.state
	a.state = statePrompt
	a.notifyStateChange(old)
}

func (a *App) handleNewSession() {
	a.sessionID = uuid.New().String()
	a.history = NewHistoryStore()
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
			a.history.Add(HistoryEntry{Role: RoleAgent, Content: RenderMarkdown(a.streamedText)})
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
		a.history.Add(HistoryEntry{Role: RoleAgent, Content: RenderErrorCard(a.lastError)})
		a.lastError = nil
	} else if a.streamedText != "" {
		a.lastRawResp = a.streamedText
		a.history.Add(HistoryEntry{Role: RoleAgent, Content: RenderMarkdown(a.streamedText)})
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

// loadHistoryFromSession loads history from a previous session.
func (a *App) loadHistoryFromSession(sessionID string) {
	if agent.GlobalSessionService == nil {
		return
	}
	resp, err := agent.GlobalSessionService.Get(context.Background(), &session.GetRequest{
		SessionID: sessionID,
	})
	if err != nil || resp.Session == nil {
		return
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

	for _, t := range turns {
		a.history.Add(HistoryEntry{Role: RoleUser, Content: t.prompt})
		if t.response != "" {
			a.history.Add(HistoryEntry{Role: RoleAgent, Content: RenderMarkdown(t.response)})
		}
	}
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

func allSlashCommandsAsEntries() []SlashCommand {
	var entries []SlashCommand
	for _, cmd := range AllSlashCommands {
		entries = append(entries, SlashCommand{Command: cmd.Command, Description: cmd.Description})
	}
	return entries
}

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
	renderer := NewRawRenderer(os.Stdout)
	eventChan := make(chan any, 256)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Apply initial mode
	if initialMode != "" {
		_ = agent.GlobalPermissionManager.SetMode(initialMode)
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

	default:
		replyLog = StyleToolError.Render(fmt.Sprintf("[error] Unknown command: %s", cmdName))
	}

	a.history.Add(HistoryEntry{Role: RoleUser, Content: "> " + inputVal})
	a.history.Add(HistoryEntry{Role: RoleSystem, Content: replyLog})
	return false
}

// Ensure unused imports are referenced
var _ = fmt.Sprintf
var _ = uuid.New

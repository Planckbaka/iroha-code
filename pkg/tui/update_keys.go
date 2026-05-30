package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"iroha/pkg/agent"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/google/uuid"
	"github.com/charmbracelet/lipgloss"
)

// HandleEvent processes a thread-safe message received by the raw event loop
func (m *Model) HandleEvent(event any) bool {
	switch msg := event.(type) {
	case string:
		if msg == "tick" {
			// Return false to redraw thinking spinner ticks
			return false
		}

	case StartupPromptMsg:
		if msg.Prompt == "" {
			return false
		}
		m.HistoryManager.Add(msg.Prompt)
		m.CurrentPrompt = msg.Prompt
		userLog := StyleUserMsg.Render("> " + m.CurrentPrompt)
		m.History = append(m.History, userLog)

		m.StreamedText = ""
		m.RenderedText = ""
		m.State = stateThinking
		m.InputBuffer = nil
		m.CursorIndex = 0
		m.RoundCount++
		m.RoundStartTime = time.Now()
		m.ActiveTool = agent.ToolStatus{}

		m.Runner.Execute(m.Ctx, "user-dev", m.SessionID, m.CurrentPrompt,
			m.OnEvent, m.OnError, m.OnDone,
		)
		return false

	case StreamTextMsg:
		m.State = stateStreaming
		m.StreamedText += msg.Text
		matches := statusTagRe.FindAllStringSubmatch(m.StreamedText, -1)
		if len(matches) > 0 {
			m.CurrentStatusText = matches[len(matches)-1][1]
		}
		return false

	case ToolStatusMsg:
		status := msg.Status
		if status.Running {
			if m.ActiveTool.Running && m.ActiveTool.Name == status.Name {
				status.StreamLines = append(m.ActiveTool.StreamLines, status.StreamLines...)
			}
			m.ActiveTool = status
			if m.RoundStartTime.IsZero() {
				m.RoundStartTime = time.Now()
			}
		} else {
			m.ActiveTool = agent.ToolStatus{}
			var logLine string
			if status.Success {
				logLine = "\n" + RenderToolSuccessCard(status.Name, status.Args, status.Duration)
			} else {
				logLine = "\n\n" + RenderToolErrorCard(status.Name, status.Args, status.Duration, status.Error)
			}

			// Push accumulated LLM text first to avoid wrapping tool log in Glamour
			if m.StreamedText != "" {
				agentLog := StyleAgentMsg.Render(RenderMarkdown(m.StreamedText))
				m.History = append(m.History, agentLog)
				m.StreamedText = ""
			}
			m.History = append(m.History, logLine)
		}
		return false

	case ConfirmationRequiredMsg:
		m.State = stateConfirming
		m.ConfirmSelectIndex = 0
		m.ConfirmDiffActive = false

		// Extract Unified Diff if present
		const diffMarker = "\n\n\x1b[1;34m[File Changes (Diff)]:\x1b[0m\n"
		if idx := strings.Index(msg.Prompt, diffMarker); idx != -1 {
			m.ConfirmationPrompt = msg.Prompt[:idx]
			m.ConfirmDiffText = msg.Prompt[idx+len(diffMarker):]
		} else {
			m.ConfirmationPrompt = msg.Prompt
			m.ConfirmDiffText = ""
		}
		return false

	case AgentErrorMsg:
		m.LastError = msg.Err
		m.finalizeTurn()
		return false

	case AgentDoneMsg:
		m.finalizeTurn()
		return false

	case Key:
		return m.handleKey(msg)
	}

	return false
}

func (m *Model) handleKey(k Key) bool {
	// 1. Ctrl+C aborting
	if k.Type == KeyCtrlC {
		if m.State == statePermissionSelect || m.State == stateSessionSelect {
			return true // Quit
		}
		if m.State != statePrompt {
			m.Cancel()
			elapsed := time.Duration(0)
			if !m.RoundStartTime.IsZero() {
				elapsed = time.Since(m.RoundStartTime)
			}
			m.StreamedText += "\n" + RenderCancelCard(elapsed)
			m.finalizeTurn()
			return false
		}
		return true // Quit
	}

	// 2. Permission selection state
	if m.State == statePermissionSelect {
		permModes := []agent.PermissionMode{agent.ModePlan, agent.ModeDefault, agent.ModeAuto}
		switch k.Type {
		case KeyUp:
			if m.PermSelectIndex > 0 {
				m.PermSelectIndex--
			}
		case KeyDown:
			if m.PermSelectIndex < len(permModes)-1 {
				m.PermSelectIndex++
			}
		case KeyEnter:
			_ = agent.GlobalPermissionManager.SetMode(permModes[m.PermSelectIndex])
			if m.StartInSessionPicker {
				m.State = stateSessionSelect
				m.loadSessionsList()
			} else {
				m.State = statePrompt
			}
		}
		return false
	}

	// 3. Session list picker state
	if m.State == stateSessionSelect {
		switch k.Type {
		case KeyUp:
			if m.SessionListIndex > 0 {
				m.SessionListIndex--
			}
		case KeyDown:
			if m.SessionListIndex < len(m.SessionsList) {
				m.SessionListIndex++
			}
		case KeyEsc:
			m.State = statePrompt
		case KeyEnter:
			if m.SessionListIndex == 0 {
				m.SessionID = uuid.New().String()
				m.History = nil
				m.TotalTokens = 0
			} else {
				sel := m.SessionsList[m.SessionListIndex-1]
				m.SessionID = sel.ID
				m.LoadHistoryFromSession(sel.ID)
			}
			m.State = statePrompt
		}
		return false
	}

	// 4. Confirmation state
	if m.State == stateConfirming {
		if m.ConfirmEditActive {
			switch k.Type {
			case KeyEnter:
				editedVal := string(m.InputBuffer)
				m.ConfirmEditActive = false
				m.State = stateThinking
				m.InputBuffer = nil
				m.CursorIndex = 0
				agent.Bridge.ResponseChan <- "edit:" + editedVal
			case KeyEsc, KeyCtrlC:
				m.ConfirmEditActive = false
				m.InputBuffer = nil
				m.CursorIndex = 0
			case KeyBackspace:
				if m.CursorIndex > 0 {
					m.InputBuffer = append(m.InputBuffer[:m.CursorIndex-1], m.InputBuffer[m.CursorIndex:]...)
					m.CursorIndex--
				}
			case KeyLeft:
				if m.CursorIndex > 0 {
					m.CursorIndex--
				}
			case KeyRight:
				if m.CursorIndex < len(m.InputBuffer) {
					m.CursorIndex++
				}
			case KeyRune:
				m.InputBuffer = append(m.InputBuffer[:m.CursorIndex], append([]rune{k.Rune}, m.InputBuffer[m.CursorIndex:]...)...)
				m.CursorIndex++
			}
			return false
		}

		switch k.Type {
		case KeyLeft, KeyShiftTab:
			m.ConfirmSelectIndex = (m.ConfirmSelectIndex - 1 + 5) % 5
		case KeyRight, KeyTab:
			m.ConfirmSelectIndex = (m.ConfirmSelectIndex + 1) % 5
		case KeyEnter:
			var resp string
			switch m.ConfirmSelectIndex {
			case 0:
				resp = "y"
			case 1:
				resp = "n"
			case 2:
				resp = "always"
			case 3:
				m.ConfirmEditActive = true
				m.ConfirmEditText = m.getEditableValue()
				m.InputBuffer = []rune(m.ConfirmEditText)
				m.CursorIndex = len(m.InputBuffer)
				return false
			case 4:
				resp = "explain"
			}
			m.State = stateThinking
			agent.Bridge.ResponseChan <- resp
		case KeyRune:
			switch k.Rune {
			case 'd', 'D':
				if m.ConfirmDiffText != "" {
					m.ConfirmDiffActive = !m.ConfirmDiffActive
				}
			case 'y', 'Y':
				m.State = stateThinking
				agent.Bridge.ResponseChan <- "y"
			case 'n', 'N':
				m.State = stateThinking
				agent.Bridge.ResponseChan <- "n"
			case 'a', 'A':
				m.State = stateThinking
				agent.Bridge.ResponseChan <- "always"
			case 'e', 'E':
				m.ConfirmEditActive = true
				m.ConfirmEditText = m.getEditableValue()
				m.InputBuffer = []rune(m.ConfirmEditText)
				m.CursorIndex = len(m.InputBuffer)
			case '?':
				m.State = stateThinking
				agent.Bridge.ResponseChan <- "explain"
			}
		}
		return false
	}

	// 5. Normal prompt state
	if m.State == statePrompt {
		switch k.Type {
		case KeyCtrlY:
			if m.LastRawResponse != "" {
				text := m.LastRawResponse
				seq := osc52.New(text)
				if strings.HasPrefix(os.Getenv("TERM"), "tmux") {
					seq = seq.Tmux()
				}
				fmt.Fprint(os.Stderr, seq.String())
				_ = clipboard.WriteAll(text)
				m.History = append(m.History, StyleToolSuccess.Render(fmt.Sprintf("Copied to clipboard (%d chars)", len(text))))
			}
		case KeyUp:
			if m.SlashMenuActive {
				if m.SlashMenuIndex > 0 {
					m.SlashMenuIndex--
				}
			} else {
				m.InputBuffer = []rune(m.HistoryManager.Up())
				m.CursorIndex = len(m.InputBuffer)
			}
		case KeyDown:
			if m.SlashMenuActive {
				if m.SlashMenuIndex < len(m.SlashMenuItems)-1 {
					m.SlashMenuIndex++
				}
			} else {
				m.InputBuffer = []rune(m.HistoryManager.Down())
				m.CursorIndex = len(m.InputBuffer)
			}
		case KeyLeft:
			if m.CursorIndex > 0 {
				m.CursorIndex--
			}
		case KeyRight:
			if m.CursorIndex < len(m.InputBuffer) {
				m.CursorIndex++
			}
		case KeyBackspace:
			if m.CursorIndex > 0 {
				m.InputBuffer = append(m.InputBuffer[:m.CursorIndex-1], m.InputBuffer[m.CursorIndex:]...)
				m.CursorIndex--
				m.updateSlashMenu(string(m.InputBuffer))
			}
		case KeyAltEnter:
			m.InputBuffer = append(m.InputBuffer[:m.CursorIndex], append([]rune{'\n'}, m.InputBuffer[m.CursorIndex:]...)...)
			m.CursorIndex++
		case KeyTab:
			if m.SlashMenuActive && len(m.SlashMenuItems) > 0 {
				selected := m.SlashMenuItems[m.SlashMenuIndex]
				m.InputBuffer = []rune(selected.Command + " ")
				m.CursorIndex = len(m.InputBuffer)
				m.SlashMenuActive = false
				m.SlashMenuItems = nil
				m.resetPathCompletion()
				return false
			}

			// Path suggestions cycling
			val := string(m.InputBuffer)
			var prefix, rest string
			lastSpace := strings.LastIndex(val, " ")
			if lastSpace == -1 {
				prefix = val
				rest = ""
			} else {
				prefix = val[lastSpace+1:]
				rest = val[:lastSpace+1]
			}
			matches := m.matchLocalPaths(prefix)
			if len(matches) > 0 {
				m.InputBuffer = []rune(rest + matches[0])
				m.CursorIndex = len(m.InputBuffer)
			}
		case KeyEsc:
			if m.SlashMenuActive {
				m.SlashMenuActive = false
				m.SlashMenuItems = nil
			}
		case KeyEnter:
			if m.SlashMenuActive && len(m.SlashMenuItems) > 0 {
				selected := m.SlashMenuItems[m.SlashMenuIndex]
				m.InputBuffer = []rune(selected.Command)
				m.CursorIndex = len(m.InputBuffer)
				m.SlashMenuActive = false
				m.SlashMenuItems = nil
			}

			inputVal := strings.TrimSpace(string(m.InputBuffer))
			if inputVal == "" {
				return false
			}

			if strings.HasPrefix(inputVal, "/") {
				return m.handleRawSlashCommand(inputVal)
			}

			m.CurrentPrompt = inputVal
			userLog := StyleUserMsg.Render("> " + m.CurrentPrompt)
			m.History = append(m.History, userLog)

			m.StreamedText = ""
			m.State = stateThinking
			m.InputBuffer = nil
			m.CursorIndex = 0
			m.RoundCount++
			m.RoundStartTime = time.Now()
			m.ActiveTool = agent.ToolStatus{}

			ctx, cancel := context.WithCancel(context.Background())
			m.Ctx = ctx
			m.Cancel = cancel

			m.Runner.Execute(m.Ctx, "user-dev", m.SessionID, m.CurrentPrompt,
				m.OnEvent, m.OnError, m.OnDone,
			)
		case KeyRune:
			m.InputBuffer = append(m.InputBuffer[:m.CursorIndex], append([]rune{k.Rune}, m.InputBuffer[m.CursorIndex:]...)...)
			m.CursorIndex++
			m.updateSlashMenu(string(m.InputBuffer))
		}
	}

	return false
}

func (m *Model) handleRawSlashCommand(inputVal string) bool {
	parts := strings.Fields(inputVal)
	cmdName := parts[0]

	if cmdName == "/exit" || cmdName == "/quit" {
		return true
	}

	m.HistoryManager.Add(inputVal)
	userLog := StyleUserMsg.Render("> " + inputVal)
	m.InputBuffer = nil
	m.CursorIndex = 0

	var replyLog string
	switch cmdName {
	case "/permission":
		if len(parts) < 2 {
			m.PrevState = m.State
			m.State = statePermissionSelect
			m.PermSelectIndex = 1
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
		if m.Runner != nil {
			modelName = m.Runner.ModelName()
		}
		sessionDuration := time.Since(m.SessionStartTime).Round(time.Second)
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Session ID", StylePrompt.Render(m.SessionID)))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Active LLM Model", StylePrompt.Render(modelName)))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Permission Mode", StylePrompt.Render(string(agent.GlobalPermissionManager.GetMode()))))
		sb.WriteString(fmt.Sprintf("  %-22s :  %d\n", "Interaction Rounds", m.RoundCount))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Session Running Time", sessionDuration))

		tokStr, costStr, velocityStr := "-", "-", "-"
		if m.TotalTokens > 0 {
			tokStr = fmt.Sprintf("%d tokens", m.TotalTokens)
			costStr = fmt.Sprintf("$%.4f USD", m.TotalSessionCost)
			sec := time.Since(m.SessionStartTime).Seconds()
			if sec > 0.5 {
				velocityStr = fmt.Sprintf("%.2f tokens/sec", float64(m.TotalTokens)/sec)
			}
		}
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Tokens Consumed", tokStr))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Estimated Session Cost", costStr))
		sb.WriteString(fmt.Sprintf("  %-22s :  %s\n", "Token Velocity", velocityStr))

		cardStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorPrimary).Padding(0, 1)
		replyLog = cardStyle.Render(sb.String()) + "\n"

	case "/sessions":
		m.PrevState = m.State
		m.State = stateSessionSelect
		m.loadSessionsList()
		return false

	case "/help", "/commands":
		replyLog = RenderHelpDashboard()

	default:
		replyLog = StyleToolError.Render(fmt.Sprintf("[error] Unknown command: %s", cmdName))
	}

	m.History = append(m.History, userLog, replyLog)
	return false
}

// resetPathCompletion clears path auto-completion states.
func (m *Model) resetPathCompletion() {
	m.SlashMenuActive = false
	m.SlashMenuItems = nil
}

package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"iroha/pkg/agent"
	"iroha/pkg/config"

	"github.com/charmbracelet/lipgloss"
	"google.golang.org/adk/session"
)

// SlashMenuItem represents a single slash command entry in the popup menu
type SlashMenuItem struct {
	Command     string
	Description string
}

// AllSlashCommands is the master list of all supported slash commands
var AllSlashCommands = []SlashMenuItem{
	{"/permission", "Select or switch permission level (plan | auto | default)"},
	{"/rules", "View current permission rules list"},
	{"/hooks", "View or hot-reload Hook configuration (reload)"},
	{"/memory", "View cross-session memory content"},
	{"/prompt", "View full System Prompt"},
	{"/sections", "View System Prompt structure outline"},
	{"/task", "View task planning board"},
	{"/team", "View multi-agent team status"},
	{"/worktree", "View Git Worktree isolation status"},
	{"/mcp", "View MCP plugin status"},
	{"/bg", "View background task status"},
	{"/skill", "Invoke a registered skill by name (e.g. /skill tdd-workflow)"},
	{"/trace", "View tool call trace log for the current session"},
	{"/stats", "View session statistics, performance latency, and token cost details"},
	{"/sessions", "View and switch session history"},
	{"/resume", "Resume the most recent session and continue the conversation"},
	{"/help", "View system help, keyboard shortcuts, and command palette"},
	{"/commands", "View all supported slash commands"},
	{"/doctor", "Run system diagnostics to check API, network, Git, and toolchain status"},
	{"/exit", "Exit the program"},
}

var statusTagRe = regexp.MustCompile(`(?m)^\[status:(.+?)\]`)

type TuiState int

const (
	statePrompt TuiState = iota
	stateThinking
	stateStreaming
	stateConfirming
	statePermissionSelect
	stateSessionSelect
)

func (s TuiState) String() string {
	switch s {
	case statePrompt:
		return "Prompt"
	case stateThinking:
		return "Thinking"
	case stateStreaming:
		return "Streaming"
	case stateConfirming:
		return "Confirming"
	case statePermissionSelect:
		return "PermissionSelect"
	case stateSessionSelect:
		return "SessionSelect"
	default:
		return "Unknown"
	}
}

// Custom Message Types
type StreamTextMsg struct {
	Text string
}

type ConfirmationRequiredMsg struct {
	Prompt string
}

type ToolStatusMsg struct {
	Status agent.ToolStatus
}

type AgentErrorMsg struct {
	Err error
}

type AgentDoneMsg struct{}

type DoctorResultMsg struct {
	Report string
}

// Model represents the active TUI state controller
type Model struct {
	State              TuiState
	InputBuffer        []rune
	CursorIndex        int
	HistoryManager     *HistoryManager
	History            []string
	CurrentPrompt      string
	StreamedText       string
	ConfirmationPrompt string
	Runner             *agent.CustomRunner
	Ctx                context.Context
	Cancel             context.CancelFunc
	LastError          error
	Width              int
	IsGoalMode         bool
	GoalText           string

	// Clipboard copy
	LastRawResponse string

	// Telemetry metrics
	ActiveTool        agent.ToolStatus
	RoundCount        int
	SessionStartTime  time.Time
	RoundStartTime    time.Time
	LastRoundDuration time.Duration

	// Token usage tracking
	TotalTokens      int
	TotalSessionCost float64

	// Incremental render cache
	RenderedText      string
	CurrentStatusText string

	// Slash command autocomplete
	SlashMenuActive bool
	SlashMenuItems  []SlashMenuItem
	SlashMenuIndex  int

	// Menu selections
	PermSelectIndex    int
	SessionListIndex   int
	ConfirmSelectIndex int
	ConfirmDiffActive  bool
	ConfirmDiffText    string
	ConfirmEditActive  bool
	ConfirmEditText    string

	// Startup initializations
	SessionID            string
	StartInSessionPicker bool
	SessionsList         []agent.SessionMetadata
	PrevState            TuiState
	StartupPrompt        string

	// Bridges
	OnEvent func(*session.Event)
	OnError func(error)
	OnDone  func()
}

// SetupRawTui initializes the state Model
func SetupRawTui(runner *agent.CustomRunner, sessionID string, startInSessionPicker bool, initialMode agent.PermissionMode, startupPrompt string) *Model {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Model{
		State:              statePermissionSelect,
		HistoryManager:     NewHistoryManager(),
		History:            make([]string, 0),
		Runner:             runner,
		Ctx:                ctx,
		Cancel:             cancel,
		SessionStartTime:   time.Now(),
		PermSelectIndex:    1, // default
		SessionID:          sessionID,
		StartInSessionPicker: startInSessionPicker,
		StartupPrompt:      startupPrompt,
	}

	if initialMode != "" {
		_ = agent.GlobalPermissionManager.SetMode(initialMode)
		if startInSessionPicker {
			m.State = stateSessionSelect
		} else {
			m.State = statePrompt
		}
	}

	if sessionID != "" && !startInSessionPicker {
		m.LoadHistoryFromSession(sessionID)
	}

	return m
}

func (m *Model) finalizeTurn() {
	m.State = statePrompt
	if !m.RoundStartTime.IsZero() {
		m.LastRoundDuration = time.Since(m.RoundStartTime)
		m.RoundStartTime = time.Time{}
	}
	m.ActiveTool = agent.ToolStatus{}
	m.CurrentStatusText = ""
	m.RenderedText = ""
	m.ConfirmEditActive = false
	m.ConfirmEditText = ""

	if m.Runner != nil {
		usage := m.Runner.GetTokenUsage()
		if usage > 0 {
			m.TotalTokens = usage
		} else if m.TotalTokens == 0 {
			m.TotalTokens = len(m.StreamedText) / 4
		}
		m.TotalSessionCost = config.EstimateCost(m.Runner.ModelName(), m.TotalTokens)
	}

	userLog := StyleUserMsg.Render("> " + m.CurrentPrompt)
	var agentLog string
	if m.LastError != nil {
		agentLog = StyleAgentMsg.Render(RenderErrorCard(m.LastError))
		m.LastError = nil
	} else {
		m.LastRawResponse = m.StreamedText
		agentLog = StyleAgentMsg.Render(RenderMarkdown(m.StreamedText))
	}

	m.History = append(m.History, userLog, agentLog)
	m.InputBuffer = nil
	m.CursorIndex = 0
}

func (m *Model) getEditableValue() string {
	if m.ActiveTool.Args == nil {
		return ""
	}
	if argMap, ok := m.ActiveTool.Args.(map[string]any); ok {
		if cmd, ok := argMap["command"].(string); ok {
			return cmd
		}
		if content, ok := argMap["content"].(string); ok {
			return content
		}
		if path, ok := argMap["path"].(string); ok {
			return path
		}
	}
	return ""
}

// Render compiles all states into a slice of console lines
func (m *Model) Render() []string {
	if m.State == statePermissionSelect {
		return m.renderPermissionSelectScreen()
	}
	if m.State == stateSessionSelect {
		return m.renderSessionSelectScreen()
	}

	var lines []string

	// 1. Dynamic Dashboards
	todoRender := RenderTodoDashboard()
	if todoRender != "" {
		lines = append(lines, strings.Split(strings.TrimRight(todoRender, "\n"), "\n")...)
	}
	taskRender := RenderTaskDashboard()
	if taskRender != "" {
		lines = append(lines, strings.Split(strings.TrimRight(taskRender, "\n"), "\n")...)
	}

	// 2. Chat history
	if len(m.History) > 0 {
		for _, hist := range m.History {
			lines = append(lines, strings.Split(strings.TrimRight(hist, "\n"), "\n")...)
		}
	} else if m.State == statePrompt {
		lines = append(lines, strings.Split(strings.TrimRight(RenderWelcomeCard(m.Runner), "\n"), "\n")...)
	}

	// 3. Current active stream states
	switch m.State {
	case stateThinking:
		if m.ActiveTool.Running {
			activity := FormatToolActivity(m.ActiveTool.Name, m.ActiveTool.Args)
			lines = append(lines, "\n"+StyleAgentMsg.Render("🤖 "+activity))
		} else {
			lines = append(lines, "\n"+StyleAgentMsg.Render("🤖 thinking..."))
		}
	case stateStreaming:
		fullText := m.RenderedText
		if m.StreamedText != "" {
			fullText = RenderMarkdown(m.StreamedText)
		}
		if fullText != "" {
			lines = append(lines, "\n"+StyleAgentMsg.Render(fullText))
		}
		if m.ActiveTool.Running {
			activity := FormatToolActivity(m.ActiveTool.Name, m.ActiveTool.Args)
			lines = append(lines, "\n"+StyleAgentMsg.Render("🤖 "+activity))
		}
	case stateConfirming:
		if m.ConfirmEditActive {
			lines = append(lines, "\n"+lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render("Editing Tool Arguments"))
			lines = append(lines, "  Press [Enter] to run with modified arguments. Press [Esc] to cancel.\n")
		} else {
			card := RenderConfirmCardWithDiff(m.ConfirmationPrompt, m.ConfirmSelectIndex, m.ConfirmDiffText != "", m.ConfirmDiffActive)
			lines = append(lines, "\n"+StyleAgentMsg.Render(RenderMarkdown(m.StreamedText)+"\n"+card))
			if m.ConfirmDiffActive && m.ConfirmDiffText != "" {
				lines = append(lines, strings.Split(m.ConfirmDiffText, "\n")...)
			}
		}
	}

	// 4. Separator Line
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorSecondary).Render(strings.Repeat("─", 80)))

	// 5. Slash command autocomplete
	if m.SlashMenuActive && len(m.SlashMenuItems) > 0 {
		menu := RenderSlashMenu(m.SlashMenuItems, m.SlashMenuIndex, 80)
		lines = append(lines, strings.Split(strings.TrimRight(menu, "\n"), "\n")...)
	}

	// 6. Input Area with Cyber-Holographic Block Cursor
	promptPrefix := "┃ "
	if m.ConfirmEditActive {
		promptPrefix = "✏️ "
	}
	inputVal := string(m.InputBuffer)
	var inputWithCursor string
	if m.CursorIndex >= len(m.InputBuffer) {
		inputWithCursor = promptPrefix + inputVal + "█"
	} else {
		inputWithCursor = promptPrefix + string(m.InputBuffer[:m.CursorIndex]) + "█" + string(m.InputBuffer[m.CursorIndex:])
	}
	lines = append(lines, strings.Split(inputWithCursor, "\n")...)

	// 7. Status bar at the bottom
	statusBar := RenderStatusBar(*m)
	lines = append(lines, strings.Split(statusBar, "\n")...)

	return lines
}

func (m *Model) renderPermissionSelectScreen() []string {
	var lines []string
	lines = append(lines, "\n"+lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("🛡️ Select Safety Permission Mode"))
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorTextMuted).Render("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))
	
	modes := []struct {
		Name string
		Desc string
	}{
		{"Plan Mode (Read-only)", "AI is strictly blocked from making any local modifications/shell writes."},
		{"Default Mode (Standard)", "Asks for explicit approval before running any write/modify operations."},
		{"Auto Mode (Automated)", "Read operations auto-approved. Write operations still ask for safety review."},
	}

	for i, md := range modes {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
		if i == m.PermSelectIndex {
			prefix = "▸ "
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
		}
		lines = append(lines, fmt.Sprintf("%s%s - %s", prefix, style.Render(md.Name), md.Desc))
	}
	lines = append(lines, "\n"+lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true).Render("  Up/Down - Move   Enter - Select   Ctrl+C - Exit"))
	return lines
}

func (m *Model) renderSessionSelectScreen() []string {
	var lines []string
	lines = append(lines, "\n"+lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render("📁 Switch Active Session Workspace"))
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorTextMuted).Render("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))

	// Option 0: Start new session
	prefixNew := "  "
	styleNew := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	if m.SessionListIndex == 0 {
		prefixNew = "▸ "
		styleNew = lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true)
	}
	lines = append(lines, prefixNew+styleNew.Render("+ Start New Clean Session"))

	for i, s := range m.SessionsList {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#A1A1AA"))
		if i+1 == m.SessionListIndex {
			prefix = "▸ "
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
		}
		summary := s.FirstPrompt
		if len(summary) > 40 {
			summary = summary[:37] + "..."
		}
		lines = append(lines, fmt.Sprintf("%s%s [%s] %s", prefix, style.Render(s.ID[:8]), s.LastUpdateTime.Format("01-02 15:04"), summary))
	}
	lines = append(lines, "\n"+lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true).Render("  Up/Down - Move   Enter - Select   Esc - Back   Ctrl+C - Exit"))
	return lines
}

func (m *Model) updateSlashMenu(input string) {
	if !strings.HasPrefix(input, "/") {
		m.SlashMenuActive = false
		m.SlashMenuItems = nil
		return
	}

	filter := strings.ToLower(strings.TrimSpace(input))
	var matches []SlashMenuItem
	for _, item := range AllSlashCommands {
		if strings.HasPrefix(strings.ToLower(item.Command), filter) {
			matches = append(matches, item)
		}
	}

	if len(matches) == 0 {
		m.SlashMenuActive = false
		m.SlashMenuItems = nil
		return
	}

	m.SlashMenuActive = true
	m.SlashMenuItems = matches
	if m.SlashMenuIndex >= len(matches) {
		m.SlashMenuIndex = len(matches) - 1
	}
	if m.SlashMenuIndex < 0 {
		m.SlashMenuIndex = 0
	}
}

func (m *Model) loadSessionsList() {
	if agent.GlobalSessionService != nil {
		list, err := agent.GlobalSessionService.ListSavedSessions()
		if err == nil {
			m.SessionsList = list
			if m.SessionListIndex > len(list) {
				m.SessionListIndex = len(list)
			}
			if m.SessionListIndex < 0 {
				m.SessionListIndex = 0
			}
		}
	}
}

func (m *Model) LoadHistoryFromSession(sessionID string) {
	m.History = nil
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
				currentTurn = &turn{
					prompt: pText,
				}
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
		userLog := StyleUserMsg.Render("> " + t.prompt)
		agentLog := StyleAgentMsg.Render(RenderMarkdown(t.response))
		m.History = append(m.History, userLog, agentLog)
	}

	totalTextLen := 0
	for _, t := range turns {
		totalTextLen += len(t.prompt) + len(t.response)
	}
	if totalTextLen > 0 {
		m.TotalTokens = totalTextLen / 4
		if m.Runner != nil {
			m.TotalSessionCost = config.EstimateCost(m.Runner.ModelName(), m.TotalTokens)
		}
	}
}

// matchLocalPaths scans the workspace directory for items matching the prefix.
func (m Model) matchLocalPaths(prefix string) []string {
	if prefix == "" {
		return nil
	}

	var dir, filePrefix string
	if strings.Contains(prefix, "/") {
		lastSlash := strings.LastIndex(prefix, "/")
		dir = prefix[:lastSlash]
		filePrefix = prefix[lastSlash+1:]
		if dir == "" {
			dir = "/"
		}
	} else {
		dir = "."
		filePrefix = prefix
	}

	cleanDir := filepath.Clean(dir)
	if cleanDir == ".." || strings.HasPrefix(cleanDir, "../") || strings.HasPrefix(cleanDir, "/") {
		return nil
	}

	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(filePrefix, ".") {
			continue
		}

		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(filePrefix)) {
			var matchPath string
			if cleanDir == "." {
				matchPath = name
			} else {
				matchPath = filepath.Join(cleanDir, name)
			}

			if entry.IsDir() {
				matchPath += "/"
			}
			matches = append(matches, matchPath)
		}
	}

	return matches
}

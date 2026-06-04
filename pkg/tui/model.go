package tui

import (
	"regexp"

	"iroha/pkg/agent"
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

// TuiState enumerates the top-level interaction states of the TUI.
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

// Custom message types dispatched through the App event loop.
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

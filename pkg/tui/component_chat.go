package tui

import (
	"fmt"
	"strings"

	"iroha/pkg/agent"

	"github.com/charmbracelet/lipgloss"
)

// ChatComponent renders the conversation history, streaming text, thinking
// indicator, and tool activity. It delegates history rendering to HistoryStore.
type ChatComponent struct {
	BaseComponent
	history      *HistoryStore
	state        TuiState
	streamedText string
	renderedText string
	activeTool   agent.ToolStatus

	// Status tag from LLM output
	currentStatusText string

	// Callbacks (wired by App in Phase 3)
	OnStreamStart func()
}

// NewChatComponent creates a ChatComponent with the given HistoryStore.
func NewChatComponent(history *HistoryStore) *ChatComponent {
	return &ChatComponent{
		history: history,
	}
}

// SetHistory replaces the conversation timeline used by the component.
func (c *ChatComponent) SetHistory(history *HistoryStore) {
	c.history = history
}

// Active returns true when the chat is the primary content area.
func (c *ChatComponent) Active(state TuiState) bool {
	// Chat is always visible in these states
	switch state {
	case statePrompt, stateThinking, stateStreaming, stateConfirming:
		return true
	default:
		return false
	}
}

// HandleInput — ChatComponent does not handle direct input.
func (c *ChatComponent) HandleInput(key Key) bool {
	return false
}

// OnStateChange reacts to state transitions.
func (c *ChatComponent) OnStateChange(oldState, newState TuiState) {
	// No special reaction needed — state is checked during Render
}

// ResetStream clears the current stream buffer.
func (c *ChatComponent) ResetStream() {
	c.streamedText = ""
	c.renderedText = ""
	c.activeTool = agent.ToolStatus{}
	c.currentStatusText = ""
}

// SetActiveTool updates the current tool status.
func (c *ChatComponent) SetActiveTool(status agent.ToolStatus) {
	if status.Running {
		c.activeTool = status
	} else {
		c.activeTool = agent.ToolStatus{}
	}
}

// RenderTail produces only the transient chat area: welcome, current stream,
// thinking/tool progress, or confirmation UI. App owns viewport composition.
func (c *ChatComponent) RenderTail(state TuiState, width int, streamText string, streamRendered string, welcomeLines []string, confirmLines []string) []string {
	width = sanitizedWidth(width)

	var lines []string
	if len(welcomeLines) > 0 && state == statePrompt {
		lines = append(lines, welcomeLines...)
	}

	switch state {
	case stateThinking:
		lines = append(lines, c.renderThinking(width)...)
	case stateStreaming:
		fullText := streamRendered
		if streamText != "" {
			fullText = RenderMarkdownWithWidth(streamText, max(1, width-2))
		}
		if fullText != "" {
			rendered := StyleAgentMsg.Render(fullText)
			lines = append(lines, "")
			lines = append(lines, strings.Split(rendered, "\n")...)
		}
		lines = append(lines, c.renderToolProgress(width)...)
	case stateConfirming:
		lines = append(lines, confirmLines...)
	}

	return lines
}

func (c *ChatComponent) renderThinking(width int) []string {
	if c.activeTool.Running {
		return c.renderToolProgress(width)
	}
	return []string{"", "  " + currentSpinnerFrame() + " " + StyleThinkingText.Render("thinking")}
}

func (c *ChatComponent) renderToolProgress(width int) []string {
	if !c.activeTool.Running {
		return nil
	}

	color, label, _ := getToolCategoryTheme(c.activeTool.Name)
	activity := FormatToolActivity(c.activeTool.Name, c.activeTool.Args)
	labelStyled := lipgloss.NewStyle().Foreground(color).Render("[" + label + "]")
	textStyled := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(strings.ToLower(activity))

	lines := []string{"", "  " + currentSpinnerFrame() + " " + labelStyled + " " + textStyled}
	if len(c.activeTool.StreamLines) == 0 {
		return lines
	}

	cmdDisplay := ""
	if argMap, ok := c.activeTool.Args.(map[string]any); ok {
		if cmd, ok := argMap["command"].(string); ok {
			cmdDisplay = cmd
		}
	}
	streamArea := RenderShellStreamArea(c.activeTool.StreamLines, cmdDisplay, width)
	if streamArea != "" {
		lines = append(lines, strings.Split(strings.TrimRight(streamArea, "\n"), "\n")...)
	}
	return lines
}

// Render produces the chat area output.
func (c *ChatComponent) Render(width int) []string {
	width = sanitizedWidth(width)

	var lines []string

	// 1. History entries (via HistoryStore)
	if c.history != nil && c.history.Len() > 0 {
		// For now, render all history lines (viewport clipping happens in App)
		histLines := c.history.Render(width, 10000)
		lines = append(lines, histLines...)
	}

	// 2. Current active stream states
	switch c.state {
	case stateThinking:
		if c.activeTool.Running {
			activity := FormatToolActivity(c.activeTool.Name, c.activeTool.Args)
			lines = append(lines, "", StyleAgentMsg.Render("🤖 "+activity))
		} else {
			lines = append(lines, "", StyleAgentMsg.Render("🤖 thinking..."))
		}
	case stateStreaming:
		fullText := c.renderedText
		if c.streamedText != "" {
			fullText = RenderMarkdownWithWidth(c.streamedText, max(1, width-2))
		}
		if fullText != "" {
			rendered := StyleAgentMsg.Render(fullText)
			lines = append(lines, "")
			lines = append(lines, strings.Split(rendered, "\n")...)
		}
		if c.activeTool.Running {
			activity := FormatToolActivity(c.activeTool.Name, c.activeTool.Args)
			lines = append(lines, "", StyleAgentMsg.Render(fmt.Sprintf("🤖 %s", activity)))
		}
	case stateConfirming:
		// ConfirmComponent handles its own rendering
	}

	return lines
}

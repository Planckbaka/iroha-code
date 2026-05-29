package tui

import (
	"fmt"
	"strings"

	"iroha/pkg/agent"
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

// SetStreamedText appends streaming text and updates state.
func (c *ChatComponent) SetStreamedText(text string) {
	c.streamedText += text
	c.state = stateStreaming
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

// Render produces the chat area output.
func (c *ChatComponent) Render(width int) []string {
	if width <= 0 {
		width = 80
	}

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
			fullText = RenderMarkdown(c.streamedText)
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

package tui

import (
	"strings"
)

// ConfirmComponent handles the human-in-the-loop confirmation flow with
// its own edit buffer (not shared with InputComponent).
type ConfirmComponent struct {
	BaseComponent
	prompt      string
	selectIndex int
	diffActive  bool
	diffText    string

	// Edit mode — own buffer, not shared
	editActive bool
	editBuffer []rune
	editCursor int

	// Active tool reference for extracting editable values
	activeToolArgs any

	// Callbacks (wired by App in Phase 3)
	OnRespond func(response string) // sends to BridgeResponder
}

// NewConfirmComponent creates a ConfirmComponent.
func NewConfirmComponent() *ConfirmComponent {
	return &ConfirmComponent{}
}

// Active returns true when the confirmation card should be shown.
func (cc *ConfirmComponent) Active(state TuiState) bool {
	return state == stateConfirming
}

// HandleInput processes key events during confirmation.
func (cc *ConfirmComponent) HandleInput(key Key) bool {
	// Edit mode key handling
	if cc.editActive {
		return cc.handleEditMode(key)
	}

	switch key.Type {
	case KeyLeft, KeyShiftTab:
		cc.selectIndex = (cc.selectIndex - 1 + 5) % 5
	case KeyRight, KeyTab:
		cc.selectIndex = (cc.selectIndex + 1) % 5
	case KeyEnter:
		var resp string
		switch cc.selectIndex {
		case 0:
			resp = "y"
		case 1:
			resp = "n"
		case 2:
			resp = "always"
		case 3:
			cc.enterEditMode()
			return true
		case 4:
			resp = "explain"
		}
		if resp != "" && cc.OnRespond != nil {
			cc.OnRespond(resp)
		}
	case KeyRune:
		switch key.Rune {
		case 'd', 'D':
			if cc.diffText != "" {
				cc.diffActive = !cc.diffActive
			}
		case 'y', 'Y':
			if cc.OnRespond != nil {
				cc.OnRespond("y")
			}
		case 'n', 'N':
			if cc.OnRespond != nil {
				cc.OnRespond("n")
			}
		case 'a', 'A':
			if cc.OnRespond != nil {
				cc.OnRespond("always")
			}
		case 'e', 'E':
			cc.enterEditMode()
		case '?':
			if cc.OnRespond != nil {
				cc.OnRespond("explain")
			}
		}
	default:
		return false
	}
	return true
}

// OnStateChange reacts to state transitions.
func (cc *ConfirmComponent) OnStateChange(oldState, newState TuiState) {
	// No special reaction needed
}

// SetPrompt sets the confirmation prompt, extracting diff if present.
func (cc *ConfirmComponent) SetPrompt(prompt string) {
	const diffMarker = "\n\n\x1b[1;34m[File Changes (Diff)]:\x1b[0m\n"
	if idx := strings.Index(prompt, diffMarker); idx != -1 {
		cc.prompt = prompt[:idx]
		cc.diffText = prompt[idx+len(diffMarker):]
	} else {
		cc.prompt = prompt
		cc.diffText = ""
	}
	cc.selectIndex = 0
	cc.diffActive = false
}

// Render produces the confirmation card output.
func (cc *ConfirmComponent) Render(width int) []string {
	if cc.editActive {
		var lines []string
		lines = append(lines, "", StyleKeyActive.Render("Editing Tool Arguments"))
		lines = append(lines, "  Press [Enter] to run with modified arguments. Press [Esc] to cancel.", "")
		return lines
	}

	card := RenderConfirmCardWithDiff(cc.prompt, cc.selectIndex, cc.diffText != "", cc.diffActive)

	var content string
	// We don't have streamedText here — just render the card
	content = card

	rendered := StyleAgentMsg.Render(content)
	var lines []string
	lines = append(lines, "")
	lines = append(lines, strings.Split(rendered, "\n")...)

	if cc.diffActive && cc.diffText != "" {
		lines = append(lines, strings.Split(cc.diffText, "\n")...)
	}

	return lines
}

// handleEditMode processes key events during argument editing.
func (cc *ConfirmComponent) handleEditMode(key Key) bool {
	switch key.Type {
	case KeyEnter:
		editedVal := string(cc.editBuffer)
		cc.editActive = false
		cc.editBuffer = nil
		cc.editCursor = 0
		if cc.OnRespond != nil {
			cc.OnRespond("edit:" + editedVal)
		}
	case KeyEsc:
		cc.editActive = false
		cc.editBuffer = nil
		cc.editCursor = 0
	case KeyBackspace:
		if cc.editCursor > 0 {
			cc.editBuffer = append(cc.editBuffer[:cc.editCursor-1], cc.editBuffer[cc.editCursor:]...)
			cc.editCursor--
		}
	case KeyLeft:
		if cc.editCursor > 0 {
			cc.editCursor--
		}
	case KeyRight:
		if cc.editCursor < len(cc.editBuffer) {
			cc.editCursor++
		}
	case KeyRune:
		cc.editBuffer = append(cc.editBuffer[:cc.editCursor], append([]rune{key.Rune}, cc.editBuffer[cc.editCursor:]...)...)
		cc.editCursor++
	default:
		return false
	}
	return true
}

// enterEditMode copies the editable value into the component's own buffer.
func (cc *ConfirmComponent) enterEditMode() {
	editableVal := cc.getEditableValue()
	cc.editActive = true
	cc.editBuffer = []rune(editableVal)
	cc.editCursor = len(cc.editBuffer)
}

// getEditableValue extracts the editable string from active tool args.
func (cc *ConfirmComponent) getEditableValue() string {
	if cc.activeToolArgs == nil {
		return ""
	}
	if argMap, ok := cc.activeToolArgs.(map[string]any); ok {
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

// EditBuffer returns the current edit buffer content.
func (cc *ConfirmComponent) EditBuffer() string {
	return string(cc.editBuffer)
}


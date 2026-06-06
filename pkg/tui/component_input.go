package tui

import (
	"strings"
)

// InputComponent manages the user input buffer, cursor movement, text editing,
// history navigation, and slash menu integration.
type InputComponent struct {
	BaseComponent
	focus     *FocusModel
	history   *HistoryManager
	slashMenu *SlashMenuComponent

	// Callbacks (wired by App in Phase 3)
	OnSubmit   func(prompt string)   // triggers agent execution
	OnSlashCmd func(cmd string) bool // handles slash commands
}

// NewInputComponent creates an InputComponent.
func NewInputComponent(focus *FocusModel, history *HistoryManager) *InputComponent {
	return &InputComponent{
		focus:   focus,
		history: history,
	}
}

// Active returns true when the input should receive key events.
func (ic *InputComponent) Active(state TuiState) bool {
	return state == statePrompt
}

// HandleInput processes key events for the input buffer.
func (ic *InputComponent) HandleInput(key Key) bool {
	if !ic.focus.Is(FocusPrompt) {
		return false
	}

	switch key.Type {
	case KeyUp:
		if ic.slashMenu != nil && ic.slashMenu.active {
			ic.slashMenu.MoveUp()
		} else if ic.history != nil {
			ic.focus.Buffer = []rune(ic.history.Up())
			ic.focus.CursorIndex = len(ic.focus.Buffer)
		}
	case KeyDown:
		if ic.slashMenu != nil && ic.slashMenu.active {
			ic.slashMenu.MoveDown()
		} else if ic.history != nil {
			ic.focus.Buffer = []rune(ic.history.Down())
			ic.focus.CursorIndex = len(ic.focus.Buffer)
		}
	case KeyLeft:
		if ic.focus.CursorIndex > 0 {
			ic.focus.CursorIndex--
		}
	case KeyRight:
		if ic.focus.CursorIndex < len(ic.focus.Buffer) {
			ic.focus.CursorIndex++
		}
	case KeyBackspace:
		if ic.focus.CursorIndex > 0 {
			ic.focus.Buffer = append(ic.focus.Buffer[:ic.focus.CursorIndex-1], ic.focus.Buffer[ic.focus.CursorIndex:]...)
			ic.focus.CursorIndex--
			ic.updateSlashMenu()
		}
	case KeyAltEnter:
		ic.focus.Buffer = append(ic.focus.Buffer[:ic.focus.CursorIndex], append([]rune{'\n'}, ic.focus.Buffer[ic.focus.CursorIndex:]...)...)
		ic.focus.CursorIndex++
	case KeyTab:
		if ic.slashMenu != nil && ic.slashMenu.active && len(ic.slashMenu.items) > 0 {
			selected := ic.slashMenu.items[ic.slashMenu.index]
			ic.focus.Buffer = []rune(selected.Command + " ")
			ic.focus.CursorIndex = len(ic.focus.Buffer)
			ic.slashMenu.Close()
			return true
		}
	case KeyEsc:
		if ic.slashMenu != nil && ic.slashMenu.active {
			ic.slashMenu.Close()
			return true
		}
	case KeyEnter:
		if ic.slashMenu != nil && ic.slashMenu.active && len(ic.slashMenu.items) > 0 {
			selected := ic.slashMenu.items[ic.slashMenu.index]
			ic.focus.Buffer = []rune(selected.Command)
			ic.focus.CursorIndex = len(ic.focus.Buffer)
			ic.slashMenu.Close()
		}

		inputVal := strings.TrimSpace(string(ic.focus.Buffer))
		if inputVal == "" {
			return true
		}

		if strings.HasPrefix(inputVal, "/") {
			if ic.OnSlashCmd != nil {
				return ic.OnSlashCmd(inputVal)
			}
			return false
		}

		if ic.OnSubmit != nil {
			ic.OnSubmit(inputVal)
		}

		// Clear buffer after submit
		ic.focus.Buffer = nil
		ic.focus.CursorIndex = 0

	case KeyRune:
		ic.focus.Buffer = append(ic.focus.Buffer[:ic.focus.CursorIndex], append([]rune{key.Rune}, ic.focus.Buffer[ic.focus.CursorIndex:]...)...)
		ic.focus.CursorIndex++
		ic.updateSlashMenu()
	default:
		return false
	}
	return true
}

// OnStateChange reacts to state transitions.
func (ic *InputComponent) OnStateChange(oldState, newState TuiState) {
	if newState == statePrompt && oldState != statePrompt {
		ic.focus.Take(FocusPrompt)
	}
}

// Render produces the input area output.
func (ic *InputComponent) Render(width int) []string {
	promptPrefix := "┃ "
	inputVal := string(ic.focus.Buffer)
	prefixWidth := visualWidth(promptPrefix)
	wrapped := WrapInput(inputVal, prefixWidth, width)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}

	lines := make([]string, len(wrapped))
	continuationPrefix := strings.Repeat(" ", prefixWidth)
	for i, line := range wrapped {
		if i == 0 {
			lines[i] = promptPrefix + line
			continue
		}
		lines[i] = continuationPrefix + line
	}
	return lines
}

// SetSlashMenu sets the slash menu component reference.
func (ic *InputComponent) SetSlashMenu(sm *SlashMenuComponent) {
	ic.slashMenu = sm
}

func (ic *InputComponent) updateSlashMenu() {
	if ic.slashMenu == nil {
		return
	}
	input := string(ic.focus.Buffer)
	ic.slashMenu.Update(input)
}

// Clear resets the input buffer.
func (ic *InputComponent) Clear() {
	ic.focus.Buffer = nil
	ic.focus.CursorIndex = 0
}

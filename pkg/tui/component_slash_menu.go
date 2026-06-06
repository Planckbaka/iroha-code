package tui

import "strings"

// SlashMenuComponent filters and renders slash commands for the input area.
type SlashMenuComponent struct {
	BaseComponent
	active bool
	items  []SlashMenuItem
	index  int
	all    []SlashMenuItem
}

// NewSlashMenuComponent creates a SlashMenuComponent with the given commands.
func NewSlashMenuComponent(commands []SlashMenuItem) *SlashMenuComponent {
	return &SlashMenuComponent{
		all: commands,
	}
}

// Active returns true when the slash menu is visible.
func (sm *SlashMenuComponent) Active(state TuiState) bool {
	return sm.active && state == statePrompt
}

// HandleInput processes key events for slash menu navigation.
func (sm *SlashMenuComponent) HandleInput(key Key) bool {
	if !sm.active {
		return false
	}
	switch key.Type {
	case KeyUp:
		sm.MoveUp()
	case KeyDown:
		sm.MoveDown()
	case KeyEsc:
		sm.Close()
	case KeyEnter, KeyTab:
		if len(sm.items) > 0 {
			sm.Close()
		}
	default:
		return false
	}
	return true
}

// OnStateChange reacts to state transitions.
func (sm *SlashMenuComponent) OnStateChange(oldState, newState TuiState) {}

// Update filters commands based on the current input.
func (sm *SlashMenuComponent) Update(input string) {
	if !strings.HasPrefix(input, "/") {
		sm.active = false
		sm.items = nil
		return
	}
	sm.active = true
	prefix := strings.ToLower(input)
	var matched []SlashMenuItem
	for _, cmd := range sm.all {
		if strings.HasPrefix(strings.ToLower(cmd.Command), prefix) {
			matched = append(matched, cmd)
		}
	}
	sm.items = matched
	if len(matched) == 0 {
		sm.active = false
		return
	}
	if sm.index >= len(sm.items) {
		sm.index = 0
	}
}

// MoveUp moves selection up.
func (sm *SlashMenuComponent) MoveUp() {
	if len(sm.items) == 0 {
		return
	}
	sm.index = (sm.index - 1 + len(sm.items)) % len(sm.items)
}

// MoveDown moves selection down.
func (sm *SlashMenuComponent) MoveDown() {
	if len(sm.items) == 0 {
		return
	}
	sm.index = (sm.index + 1) % len(sm.items)
}

// Close hides the menu.
func (sm *SlashMenuComponent) Close() {
	sm.active = false
	sm.items = nil
	sm.index = 0
}

// Render produces the slash menu output.
func (sm *SlashMenuComponent) Render(width int) []string {
	if !sm.active || len(sm.items) == 0 {
		return nil
	}
	var lines []string
	for i, cmd := range sm.items {
		prefix := "  "
		if i == sm.index {
			prefix = "> "
		}
		lines = append(lines, prefix+cmd.Command+" - "+cmd.Description)
	}
	return lines
}

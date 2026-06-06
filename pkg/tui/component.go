package tui

// Component is the base interface for all TUI components.
// Inspired by pi-tui's retained mode component model.
// Components communicate via callback fields, not direct references to App.
type Component interface {
	// Render produces the visual output for this component given available width.
	Render(width int) []string

	// HandleInput processes a key event. Returns true if the event was consumed.
	HandleInput(key Key) bool

	// Active returns whether this component should receive input in the given state.
	Active(state TuiState) bool

	// OnStateChange is called when the global TUI state transitions.
	// Components use this to react to state changes (e.g., ChatComponent
	// starts showing streaming text when state becomes stateStreaming).
	OnStateChange(oldState, newState TuiState)
}

// BaseComponent provides a shared embedding point for all components.
type BaseComponent struct{}

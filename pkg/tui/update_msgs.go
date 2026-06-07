package tui

// StartupPromptMsg carries a CLI-provided trailing prompt to be executed once
// the App event loop is ready.
type StartupPromptMsg struct {
	Prompt string
}

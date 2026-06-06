package tui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Terminal agent palette. Keep core chrome quiet and reserve strong color for
// state changes that matter: active work, success, warning, and failure.
var (
	ColorPrimary   = lipgloss.Color("#7DD3FC")
	ColorSecondary = lipgloss.Color("#A1A1AA")
	ColorSuccess   = lipgloss.Color("#22C55E")
	ColorWarning   = lipgloss.Color("#F59E0B")
	ColorDanger    = lipgloss.Color("#F43F5E")
	ColorTextMuted = lipgloss.Color("#71717A")
	ColorText      = lipgloss.Color("#E4E4E7")
	ColorBorder    = lipgloss.Color("#3F3F46")
	ColorPanel     = lipgloss.Color("#18181B")
)

// Shared card styles — hoisted from repeated inline declarations across view.go.
var (
	// cardStyleCompact is the standard padded card used by most dashboard renderers.
	cardStyleCompact = lipgloss.NewStyle().Padding(1, 2).MarginTop(1).MarginBottom(1)

	// cardStyleSlim is a narrower card variant with less horizontal padding.
	cardStyleSlim = lipgloss.NewStyle().Padding(0, 1).MarginTop(1).MarginBottom(1)

	// cardStyleFlush is a borderless, zero-padding card used by the help overlay.
	cardStyleFlush = lipgloss.NewStyle().Padding(0, 0).MarginTop(1).MarginBottom(1)

	// cardStyleBordered is a rounded-border card used by the background dashboard.
	cardStyleBordered = lipgloss.NewStyle().
		Padding(0, 1).MarginTop(1).MarginBottom(1).
		Border(lipgloss.RoundedBorder()).BorderForeground(ColorPrimary)
)

// sanitizedWidth returns a safe positive width, defaulting to 80 when the
// provided value is zero or negative.
func sanitizedWidth(w int) int {
	if w <= 0 {
		return 80
	}
	return w
}

// Lipgloss Styles
var (
	StylePrompt = lipgloss.NewStyle().
			Foreground(ColorText).
			Bold(true)

	StyleWelcome = lipgloss.NewStyle().
			Foreground(ColorText).
			Padding(0, 2).
			MarginTop(1).
			MarginBottom(1)

	StyleUserMsg = lipgloss.NewStyle().
			Foreground(ColorText).
			Bold(true).
			MarginTop(1)

	StyleAgentMsg = lipgloss.NewStyle().
			Foreground(ColorText).
			MarginTop(1)



	StyleToolSuccess = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				Bold(true)

	StyleToolError = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)



	StyleKeyHelp = lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Italic(true)

	StyleKeyActive = lipgloss.NewStyle().
			Foreground(ColorText).
			Bold(true)

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(ColorTextMuted)



	// StyleSpinner styles the braille spinner frame (hoisted from per-frame
	// allocation in the render loop).
	StyleSpinner = lipgloss.NewStyle().Foreground(ColorSecondary)

	// StyleThinkingText styles the "thinking..." label during stateThinking.
	StyleThinkingText = lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true)
)

// spinnerFrames holds the braille animation frames shared across render states.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// currentSpinnerFrame returns the styled spinner glyph for the current time.
func currentSpinnerFrame() string {
	frame := spinnerFrames[(time.Now().UnixNano()/100000000)%int64(len(spinnerFrames))]
	return StyleSpinner.Render(frame)
}

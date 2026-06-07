package tui

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// WordWrap wraps text to fit within the given visual width. ANSI escape
// sequences are preserved and not counted toward width.
func WordWrap(text string, width int) string {
	if width <= 0 || text == "" {
		return text
	}

	return xansi.Hardwrap(text, width, false)
}

// WrapInput wraps a long input line at the terminal width, accounting for
// the prompt prefix ("┃ " = 2 chars).
func WrapInput(input string, prefixLen, width int) []string {
	maxWidth := width - prefixLen
	if maxWidth <= 0 {
		maxWidth = 1
	}
	wrapped := WordWrap(input, maxWidth)
	return strings.Split(wrapped, "\n")
}

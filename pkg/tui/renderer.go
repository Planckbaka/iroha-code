package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/muesli/termenv"
	"github.com/charmbracelet/lipgloss"
)

// RawRenderer manages frame buffers, terminal sizes, and performs flicker-free 
// differential redraws on the terminal's main screen.
type RawRenderer struct {
	out           io.Writer
	oldLines      []string
	profile       termenv.Profile
	cursorUpLines int
}

// NewRawRenderer initializes a new RawRenderer with default color profile.
func NewRawRenderer(out io.Writer) *RawRenderer {
	return &RawRenderer{
		out:     out,
		profile: termenv.ColorProfile(),
	}
}

// Reset clears the cached screen state buffer.
func (r *RawRenderer) Reset() {
	r.oldLines = nil
	r.cursorUpLines = 0
}

// Draw performs a differential redraw to update the screen from r.oldLines to newLines.
func (r *RawRenderer) Draw(newLines []string, cursorRow, cursorCol int) {
	// Restore hardware cursor position to the bottom of the screen
	if r.cursorUpLines > 0 {
		fmt.Fprintf(r.out, "\x1b[%dB", r.cursorUpLines)
		r.cursorUpLines = 0
	}

	// Flatten all elements in newLines by splitting by \n to ensure 1 element = 1 console row
	var flatLines []string
	for _, line := range newLines {
		parts := strings.Split(line, "\n")
		for _, part := range parts {
			part = strings.ReplaceAll(part, "\r", "")
			flatLines = append(flatLines, part)
		}
	}
	newLines = flatLines

	// Enable Synchronized Output to prevent tearing and screen flicker in modern terminals
	fmt.Fprint(r.out, "\x1b[?2026h")
	defer fmt.Fprint(r.out, "\x1b[?2026l")

	if len(r.oldLines) == 0 {
		// First draw: simply print all new lines sequentially
		for _, line := range newLines {
			fmt.Fprintf(r.out, "\r\x1b[K%s\n", line)
		}
		r.oldLines = make([]string, len(newLines))
		copy(r.oldLines, newLines)
		return
	}

	// Find the first line where the old and new content differ
	firstDiff := len(r.oldLines)
	minLen := len(r.oldLines)
	if len(newLines) < minLen {
		minLen = len(newLines)
	}

	for i := 0; i < minLen; i++ {
		if r.oldLines[i] != newLines[i] {
			firstDiff = i
			break
		}
	}

	// If new output is shorter, first diff could be at the new length boundary
	if firstDiff == len(r.oldLines) && len(newLines) < len(r.oldLines) {
		firstDiff = len(newLines)
	}

	// If no differences found and lengths are identical, do nothing
	if firstDiff == len(r.oldLines) && len(newLines) == len(r.oldLines) {
		return
	}

	// 1. Move cursor up to the first differing line
	upLines := len(r.oldLines) - firstDiff
	if upLines > 0 {
		fmt.Fprintf(r.out, "\x1b[%dA", upLines)
	}

	// 2. Overwrite from the first diff line onwards
	for i := firstDiff; i < len(newLines); i++ {
		// Carriage return + Clear-to-EOL + Write new content
		line := newLines[i]
		// Clean trailing carriage returns/newlines to prevent layout breakage
		line = strings.ReplaceAll(line, "\r", "")
		line = strings.ReplaceAll(line, "\n", "")
		fmt.Fprintf(r.out, "\r\x1b[K%s\n", line)
	}

	// 3. Clear any leftover trailing lines if the new output is shorter than the old output
	if len(r.oldLines) > len(newLines) {
		extra := len(r.oldLines) - len(newLines)
		for i := 0; i < extra; i++ {
			fmt.Fprint(r.out, "\r\x1b[K\n")
		}
		// Move cursor back up to the end of the new output
		fmt.Fprintf(r.out, "\x1b[%dA", extra)
	}

	// Cache the drawn lines
	r.oldLines = make([]string, len(newLines))
	copy(r.oldLines, newLines)

	// Position terminal hardware cursor exactly on the calculated coordinates
	// to ensure IME input method candidate windows align perfectly.
	if cursorRow != -1 {
		up := len(newLines) - cursorRow
		if up > 0 {
			fmt.Fprintf(r.out, "\x1b[%dA", up)
		}
		// Carriage return + move right to the software cursor column
		fmt.Fprintf(r.out, "\r\x1b[%dC", cursorCol-1)
		r.cursorUpLines = up
	}
}

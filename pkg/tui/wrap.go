package tui

import (
	"strings"
)

// WordWrap wraps text to fit within the given visual width, respecting
// word boundaries. ANSI escape sequences are preserved and not counted
// toward width.
func WordWrap(text string, width int) string {
	if width <= 0 || text == "" {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for lineIdx, line := range lines {
		if lineIdx > 0 {
			result.WriteByte('\n')
		}
		wrapped := wrapLine(line, width)
		result.WriteString(wrapped)
	}

	return result.String()
}

// wrapLine wraps a single line at word boundaries.
func wrapLine(line string, width int) string {
	if visualWidth(line) <= width {
		return line
	}

	// Strip and track ANSI for width calculation
	var result strings.Builder
	var currentWord strings.Builder
	var ansiSeq strings.Builder
	inAnsi := false
	currentLen := 0
	wordLen := 0

	for _, r := range line {
		if inAnsi {
			ansiSeq.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inAnsi = false
				currentWord.WriteString(ansiSeq.String())
				ansiSeq.Reset()
			}
			continue
		}

		if r == '\x1b' {
			inAnsi = true
			ansiSeq.WriteRune(r)
			continue
		}

		if r == ' ' {
			if currentLen+wordLen+1 > width && currentLen > 0 {
				result.WriteByte('\n')
				currentLen = 0
			} else if currentLen > 0 {
				result.WriteByte(' ')
				currentLen++
			}
			result.WriteString(currentWord.String())
			currentLen += wordLen
			currentWord.Reset()
			wordLen = 0
		} else {
			currentWord.WriteRune(r)
			wordLen++
		}

		// Hard wrap if a single word exceeds width
		if wordLen >= width {
			result.WriteString(currentWord.String()[:width])
			result.WriteByte('\n')
			currentWord.Reset()
			remaining := currentWord.String()[width:]
			currentWord.Reset()
			currentWord.WriteString(remaining)
			wordLen = len(remaining)
			currentLen = 0
		}
	}

	// Flush remaining word
	if currentWord.Len() > 0 {
		if currentLen+wordLen > width && currentLen > 0 {
			result.WriteByte('\n')
		} else if currentLen > 0 {
			result.WriteByte(' ')
		}
		result.WriteString(currentWord.String())
	}

	return result.String()
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

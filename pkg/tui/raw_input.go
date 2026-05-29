package tui

import (
	"context"
	"io"
	"os"

	"golang.org/x/term"
)

// Key represents a parsed terminal keystroke or ANSI escape sequence
type Key struct {
	Type  KeyType
	Rune  rune
	Bytes []byte
}

// KeyType enumerates the different keyboard interactions supported in raw mode
type KeyType int

const (
	KeyRune KeyType = iota
	KeyEnter
	KeyAltEnter
	KeyBackspace
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyTab
	KeyShiftTab
	KeyEsc
	KeyCtrlC
	KeyCtrlD
	KeyCtrlY
	KeyPgUp
	KeyPgDown
)

// ReadRawKeys runs an input scanning loop on os.Stdin in raw terminal mode.
// It executes the onKey callback for each parsed key sequence.
func ReadRawKeys(ctx context.Context, onKey func(Key) bool) error {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	buf := make([]byte, 32)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if n == 0 {
			continue
		}

		keys := parseBytes(buf[:n])
		for _, k := range keys {
			if !onKey(k) {
				return nil
			}
		}
	}
}

func parseBytes(b []byte) []Key {
	var keys []Key

	i := 0
	for i < len(b) {
		if b[i] == 3 { // Ctrl+C
			keys = append(keys, Key{Type: KeyCtrlC, Bytes: []byte{3}})
			i++
			continue
		}
		if b[i] == 4 { // Ctrl+D
			keys = append(keys, Key{Type: KeyCtrlD, Bytes: []byte{4}})
			i++
			continue
		}
		if b[i] == 25 { // Ctrl+Y (Copy response)
			keys = append(keys, Key{Type: KeyCtrlY, Bytes: []byte{25}})
			i++
			continue
		}
		if b[i] == 127 || b[i] == 8 { // Backspace
			keys = append(keys, Key{Type: KeyBackspace, Bytes: []byte{b[i]}})
			i++
			continue
		}
		if b[i] == 9 { // Tab
			keys = append(keys, Key{Type: KeyTab, Bytes: []byte{9}})
			i++
			continue
		}
		if b[i] == 13 || b[i] == 10 { // Enter
			keys = append(keys, Key{Type: KeyEnter, Bytes: []byte{b[i]}})
			i++
			continue
		}

		// ANSI Escape sequences
		if b[i] == 27 {
			if i+1 < len(b) {
				// Alt+Enter
				if b[i+1] == 13 || b[i+1] == 10 {
					keys = append(keys, Key{Type: KeyAltEnter, Bytes: []byte{27, b[i+1]}})
					i += 2
					continue
				}
				// Arrow keys, Shift+Tab, Page Up/Down
				if b[i+1] == '[' {
					if i+2 < len(b) {
						switch b[i+2] {
						case 'A': // Up
							keys = append(keys, Key{Type: KeyUp, Bytes: b[i : i+3]})
							i += 3
							continue
						case 'B': // Down
							keys = append(keys, Key{Type: KeyDown, Bytes: b[i : i+3]})
							i += 3
							continue
						case 'C': // Right
							keys = append(keys, Key{Type: KeyRight, Bytes: b[i : i+3]})
							i += 3
							continue
						case 'D': // Left
							keys = append(keys, Key{Type: KeyLeft, Bytes: b[i : i+3]})
							i += 3
							continue
						case 'Z': // Shift+Tab
							keys = append(keys, Key{Type: KeyShiftTab, Bytes: b[i : i+3]})
							i += 3
							continue
						}
					}
					// Extended escape sequences (PgUp: \x1b[5~ / PgDn: \x1b[6~)
					if i+3 < len(b) && b[i+3] == '~' {
						if b[i+2] == '5' {
							keys = append(keys, Key{Type: KeyPgUp, Bytes: b[i : i+4]})
							i += 4
							continue
						}
						if b[i+2] == '6' {
							keys = append(keys, Key{Type: KeyPgDown, Bytes: b[i : i+4]})
							i += 4
							continue
						}
					}
				}
			}
			// Single Esc
			keys = append(keys, Key{Type: KeyEsc, Bytes: []byte{27}})
			i++
			continue
		}

		// UTF-8 multi-byte decoding
		r, sz := decodeRune(b[i:])
		keys = append(keys, Key{Type: KeyRune, Rune: r, Bytes: b[i : i+sz]})
		i += sz
	}

	return keys
}

func decodeRune(b []byte) (rune, int) {
	if len(b) == 0 {
		return 0, 0
	}
	r := rune(b[0])
	if r < 0x80 {
		return r, 1
	}
	if r&0xE0 == 0xC0 && len(b) >= 2 {
		return (r&0x1F)<<6 | rune(b[1]&0x3F), 2
	}
	if r&0xF0 == 0xE0 && len(b) >= 3 {
		return (r&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F), 3
	}
	if r&0xF8 == 0xF0 && len(b) >= 4 {
		return (r&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F), 4
	}
	return r, 1
}

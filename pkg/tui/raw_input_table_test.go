package tui

import (
	"testing"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// TestParseBytesTable — comprehensive table-driven tests for parseBytes
// ---------------------------------------------------------------------------

func TestParseBytesTable(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantKeys []Key
	}{
		// Control characters
		{
			name:     "Ctrl+C",
			input:    []byte{3},
			wantKeys: []Key{{Type: KeyCtrlC}},
		},
		{
			name:     "Ctrl+D",
			input:    []byte{4},
			wantKeys: []Key{{Type: KeyCtrlD}},
		},
		{
			name:     "Ctrl+Y",
			input:    []byte{25},
			wantKeys: []Key{{Type: KeyCtrlY}},
		},
		{
			name:     "Backspace (127)",
			input:    []byte{127},
			wantKeys: []Key{{Type: KeyBackspace}},
		},
		{
			name:     "Backspace (8)",
			input:    []byte{8},
			wantKeys: []Key{{Type: KeyBackspace}},
		},
		{
			name:     "Tab",
			input:    []byte{9},
			wantKeys: []Key{{Type: KeyTab}},
		},
		{
			name:     "Enter (13)",
			input:    []byte{13},
			wantKeys: []Key{{Type: KeyEnter}},
		},
		{
			name:     "Enter (10)",
			input:    []byte{10},
			wantKeys: []Key{{Type: KeyEnter}},
		},

		// Escape sequences
		{
			name:     "Esc alone",
			input:    []byte{27},
			wantKeys: []Key{{Type: KeyEsc}},
		},
		{
			name:     "Alt+Enter (27 + 13)",
			input:    []byte{27, 13},
			wantKeys: []Key{{Type: KeyAltEnter}},
		},
		{
			name:     "Alt+Enter (27 + 10)",
			input:    []byte{27, 10},
			wantKeys: []Key{{Type: KeyAltEnter}},
		},

		// Arrow keys
		{
			name:     "Arrow Up",
			input:    []byte{27, '[', 'A'},
			wantKeys: []Key{{Type: KeyUp}},
		},
		{
			name:     "Arrow Down",
			input:    []byte{27, '[', 'B'},
			wantKeys: []Key{{Type: KeyDown}},
		},
		{
			name:     "Arrow Right",
			input:    []byte{27, '[', 'C'},
			wantKeys: []Key{{Type: KeyRight}},
		},
		{
			name:     "Arrow Left",
			input:    []byte{27, '[', 'D'},
			wantKeys: []Key{{Type: KeyLeft}},
		},

		// Shift+Tab
		{
			name:     "Shift+Tab",
			input:    []byte{27, '[', 'Z'},
			wantKeys: []Key{{Type: KeyShiftTab}},
		},

		// Page Up / Page Down
		{
			name:     "Page Up (ESC[5~)",
			input:    []byte{27, '[', '5', '~'},
			wantKeys: []Key{{Type: KeyPgUp}},
		},
		{
			name:     "Page Down (ESC[6~)",
			input:    []byte{27, '[', '6', '~'},
			wantKeys: []Key{{Type: KeyPgDown}},
		},

		// SGR mouse wheel events
		{
			name:     "Mouse wheel up",
			input:    []byte("\x1b[<64;10;20M"),
			wantKeys: []Key{{Type: KeyWheelUp}},
		},
		{
			name:     "Mouse wheel down",
			input:    []byte("\x1b[<65;10;20M"),
			wantKeys: []Key{{Type: KeyWheelDown}},
		},

		// UTF-8 multi-byte rune (Japanese)
		{
			name:     "Japanese character (hiragana 'a')",
			input:    []byte("あ"),
			wantKeys: []Key{{Type: KeyRune, Rune: 'あ'}},
		},

		// Default rune fallback
		{
			name:     "ASCII rune 'x'",
			input:    []byte{'x'},
			wantKeys: []Key{{Type: KeyRune, Rune: 'x'}},
		},

		// Multiple keys in sequence
		{
			name:  "Ctrl+C then 'a'",
			input: []byte{3, 'a'},
			wantKeys: []Key{
				{Type: KeyCtrlC},
				{Type: KeyRune, Rune: 'a'},
			},
		},
		{
			name:  "Arrow Up then Arrow Down",
			input: []byte{27, '[', 'A', 27, '[', 'B'},
			wantKeys: []Key{
				{Type: KeyUp},
				{Type: KeyDown},
			},
		},

		// Esc followed by non-bracket (not a sequence) — produces Esc + rune 'a'
		{
			name:  "Esc then 'a' (not a sequence)",
			input: []byte{27, 'a'},
			wantKeys: []Key{
				{Type: KeyEsc},
				{Type: KeyRune, Rune: 'a'},
			},
		},

		// Empty input
		{
			name:     "empty input",
			input:    []byte{},
			wantKeys: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBytes(tt.input)

			if len(got) != len(tt.wantKeys) {
				t.Fatalf("parseBytes returned %d keys, want %d: %+v", len(got), len(tt.wantKeys), got)
			}

			for i, want := range tt.wantKeys {
				if got[i].Type != want.Type {
					t.Errorf("key[%d].Type = %v, want %v", i, got[i].Type, want.Type)
				}
				if want.Rune != 0 && got[i].Rune != want.Rune {
					t.Errorf("key[%d].Rune = %q, want %q", i, got[i].Rune, want.Rune)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestDecodeRuneTable
// ---------------------------------------------------------------------------

func TestDecodeRuneTable(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantRune rune
		wantSize int
	}{
		{
			name:     "empty input",
			input:    []byte{},
			wantRune: 0,
			wantSize: 0,
		},
		{
			name:     "ASCII 'a'",
			input:    []byte{'a'},
			wantRune: 'a',
			wantSize: 1,
		},
		{
			name:     "valid UTF-8 Japanese",
			input:    []byte("あ"),
			wantRune: 'あ',
			wantSize: 3,
		},
		{
			name:     "invalid UTF-8 (0xFF)",
			input:    []byte{0xFF},
			wantRune: rune(0xFF), // fallback to raw byte
			wantSize: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sz := decodeRune(tt.input)
			if r != tt.wantRune {
				t.Errorf("decodeRune() rune = %U (%q), want %U (%q)", r, r, tt.wantRune, tt.wantRune)
			}
			if sz != tt.wantSize {
				t.Errorf("decodeRune() size = %d, want %d", sz, tt.wantSize)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestParseSGRMouseTable
// ---------------------------------------------------------------------------

func TestParseSGRMouseTable(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		start    int
		wantType KeyType
		wantOK   bool
	}{
		{
			name:     "wheel up button 64",
			input:    []byte("\x1b[<64;10;20M"),
			start:    0,
			wantType: KeyWheelUp,
			wantOK:   true,
		},
		{
			name:     "wheel down button 65",
			input:    []byte("\x1b[<65;10;20M"),
			start:    0,
			wantType: KeyWheelDown,
			wantOK:   true,
		},
		{
			name:     "left click button 0 (non-wheel)",
			input:    []byte("\x1b[<0;10;20M"),
			start:    0,
			wantType: KeyEsc, // non-wheel returns Esc type
			wantOK:   true,
		},
		{
			name:   "incomplete sequence (no M/m terminator)",
			input:  []byte("\x1b[<64;10;20"),
			start:  0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, _, ok := parseSGRMouse(tt.input, tt.start)
			if ok != tt.wantOK {
				t.Errorf("parseSGRMouse() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if ok && key.Type != tt.wantType {
				t.Errorf("parseSGRMouse() key.Type = %v, want %v", key.Type, tt.wantType)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestKeyBytesField
// ---------------------------------------------------------------------------

func TestKeyBytesField(t *testing.T) {
	// Verify that key Bytes field is populated correctly
	keys := parseBytes([]byte{27, '[', 'A'})
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if len(keys[0].Bytes) != 3 {
		t.Errorf("expected 3 bytes in key.Bytes, got %d", len(keys[0].Bytes))
	}
	if keys[0].Bytes[0] != 27 || keys[0].Bytes[1] != '[' || keys[0].Bytes[2] != 'A' {
		t.Errorf("key.Bytes = %v, want [27 91 65]", keys[0].Bytes)
	}
}

// Ensure unicode/utf8 import is used (required for decodeRune tests)
var _ = utf8.RuneLen

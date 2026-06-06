package tui

import "testing"

func TestParseBytesSGRMouseWheel(t *testing.T) {
	keys := parseBytes([]byte("\x1b[<64;10;20M\x1b[<65;10;20M"))
	if len(keys) != 2 {
		t.Fatalf("expected two wheel keys, got %d: %#v", len(keys), keys)
	}
	if keys[0].Type != KeyWheelUp {
		t.Fatalf("expected wheel up, got %v", keys[0].Type)
	}
	if keys[1].Type != KeyWheelDown {
		t.Fatalf("expected wheel down, got %v", keys[1].Type)
	}
}

func TestParseBytesConsumesNonWheelSGRMouseEvents(t *testing.T) {
	keys := parseBytes([]byte("\x1b[<0;10;20Mabc"))
	if len(keys) != 3 {
		t.Fatalf("expected mouse click to be consumed before runes, got %#v", keys)
	}
	for i, want := range []rune{'a', 'b', 'c'} {
		if keys[i].Type != KeyRune || keys[i].Rune != want {
			t.Fatalf("key %d = %#v, want rune %q", i, keys[i], want)
		}
	}
}

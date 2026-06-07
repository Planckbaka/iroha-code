package tui

import (
	"testing"
)

func TestFocusModel_TakeAndRelease(t *testing.T) {
	f := &FocusModel{}

	if f.Is(FocusNone) != true {
		t.Error("default owner should be FocusNone")
	}

	f.Take(FocusPrompt)
	if !f.Is(FocusPrompt) {
		t.Error("expected FocusPrompt after Take")
	}
	if f.Is(FocusConfirmEdit) {
		t.Error("should not be FocusConfirmEdit")
	}

	f.Take(FocusConfirmEdit)
	if !f.Is(FocusConfirmEdit) {
		t.Error("expected FocusConfirmEdit after Take")
	}

	f.Release()
	if !f.Is(FocusNone) {
		t.Error("expected FocusNone after Release")
	}
}

func TestFocusModel_Buffer(t *testing.T) {
	f := &FocusModel{}
	f.Take(FocusPrompt)
	f.Buffer = []rune("hello")
	f.CursorIndex = 5

	if string(f.Buffer) != "hello" {
		t.Errorf("expected buffer 'hello', got %q", string(f.Buffer))
	}
	if f.CursorIndex != 5 {
		t.Errorf("expected cursor 5, got %d", f.CursorIndex)
	}

	f.Release()
	if f.Is(FocusPrompt) {
		t.Error("should not be FocusPrompt after Release")
	}
}

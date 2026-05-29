package tui

// InputOwner tracks which component currently owns the shared input focus.
type InputOwner int

const (
	FocusNone       InputOwner = iota
	FocusPrompt                // InputComponent owns buffer
	FocusConfirmEdit           // ConfirmComponent owns buffer (edit mode)
)

// FocusModel manages input buffer ownership between components.
// This models the current behavior where confirm-edit mode reuses InputBuffer.
//
// Buffer and CursorIndex are only valid when Owner == FocusPrompt.
// When Owner == FocusConfirmEdit, ConfirmComponent uses its own editBuffer.
type FocusModel struct {
	Owner       InputOwner
	Buffer      []rune
	CursorIndex int
}

// Take transfers input focus to the specified owner.
func (f *FocusModel) Take(owner InputOwner) {
	f.Owner = owner
}

// Release returns focus to none.
func (f *FocusModel) Release() {
	f.Owner = FocusNone
}

// Is reports whether the specified owner currently holds focus.
func (f *FocusModel) Is(owner InputOwner) bool {
	return f.Owner == owner
}

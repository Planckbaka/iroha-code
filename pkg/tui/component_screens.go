package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PermModeEntry describes a permission mode option.
type PermModeEntry struct {
	Label string
	Desc  string
}

// SessionEntry describes a historical session.
type SessionEntry struct {
	ID            string
	LastUpdateStr string
	TotalTokens   int
	TotalCost     float64
	LastMsg       string
}

// ScreenComponent handles permission selection and session selection screens.
type ScreenComponent struct {
	BaseComponent
	screenType       string // "permission" or "session"
	permSelectIndex  int
	sessionListIndex int
	sessionsList     []SessionEntry

	// Callbacks (wired by App)
	OnPermSelect    func(mode string)
	OnSessionSelect func(sessionID string)
	OnNewSession    func()
}

// NewScreenComponent creates a ScreenComponent.
func NewScreenComponent() *ScreenComponent {
	return &ScreenComponent{
		permSelectIndex:  1,
		sessionListIndex: 0,
	}
}

// Active returns true when a selection screen is showing.
func (sc *ScreenComponent) Active(state TuiState) bool {
	return state == statePermissionSelect || state == stateSessionSelect
}

// HandleInput processes key events on selection screens.
func (sc *ScreenComponent) HandleInput(key Key) bool {
	switch key.Type {
	case KeyUp:
		if sc.screenType == "permission" {
			if sc.permSelectIndex > 0 {
				sc.permSelectIndex--
			}
		} else {
			if sc.sessionListIndex > 0 {
				sc.sessionListIndex--
			}
		}
	case KeyDown:
		if sc.screenType == "permission" {
			if sc.permSelectIndex < len(permModeNames)-1 {
				sc.permSelectIndex++
			}
		} else {
			if sc.sessionListIndex < len(sc.sessionsList) {
				sc.sessionListIndex++
			}
		}
	case KeyEnter:
		if sc.screenType == "permission" {
			if sc.OnPermSelect != nil && sc.permSelectIndex < len(permModeNames) {
				sc.OnPermSelect(permModeNames[sc.permSelectIndex].Label)
			}
		} else {
			if sc.sessionListIndex == 0 {
				if sc.OnNewSession != nil {
					sc.OnNewSession()
				}
			} else {
				idx := sc.sessionListIndex - 1
				if idx < len(sc.sessionsList) && sc.OnSessionSelect != nil {
					sc.OnSessionSelect(sc.sessionsList[idx].ID)
				}
			}
		}
	default:
		return false
	}
	return true
}

// OnStateChange reacts to state transitions.
func (sc *ScreenComponent) OnStateChange(oldState, newState TuiState) {
	if newState == statePermissionSelect {
		sc.screenType = "permission"
	} else if newState == stateSessionSelect {
		sc.screenType = "session"
	}
}

// SetPermIndex sets the permission selection index.
func (sc *ScreenComponent) SetPermIndex(idx int) {
	sc.permSelectIndex = idx
}

// SetSessions sets the session list for the session picker.
func (sc *ScreenComponent) SetSessions(sessions []SessionEntry) {
	sc.sessionsList = sessions
}

// SetSessionIndex sets the session list selection index.
func (sc *ScreenComponent) SetSessionIndex(idx int) {
	sc.sessionListIndex = idx
}

// Render produces the selection screen output.
func (sc *ScreenComponent) Render(width int) []string {
	if width <= 0 {
		width = 80
	}

	if sc.screenType == "permission" {
		return sc.renderPermissionScreen()
	}
	return sc.renderSessionScreen()
}

func (sc *ScreenComponent) renderPermissionScreen() []string {
	var lines []string
	lines = append(lines, "", "")
	lines = append(lines, StyleKeyActive.Render("  Select Agent Permission Mode"))
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorTextMuted).
		Render("  This setting controls the security level for Agent tool execution"))
	lines = append(lines, "")

	for i, entry := range permModeNames {
		if i == sc.permSelectIndex {
			pointer := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render("▶ ")
			label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(entry.Label)
			desc := lipgloss.NewStyle().Foreground(lipgloss.Color("#A1A1AA")).Render(entry.Desc)
			lines = append(lines, "  "+pointer+label)
			lines = append(lines, "     "+desc)
		} else {
			label := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Width(16).Render(entry.Label)
			desc := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(entry.Desc)
			lines = append(lines, "     "+label+"  "+desc)
		}
		lines = append(lines, "")
	}

	lines = append(lines, lipgloss.NewStyle().Foreground(ColorTextMuted).
		Render("  Up/Down select   Enter confirm   Ctrl+C exit"))
	return lines
}

func (sc *ScreenComponent) renderSessionScreen() []string {
	var lines []string
	lines = append(lines, "", "")
	lines = append(lines, StyleKeyActive.Render("  Iroha Code - Session History Manager"))
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorTextMuted).
		Render("  Select a session to resume, or start a new session:"))
	lines = append(lines, "")

	// New session entry
	if sc.sessionListIndex == 0 {
		pointer := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render("▶ ")
		label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render("[ Start New Session ]")
		desc := lipgloss.NewStyle().Foreground(lipgloss.Color("#A1A1AA")).Render("Start a fresh session with no history.")
		lines = append(lines, "  "+pointer+label)
		lines = append(lines, "     "+desc)
	} else {
		label := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("[ Start New Session ]")
		desc := lipgloss.NewStyle().Foreground(ColorTextMuted).Render("Start a fresh session with no history.")
		lines = append(lines, "     "+label+"  "+desc)
	}
	lines = append(lines, "")

	for i, sess := range sc.sessionsList {
		if i+1 == sc.sessionListIndex {
			pointer := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render("▶ ")
			label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(sess.LastMsg)
			lines = append(lines, "  "+pointer+fmt.Sprintf("%s  %s", sess.LastUpdateStr, label))
		} else {
			lines = append(lines, "     "+fmt.Sprintf("%s  %s", sess.LastUpdateStr, sess.LastMsg))
		}
		lines = append(lines, "")
	}

	lines = append(lines, lipgloss.NewStyle().Foreground(ColorTextMuted).
		Render("  Up/Down select   Enter confirm   Ctrl+C exit"))
	return lines
}

// permModeNames is defined in view.go — referenced here for the permission screen.
var _ = fmt.Sprintf     // ensure fmt import
var _ = strings.Contains // ensure strings import

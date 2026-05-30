package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"iroha/pkg/agent"
)

// StatusBarComponent renders mode, tokens, cost, and active tool info.
type StatusBarComponent struct {
	BaseComponent
	state            TuiState
	mode             string
	totalTokens      int
	sessionCost      float64
	statusText       string
	activeTool       agent.ToolStatus
	roundStartTime   time.Time
	isGoalMode       bool
	goalText         string
}

// NewStatusBarComponent creates a StatusBarComponent.
func NewStatusBarComponent() *StatusBarComponent {
	return &StatusBarComponent{}
}

// Active returns true — status bar is always visible.
func (sb *StatusBarComponent) Active(state TuiState) bool {
	return true
}

// HandleInput — status bar does not handle input.
func (sb *StatusBarComponent) HandleInput(key Key) bool {
	return false
}

// OnStateChange reacts to state transitions.
func (sb *StatusBarComponent) OnStateChange(oldState, newState TuiState) {
	sb.state = newState
}

// SetMode updates the permission mode string.
func (sb *StatusBarComponent) SetMode(mode string) {
	sb.mode = mode
}

// SetTokenUsage updates token count and cost.
func (sb *StatusBarComponent) SetTokenUsage(tokens int, cost float64) {
	sb.totalTokens = tokens
	sb.sessionCost = cost
}

// SetActiveTool updates the current tool status.
func (sb *StatusBarComponent) SetActiveTool(status agent.ToolStatus) {
	sb.activeTool = status
}

// SetRoundStart records when a new agent round begins.
func (sb *StatusBarComponent) SetRoundStart(t time.Time) {
	sb.roundStartTime = t
}

// SetGoalMode updates goal mode state.
func (sb *StatusBarComponent) SetGoalMode(active bool, text string) {
	sb.isGoalMode = active
	sb.goalText = text
}

// SetStatusText updates the LLM status tag text.
func (sb *StatusBarComponent) SetStatusText(text string) {
	sb.statusText = text
}

// Render produces the status bar output.
func (sb *StatusBarComponent) Render(width int) []string {
	if width <= 0 {
		width = 80
	}
	modeStr := strings.ToLower(string(agent.GlobalPermissionManager.GetMode()))
	if modeStr == "" {
		modeStr = "-"
	}

	var left string
	if sb.statusText != "" && (sb.state == stateThinking || sb.state == stateStreaming) {
		left = fmt.Sprintf("  [thinking] %s", sb.statusText)
	} else if sb.activeTool.Running {
		dur := time.Since(sb.roundStartTime).Round(time.Millisecond)
		activity := FormatToolActivity(sb.activeTool.Name, sb.activeTool.Args)
		if len(activity) > 40 {
			activity = activity[:37] + "..."
		}
		left = fmt.Sprintf("  [tool] %s (%v)", activity, dur)
	} else if sb.state == stateThinking || sb.state == stateStreaming {
		dur := time.Since(sb.roundStartTime).Round(time.Second)
		left = fmt.Sprintf("  [thinking] thinking... (%v)", dur)
	} else {
		left = fmt.Sprintf("  mode:%s", modeStr)
	}

	if sb.isGoalMode && sb.goalText != "" {
		goalText := sb.goalText
		if len(goalText) > 20 {
			goalText = goalText[:17] + "..."
		}
		left = fmt.Sprintf("  🎯 [goal] %s | %s", goalText, strings.TrimPrefix(left, "  "))
	}

	var tokenStr string
	if sb.totalTokens > 0 {
		var tokPart string
		if sb.totalTokens >= 1000 {
			tokPart = fmt.Sprintf("%.1fk", float64(sb.totalTokens)/1000)
		} else {
			tokPart = fmt.Sprintf("%d", sb.totalTokens)
		}
		if sb.sessionCost > 0 {
			var costPart string
			if sb.sessionCost < 0.01 {
				costPart = fmt.Sprintf("$%.4f", sb.sessionCost)
			} else {
				costPart = fmt.Sprintf("$%.2f", sb.sessionCost)
			}
			tokenStr = fmt.Sprintf("%s (%s)", tokPart, costPart)
		} else {
			tokenStr = tokPart
		}
	} else {
		tokenStr = "-"
	}
	right := fmt.Sprintf("[%s] %s  ", modeStr, tokenStr)

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)

	spaces := width - leftWidth - rightWidth
	if spaces < 0 {
		spaces = 0
	}

	barText := left + strings.Repeat(" ", spaces) + right
	return []string{StyleStatusBar.Render(barText)}
}

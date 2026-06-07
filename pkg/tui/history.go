package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// MessageRole identifies the source of a history entry.
type MessageRole string

const (
	RoleUser   MessageRole = "user"
	RoleAgent  MessageRole = "agent"
	RoleSystem MessageRole = "system"
	RoleTool   MessageRole = "tool"
)

// HistoryEntry stores a single conversation turn with structured metadata.
type HistoryEntry struct {
	Role     MessageRole
	Content  string // Raw content (markdown)
	TS       time.Time
	Tokens   int
	Metadata map[string]any // tool name, duration, error, etc.
}

// HistoryStore manages structured conversation history with viewport rendering.
type HistoryStore struct {
	entries        []HistoryEntry
	scrollOffset   int              // 0 = most recent visible
	renderedCache  map[int][]string // entry index -> rendered lines
	cachedWidth    int
	lastTotalLines int
	lastMaxLines   int
}

// NewHistoryStore creates an empty HistoryStore.
func NewHistoryStore() *HistoryStore {
	return &HistoryStore{
		entries:       make([]HistoryEntry, 0),
		renderedCache: make(map[int][]string),
	}
}

// Add appends a new entry without disturbing a user reading older content.
func (s *HistoryStore) Add(entry HistoryEntry) {
	if entry.TS.IsZero() {
		entry.TS = time.Now()
	}
	s.entries = append(s.entries, entry)
}

// Render returns the visible lines for the current viewport.
// width: terminal width for line wrapping
// maxLines: maximum lines to return (terminal height minus fixed UI chrome)
// scrollOffset: how far back the user has scrolled (0 = most recent)
func (s *HistoryStore) Render(width, maxLines int) []string {
	return s.RenderWithTail(width, maxLines, nil)
}

// RenderWithTail renders history and transient lines as one scrollable
// timeline. Transient lines are the active model stream, tool output, or
// confirmation UI that has not yet been committed to history.
func (s *HistoryStore) RenderWithTail(width, maxLines int, tail []string) []string {
	if len(s.entries) == 0 || width <= 0 || maxLines <= 0 {
		if len(tail) == 0 || width <= 0 || maxLines <= 0 {
			return nil
		}
	}

	var allLines []string
	for i, entry := range s.entries {
		rendered := s.renderEntry(i, entry, width)
		allLines = append(allLines, rendered...)
	}
	allLines = append(allLines, tail...)

	totalLines := len(allLines)
	if s.scrollOffset > 0 && s.lastTotalLines > 0 && totalLines > s.lastTotalLines {
		// Keep the same visible content anchored while new events arrive below.
		s.scrollOffset += totalLines - s.lastTotalLines
	}
	s.lastTotalLines = totalLines
	s.lastMaxLines = maxLines
	s.clampScrollOffset()

	if totalLines <= maxLines {
		return allLines
	}

	startIdx := max(0, totalLines-maxLines-s.scrollOffset)
	endIdx := min(totalLines, startIdx+maxLines)

	return allLines[startIdx:endIdx]
}

// ScrollUp moves the viewport toward older entries.
func (s *HistoryStore) ScrollUp(lines int) {
	s.scrollOffset += lines
	s.clampScrollOffset()
}

// ScrollDown moves the viewport toward newer entries.
func (s *HistoryStore) ScrollDown(lines int) {
	s.scrollOffset -= lines
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

// Search returns entry indices matching the query.
func (s *HistoryStore) Search(query string) []int {
	if query == "" {
		return nil
	}
	lower := strings.ToLower(query)
	var results []int
	for i, entry := range s.entries {
		if strings.Contains(strings.ToLower(entry.Content), lower) {
			results = append(results, i)
		}
	}
	return results
}

// InvalidateCache clears the render cache (call on width change).
func (s *HistoryStore) InvalidateCache() {
	s.renderedCache = make(map[int][]string)
	s.cachedWidth = 0
	s.lastTotalLines = 0
}

// Len returns the number of history entries.
func (s *HistoryStore) Len() int {
	return len(s.entries)
}

// ScrollOffset returns the current scroll offset.
func (s *HistoryStore) ScrollOffset() int {
	return s.scrollOffset
}

// renderEntry renders a single entry with caching.
func (s *HistoryStore) renderEntry(idx int, entry HistoryEntry, width int) []string {
	// Check cache
	if s.cachedWidth == width {
		if cached, ok := s.renderedCache[idx]; ok {
			return cached
		}
	}

	// Invalidate all cache on width change
	if s.cachedWidth != width {
		s.renderedCache = make(map[int][]string)
		s.cachedWidth = width
	}

	var rendered string
	switch entry.Role {
	case RoleUser:
		rendered = StyleUserMsg.Render("> " + entry.Content)
	case RoleAgent:
		rendered = StyleAgentMsg.Render(RenderMarkdownWithWidth(entry.Content, max(1, width-2)))
	case RoleSystem, RoleTool:
		rendered = entry.Content
	}

	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")

	// Cache the result
	s.renderedCache[idx] = lines
	return lines
}

// clampScrollOffset ensures scroll doesn't exceed bounds.
func (s *HistoryStore) clampScrollOffset() {
	if s.lastTotalLines == 0 {
		return
	}
	maxScroll := s.maxScrollOffset()
	if s.scrollOffset > maxScroll {
		s.scrollOffset = maxScroll
	}
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

// maxScrollOffset returns the maximum allowed offset from actual rendered lines.
func (s *HistoryStore) maxScrollOffset() int {
	return max(0, s.lastTotalLines-s.lastMaxLines)
}

// ResetScroll resets scroll offset to 0 (bottom/most recent).
func (s *HistoryStore) ResetScroll() {
	s.scrollOffset = 0
}

// PageUp scrolls up by pageLines (terminal height minus chrome).
func (s *HistoryStore) PageUp(pageLines int) {
	if pageLines <= 0 {
		pageLines = 20
	}
	s.ScrollUp(pageLines)
}

// PageDown scrolls down by pageLines (terminal height minus chrome).
func (s *HistoryStore) PageDown(pageLines int) {
	if pageLines <= 0 {
		pageLines = 20
	}
	s.ScrollDown(pageLines)
}

// lipgloss.Width helper — used by wrap.go for visual width measurement.
func visualWidth(s string) int {
	return lipgloss.Width(s)
}

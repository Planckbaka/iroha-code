package tui

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// HistoryStore — table-driven tests
// ---------------------------------------------------------------------------

func TestNewHistoryStore(t *testing.T) {
	s := NewHistoryStore()
	if s == nil {
		t.Fatal("NewHistoryStore returned nil")
	}
	if s.Len() != 0 {
		t.Errorf("expected Len() = 0, got %d", s.Len())
	}
	if s.ScrollOffset() != 0 {
		t.Errorf("expected ScrollOffset() = 0, got %d", s.ScrollOffset())
	}
}

func TestHistoryStoreAdd(t *testing.T) {
	tests := []struct {
		name      string
		entries   []HistoryEntry
		wantLen   int
		wantTimes []bool // true = expect non-zero timestamp
	}{
		{
			name:      "add single user entry",
			entries:   []HistoryEntry{{Role: RoleUser, Content: "hello"}},
			wantLen:   1,
			wantTimes: []bool{true},
		},
		{
			name: "add multiple entries",
			entries: []HistoryEntry{
				{Role: RoleUser, Content: "hi"},
				{Role: RoleAgent, Content: "hello"},
				{Role: RoleSystem, Content: "system msg"},
			},
			wantLen:   3,
			wantTimes: []bool{true, true, true},
		},
		{
			name:      "add entry with explicit timestamp preserves it",
			entries:   []HistoryEntry{{Role: RoleUser, Content: "test", TS: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}},
			wantLen:   1,
			wantTimes: []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewHistoryStore()
			for _, e := range tt.entries {
				s.Add(e)
			}
			if s.Len() != tt.wantLen {
				t.Errorf("Len() = %d, want %d", s.Len(), tt.wantLen)
			}
			for i, wantNonZero := range tt.wantTimes {
				if i >= len(s.entries) {
					break
				}
				got := !s.entries[i].TS.IsZero()
				if got != wantNonZero {
					t.Errorf("entry[%d].TS.IsZero() = %v, want %v", i, !got, !wantNonZero)
				}
			}
		})
	}
}

func TestHistoryStoreAddZeroTime(t *testing.T) {
	s := NewHistoryStore()
	before := time.Now()
	s.Add(HistoryEntry{Role: RoleUser, Content: "auto-timestamp"})
	after := time.Now()

	if s.entries[0].TS.Before(before) || s.entries[0].TS.After(after) {
		t.Error("expected auto-generated timestamp to be between before and after")
	}
}

func TestHistoryStoreAddExplicitTime(t *testing.T) {
	s := NewHistoryStore()
	explicit := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	s.Add(HistoryEntry{Role: RoleUser, Content: "explicit", TS: explicit})

	if !s.entries[0].TS.Equal(explicit) {
		t.Errorf("expected explicit timestamp preserved, got %v", s.entries[0].TS)
	}
}

func TestHistoryStoreRender(t *testing.T) {
	tests := []struct {
		name      string
		entries   []HistoryEntry
		width     int
		maxLines  int
		wantNil   bool
		wantCount int // expected minimum number of lines
	}{
		{
			name:     "empty store returns nil",
			entries:  nil,
			width:    80,
			maxLines: 20,
			wantNil:  true,
		},
		{
			name:     "zero width returns nil",
			entries:  []HistoryEntry{{Role: RoleUser, Content: "hello"}},
			width:    0,
			maxLines: 20,
			wantNil:  true,
		},
		{
			name:     "zero maxLines returns nil",
			entries:  []HistoryEntry{{Role: RoleUser, Content: "hello"}},
			width:    80,
			maxLines: 0,
			wantNil:  true,
		},
		{
			name:      "single user entry renders",
			entries:   []HistoryEntry{{Role: RoleUser, Content: "hello"}},
			width:     80,
			maxLines:  20,
			wantCount: 1,
		},
		{
			name:      "agent entry renders",
			entries:   []HistoryEntry{{Role: RoleAgent, Content: "response"}},
			width:     80,
			maxLines:  20,
			wantCount: 1,
		},
		{
			name:      "system/tool entry renders",
			entries:   []HistoryEntry{{Role: RoleSystem, Content: "system msg"}},
			width:     80,
			maxLines:  20,
			wantCount: 1,
		},
		{
			name:      "tool role entry renders",
			entries:   []HistoryEntry{{Role: RoleTool, Content: "tool output"}},
			width:     80,
			maxLines:  20,
			wantCount: 1,
		},
		{
			name:      "multiple entries render",
			entries:   []HistoryEntry{{Role: RoleUser, Content: "q"}, {Role: RoleAgent, Content: "a"}},
			width:     80,
			maxLines:  20,
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewHistoryStore()
			for _, e := range tt.entries {
				s.Add(e)
			}
			lines := s.Render(tt.width, tt.maxLines)
			if tt.wantNil {
				if lines != nil {
					t.Errorf("expected nil, got %d lines", len(lines))
				}
				return
			}
			if len(lines) < tt.wantCount {
				t.Errorf("expected at least %d lines, got %d", tt.wantCount, len(lines))
			}
		})
	}
}

func TestHistoryStoreRenderViewportClipping(t *testing.T) {
	s := NewHistoryStore()
	// Add enough entries to exceed maxLines
	for i := 0; i < 30; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}

	maxLines := 5
	lines := s.Render(80, maxLines)
	if len(lines) > maxLines {
		t.Errorf("expected at most %d lines, got %d", maxLines, len(lines))
	}
}

func TestHistoryStoreScrollUpDown(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 30; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}
	// Render to set lastTotalLines
	s.Render(80, 10)

	tests := []struct {
		name     string
		action   func()
		wantOff  int
	}{
		{"scroll up 3", func() { s.ScrollUp(3) }, 3},
		{"scroll down 1", func() { s.ScrollDown(1) }, 2},
		{"scroll down past 0 clamps to 0", func() { s.ScrollDown(100) }, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.action()
			if off := s.ScrollOffset(); off != tt.wantOff {
				t.Errorf("ScrollOffset() = %d, want %d", off, tt.wantOff)
			}
		})
	}
}

func TestHistoryStoreScrollUpClampsMax(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 10; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}
	s.Render(80, 10)

	// maxScroll = max(0, lastTotalLines - lastMaxLines) = max(0, 10 - 10) = 0
	s.ScrollUp(100)
	off := s.ScrollOffset()
	// lastTotalLines == lastMaxLines == 10, so maxScroll = 0
	if off != 0 {
		t.Errorf("ScrollOffset() = %d, expected clamped to 0", off)
	}
}

func TestHistoryStoreResetScroll(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 30; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}
	s.Render(80, 10)
	s.ScrollUp(5)
	if s.ScrollOffset() == 0 {
		t.Fatal("expected non-zero offset before reset")
	}
	s.ResetScroll()
	if s.ScrollOffset() != 0 {
		t.Errorf("ScrollOffset() = %d, want 0 after reset", s.ScrollOffset())
	}
}

func TestHistoryStorePageUpDown(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 50; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}
	s.Render(80, 10)

	s.PageUp(5)
	if s.ScrollOffset() != 5 {
		t.Errorf("after PageUp(5): offset = %d, want 5", s.ScrollOffset())
	}
	s.PageDown(3)
	if s.ScrollOffset() != 2 {
		t.Errorf("after PageDown(3): offset = %d, want 2", s.ScrollOffset())
	}
}

func TestHistoryStorePageUpDownDefaultLines(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 100; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}
	s.Render(80, 10)

	// PageUp with 0 or negative should default to 20
	s.PageUp(0)
	if s.ScrollOffset() != 20 {
		t.Errorf("PageUp(0) should default to 20, got %d", s.ScrollOffset())
	}
	s.ResetScroll()
	s.PageUp(-1)
	if s.ScrollOffset() != 20 {
		t.Errorf("PageUp(-1) should default to 20, got %d", s.ScrollOffset())
	}
	s.ResetScroll()
	s.ScrollUp(30)
	s.PageDown(0)
	if s.ScrollOffset() != 10 {
		t.Errorf("PageDown(0) should default to 20, got offset %d", s.ScrollOffset())
	}
}

func TestHistoryStoreSearch(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "hello world"})
	s.Add(HistoryEntry{Role: RoleAgent, Content: "greetings"})
	s.Add(HistoryEntry{Role: RoleUser, Content: "hello again"})

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantFirst int
	}{
		{"empty query returns nil", "", 0, -1},
		{"match hello", "hello", 2, 0},
		{"match greetings", "greetings", 1, 1},
		{"case insensitive", "HELLO", 2, 0},
		{"no match", "missing", 0, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := s.Search(tt.query)
			if len(results) != tt.wantCount {
				t.Errorf("Search(%q) returned %d results, want %d", tt.query, len(results), tt.wantCount)
				return
			}
			if tt.wantFirst >= 0 && len(results) > 0 && results[0] != tt.wantFirst {
				t.Errorf("first result = %d, want %d", results[0], tt.wantFirst)
			}
		})
	}
}

func TestHistoryStoreInvalidateCache(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "test"})

	// Render to populate cache
	s.Render(80, 20)
	if s.cachedWidth != 80 {
		t.Errorf("cachedWidth = %d, want 80", s.cachedWidth)
	}

	s.InvalidateCache()
	if s.cachedWidth != 0 {
		t.Errorf("after InvalidateCache: cachedWidth = %d, want 0", s.cachedWidth)
	}
	if len(s.renderedCache) != 0 {
		t.Errorf("after InvalidateCache: renderedCache should be empty, got %d entries", len(s.renderedCache))
	}
	if s.lastTotalLines != 0 {
		t.Errorf("after InvalidateCache: lastTotalLines = %d, want 0", s.lastTotalLines)
	}
}

func TestHistoryStoreRenderWithTail(t *testing.T) {
	tests := []struct {
		name      string
		entries   []HistoryEntry
		tail      []string
		width     int
		maxLines  int
		wantNil   bool
		wantCount int
	}{
		{
			name:     "empty entries with tail",
			entries:  nil,
			tail:     []string{"streaming line 1", "streaming line 2"},
			width:    80,
			maxLines: 20,
			wantNil:  false,
		},
		{
			name:     "empty entries and empty tail returns nil",
			entries:  nil,
			tail:     nil,
			width:    80,
			maxLines: 20,
			wantNil:  true,
		},
		{
			name:     "entries with tail appends tail",
			entries:  []HistoryEntry{{Role: RoleSystem, Content: "history"}},
			tail:     []string{"tail1", "tail2"},
			width:    80,
			maxLines: 20,
			wantNil:  false,
		},
		{
			name:     "zero width with tail returns nil",
			entries:  nil,
			tail:     []string{"line"},
			width:    0,
			maxLines: 20,
			wantNil:  true,
		},
		{
			name:     "zero maxLines with tail returns nil",
			entries:  nil,
			tail:     []string{"line"},
			width:    80,
			maxLines: 0,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewHistoryStore()
			for _, e := range tt.entries {
				s.Add(e)
			}
			lines := s.RenderWithTail(tt.width, tt.maxLines, tt.tail)
			if tt.wantNil {
				if lines != nil {
					t.Errorf("expected nil, got %d lines", len(lines))
				}
				return
			}
			if len(lines) == 0 {
				t.Error("expected non-nil, non-empty lines")
			}
		})
	}
}

func TestHistoryStoreRenderWithTailViewportClipping(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 20; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}
	tail := []string{"tail1", "tail2"}
	maxLines := 5
	lines := s.RenderWithTail(80, maxLines, tail)
	if len(lines) > maxLines {
		t.Errorf("expected at most %d lines, got %d", maxLines, len(lines))
	}
}

func TestHistoryStoreRenderCachingWidth(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "cache test"})

	// Render at width 80
	s.Render(80, 20)
	if s.cachedWidth != 80 {
		t.Errorf("cachedWidth = %d, want 80", s.cachedWidth)
	}

	// Render at width 40 — should invalidate and re-cache
	s.Render(40, 20)
	if s.cachedWidth != 40 {
		t.Errorf("cachedWidth = %d, want 40", s.cachedWidth)
	}
}

func TestHistoryStoreRenderEntryRoles(t *testing.T) {
	tests := []struct {
		name   string
		entry  HistoryEntry
		width  int
	}{
		{"user role", HistoryEntry{Role: RoleUser, Content: "user msg"}, 80},
		{"agent role", HistoryEntry{Role: RoleAgent, Content: "agent msg"}, 80},
		{"system role", HistoryEntry{Role: RoleSystem, Content: "system msg"}, 80},
		{"tool role", HistoryEntry{Role: RoleTool, Content: "tool msg"}, 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewHistoryStore()
			s.Add(tt.entry)
			lines := s.Render(tt.width, 20)
			if len(lines) == 0 {
				t.Error("expected at least 1 rendered line")
			}
		})
	}
}

func TestHistoryStoreRenderUserEntryContainsContent(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "hello"})
	lines := s.Render(80, 20)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "hello") {
		t.Error("expected rendered output to contain 'hello'")
	}
}

func TestHistoryStoreScrollAnchor(t *testing.T) {
	// Test that scrollOffset adjusts when new entries arrive below
	s := NewHistoryStore()
	for i := 0; i < 20; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}
	s.Render(80, 5) // sets lastTotalLines = 20
	s.ScrollUp(5)   // scrollOffset = 5

	// Add more entries
	for i := 0; i < 5; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "new"})
	}
	s.RenderWithTail(80, 5, nil)
	// scrollOffset should have been adjusted upward by 5 (new entries)
	if s.ScrollOffset() < 5 {
		t.Errorf("expected scrollOffset >= 5 after new entries, got %d", s.ScrollOffset())
	}
}

func TestVisualWidth(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantGT int // result should be > this
	}{
		{"plain ASCII", "hello", 0},
		{"empty string", "", -1},
		{"ANSI codes", "\x1b[31mred\x1b[0m", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := visualWidth(tt.input)
			if tt.wantGT < 0 {
				// empty string special case
				if w != 0 {
					t.Errorf("visualWidth(%q) = %d, want 0", tt.input, w)
				}
			} else if w <= tt.wantGT {
				t.Errorf("visualWidth(%q) = %d, want > %d", tt.input, w, tt.wantGT)
			}
		})
	}
}

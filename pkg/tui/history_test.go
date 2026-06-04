package tui

import (
	"strings"
	"testing"
	"time"
)

func TestHistoryStore_Add(t *testing.T) {
	s := NewHistoryStore()
	if s.Len() != 0 {
		t.Error("new store should be empty")
	}

	s.Add(HistoryEntry{Role: RoleUser, Content: "hello"})
	s.Add(HistoryEntry{Role: RoleAgent, Content: "world"})

	if s.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", s.Len())
	}

	e, ok := s.Entry(0)
	if !ok || e.Role != RoleUser || e.Content != "hello" {
		t.Error("first entry should be user message 'hello'")
	}

	e, ok = s.Entry(1)
	if !ok || e.Role != RoleAgent || e.Content != "world" {
		t.Error("second entry should be agent message 'world'")
	}
}

func TestHistoryStore_AddSetsTimestamp(t *testing.T) {
	s := NewHistoryStore()
	before := time.Now()
	s.Add(HistoryEntry{Role: RoleUser, Content: "test"})
	after := time.Now()

	e, _ := s.Entry(0)
	if e.TS.Before(before) || e.TS.After(after) {
		t.Error("timestamp should be set to current time")
	}
}

func TestHistoryStore_AddPreservesScroll(t *testing.T) {
	s := NewHistoryStore()
	s.scrollOffset = 10
	s.Add(HistoryEntry{Role: RoleUser, Content: "reset scroll"})
	if s.ScrollOffset() != 10 {
		t.Error("Add should preserve scroll position when the user is reading older content")
	}
}

func TestHistoryStore_Render(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "msg1"})
	s.Add(HistoryEntry{Role: RoleAgent, Content: "msg2"})

	lines := s.Render(80, 100)
	if len(lines) == 0 {
		t.Error("Render should return lines")
	}

	// Should contain both messages
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "msg1") {
		t.Error("rendered output should contain msg1")
	}
	if !strings.Contains(joined, "msg2") {
		t.Error("rendered output should contain msg2")
	}
}

func TestHistoryStore_RenderEmpty(t *testing.T) {
	s := NewHistoryStore()
	lines := s.Render(80, 100)
	if lines != nil {
		t.Error("empty store should return nil")
	}
}

func TestHistoryStore_RenderZeroDimensions(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "test"})

	if s.Render(0, 100) != nil {
		t.Error("zero width should return nil")
	}
	if s.Render(80, 0) != nil {
		t.Error("zero maxLines should return nil")
	}
}

func TestHistoryStore_ScrollUp(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 20; i++ {
		s.Add(HistoryEntry{Role: RoleUser, Content: "entry"})
	}
	s.Render(80, 5)

	s.ScrollUp(5)
	if s.ScrollOffset() != 5 {
		t.Errorf("expected scroll offset 5, got %d", s.ScrollOffset())
	}

	s.ScrollUp(100)
	// Should clamp to max
	if s.ScrollOffset() < 5 {
		t.Error("scroll up should not go negative after clamping")
	}
}

func TestHistoryStore_ClampsUsingRenderedLineCount(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 10; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "line"})
	}

	s.Render(80, 4)
	s.ScrollUp(100)

	if got, want := s.ScrollOffset(), 6; got != want {
		t.Fatalf("scroll offset = %d, want actual rendered maximum %d", got, want)
	}
}

func TestHistoryStore_RenderWithTailKeepsViewportAnchored(t *testing.T) {
	s := NewHistoryStore()
	for i := 0; i < 8; i++ {
		s.Add(HistoryEntry{Role: RoleSystem, Content: "history"})
	}

	before := s.RenderWithTail(80, 4, []string{"tail-1"})
	s.ScrollUp(2)
	before = s.RenderWithTail(80, 4, []string{"tail-1"})
	after := s.RenderWithTail(80, 4, []string{"tail-1", "tail-2"})

	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("viewport moved when transient content arrived\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestHistoryStore_ScrollDown(t *testing.T) {
	s := NewHistoryStore()
	s.scrollOffset = 10

	s.ScrollDown(3)
	if s.ScrollOffset() != 7 {
		t.Errorf("expected scroll offset 7, got %d", s.ScrollOffset())
	}

	s.ScrollDown(100)
	if s.ScrollOffset() != 0 {
		t.Error("scroll down should clamp to 0")
	}
}

func TestHistoryStore_ResetScroll(t *testing.T) {
	s := NewHistoryStore()
	s.scrollOffset = 50
	s.ResetScroll()
	if s.ScrollOffset() != 0 {
		t.Error("ResetScroll should set offset to 0")
	}
}

func TestHistoryStore_Search(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "find this message"})
	s.Add(HistoryEntry{Role: RoleAgent, Content: "another response"})
	s.Add(HistoryEntry{Role: RoleUser, Content: "FIND case insensitive"})

	results := s.Search("find")
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'find', got %d", len(results))
	}

	results = s.Search("nonexistent")
	if len(results) != 0 {
		t.Error("nonexistent query should return empty")
	}

	results = s.Search("")
	if results != nil {
		t.Error("empty query should return nil")
	}
}

func TestHistoryStore_InvalidateCache(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "cached"})

	// Render to populate cache
	s.Render(80, 100)
	if s.cachedWidth != 80 {
		t.Error("cachedWidth should be 80 after render")
	}

	// Invalidate
	s.InvalidateCache()
	if len(s.renderedCache) != 0 {
		t.Error("InvalidateCache should clear cache")
	}
	if s.cachedWidth != 0 {
		t.Error("InvalidateCache should reset cachedWidth")
	}
}

func TestHistoryStore_EntryOutOfBounds(t *testing.T) {
	s := NewHistoryStore()
	s.Add(HistoryEntry{Role: RoleUser, Content: "only one"})

	_, ok := s.Entry(-1)
	if ok {
		t.Error("negative index should return false")
	}

	_, ok = s.Entry(5)
	if ok {
		t.Error("out of range index should return false")
	}
}

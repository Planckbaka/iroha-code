package tui

import (
	"bytes"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestRawRendererResetTable
// ---------------------------------------------------------------------------

func TestRawRendererResetTable(t *testing.T) {
	tests := []struct {
		name        string
		preDraw     []string
	}{
		{"reset after draw", []string{"hello", "world"}},
		{"reset with no prior draw", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := NewRawRenderer(&buf)

			if tt.preDraw != nil {
				r.Draw(tt.preDraw, -1, 0)
			}

			r.Reset()

			if r.oldLines != nil {
				t.Error("oldLines should be nil after reset")
			}
			if r.cursorUpLines != 0 {
				t.Error("cursorUpLines should be 0 after reset")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRawRendererDrawTable
// ---------------------------------------------------------------------------

func TestRawRendererDrawTable(t *testing.T) {
	// Single-draw tests: verify first-draw output
	t.Run("first draw renders all lines", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewRawRenderer(&buf)
		r.Draw([]string{"line1", "line2", "line3"}, -1, 0)
		if !strings.Contains(buf.String(), "line1") {
			t.Errorf("first draw should contain 'line1', got:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "line3") {
			t.Errorf("first draw should contain 'line3', got:\n%s", buf.String())
		}
	})

	t.Run("first draw with cursor positioning", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewRawRenderer(&buf)
		r.Draw([]string{"hello", "world"}, 0, 3)
		out := buf.String()
		if !strings.Contains(out, "hello") {
			t.Errorf("should contain 'hello', got:\n%s", out)
		}
		// Cursor positioning emits escape sequences (move up + move right)
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("expected cursor positioning escape sequences, got:\n%s", out)
		}
	})

	t.Run("cursor at last line", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewRawRenderer(&buf)
		r.Draw([]string{"top", "middle", "bottom"}, 2, 1)
		// cursorRow=2 (last line) means up=0, so no cursor-up escape
		out := buf.String()
		if !strings.Contains(out, "bottom") {
			t.Errorf("should contain 'bottom', got:\n%s", out)
		}
	})

	// Differential redraw tests: verify second-draw output
	t.Run("differential redraw with appended lines", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewRawRenderer(&buf)
		r.Draw([]string{"alpha", "beta"}, -1, 0)
		buf.Reset()
		r.Draw([]string{"alpha", "beta", "gamma"}, -1, 0)
		out := buf.String()
		if !strings.Contains(out, "gamma") {
			t.Errorf("second draw should contain 'gamma', got:\n%s", out)
		}
		if strings.Contains(out, "alpha") {
			t.Errorf("differential redraw should NOT re-render identical lines like 'alpha', got:\n%s", out)
		}
	})

	t.Run("differential redraw with changed line", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewRawRenderer(&buf)
		r.Draw([]string{"aaa", "bbb"}, -1, 0)
		buf.Reset()
		r.Draw([]string{"aaa", "ccc"}, -1, 0)
		out := buf.String()
		if !strings.Contains(out, "ccc") {
			t.Errorf("second draw should contain 'ccc', got:\n%s", out)
		}
		if strings.Contains(out, "aaa") {
			t.Errorf("differential redraw should NOT re-render identical line 'aaa', got:\n%s", out)
		}
	})

	t.Run("differential redraw with fewer lines", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewRawRenderer(&buf)
		r.Draw([]string{"one", "two", "three"}, -1, 0)
		buf.Reset()
		r.Draw([]string{"one"}, -1, 0)
		out := buf.String()
		// Fewer lines means extra lines are cleared, then cursor moves back up
		// The diff starts at index 1 since "one" matches oldLines[0]
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("expected escape sequences for clearing extra lines, got:\n%s", out)
		}
	})

	t.Run("identical content produces no redraw", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewRawRenderer(&buf)
		r.Draw([]string{"same"}, -1, 0)
		buf.Reset()
		r.Draw([]string{"same"}, -1, 0)
		out := buf.String()
		// Only sync output delimiters, no actual line content
		if strings.Contains(out, "same") {
			t.Errorf("identical content should not be re-rendered, got:\n%s", out)
		}
	})
}

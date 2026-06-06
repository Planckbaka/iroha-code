package agent

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// WebFetchHandler: URL validation and error paths (no server needed)
// These tests exercise WebFetchHandler's input validation without making real
// HTTP requests. The SSRF check blocks 127.0.0.1 (httptest server), so
// server-based integration tests live in tools_web_ext_test.go using
// searxngSearch/parseDDGResults directly.
// ---------------------------------------------------------------------------

func TestWebFetchHandler_InvalidURLParse(t *testing.T) {
	resetFetchRateLimiter()

	_, err := WebFetchHandler(newMockToolCtx(), WebFetchArgs{URL: "://missing-scheme"})
	if err == nil {
		t.Fatal("expected error for unparseable URL")
	}
}

func TestWebFetchHandler_EmptyURL(t *testing.T) {
	resetFetchRateLimiter()

	_, err := WebFetchHandler(newMockToolCtx(), WebFetchArgs{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// ---------------------------------------------------------------------------
// WebSearchHandler rate limit
// ---------------------------------------------------------------------------

func TestWebSearchHandler_RateLimitBlocks(t *testing.T) {
	webSearchRateLimiter = newRateLimiter(1, time.Minute)
	t.Cleanup(resetSearchRateLimiter)

	// Exhaust the quota
	webSearchRateLimiter.Allow()

	// Now WebSearchHandler should be rate-limited
	_, err := WebSearchHandler(newMockToolCtx(), WebSearchArgs{Query: "test"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should mention rate limit, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// searxngSearch additional tests
// ---------------------------------------------------------------------------

func TestSearXNGSearch_ServerError(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer ts.Close()

		_, err := searxngSearch(ts.URL, "test", 5)
		if err == nil {
			t.Fatal("expected error for 502 response")
		}
		if !strings.Contains(err.Error(), "HTTP 502") {
			t.Errorf("error should mention HTTP 502, got: %v", err)
		}
	})
}

func TestSearXNGSearch_QueryEncoding(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		var receivedQuery string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedQuery = r.URL.Query().Get("q")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"results":[]}`))
		}))
		defer ts.Close()

		_, err := searxngSearch(ts.URL, "golang testing & more", 5)
		if err != nil {
			t.Fatalf("searxngSearch failed: %v", err)
		}
		if receivedQuery != "golang testing & more" {
			t.Errorf("query = %q, want 'golang testing & more'", receivedQuery)
		}
	})
}

func TestSearXNGSearch_SingleResult(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"results":[{"title":"Only Result","url":"https://example.com","content":"Only snippet"}]}`))
		}))
		defer ts.Close()

		result, err := searxngSearch(ts.URL, "test", 5)
		if err != nil {
			t.Fatalf("searxngSearch failed: %v", err)
		}
		if len(result.Results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result.Results))
		}
		if result.Results[0].Title != "Only Result" {
			t.Errorf("Title = %q, want 'Only Result'", result.Results[0].Title)
		}
		if result.Results[0].Snippet != "Only snippet" {
			t.Errorf("Snippet = %q, want 'Only snippet'", result.Results[0].Snippet)
		}
	})
}

// ---------------------------------------------------------------------------
// htmlToText additional coverage
// ---------------------------------------------------------------------------

func TestHTMLToText_Links(t *testing.T) {
	input := `<html><body><a href="https://example.com">Click here</a></body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "Click here") {
		t.Errorf("htmlToText should preserve link text, got: %q", got)
	}
}

func TestHTMLToText_Lists(t *testing.T) {
	input := `<html><body><ul><li>Item 1</li><li>Item 2</li></ul></body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "Item 1") || !strings.Contains(got, "Item 2") {
		t.Errorf("htmlToText should preserve list items, got: %q", got)
	}
}

func TestHTMLToText_ScriptAndStyle(t *testing.T) {
	input := `<html><head><script>alert('xss')</script><style>body{}</style></head><body>Visible</body></html>`
	got := htmlToText(strings.NewReader(input))
	if strings.Contains(got, "alert") {
		t.Errorf("htmlToText should strip script content, got: %q", got)
	}
	if !strings.Contains(got, "Visible") {
		t.Errorf("htmlToText should preserve body text, got: %q", got)
	}
}

func TestHTMLToText_ComplexStructure(t *testing.T) {
	input := `<html><body>
	<h1>Title</h1>
	<p>Paragraph with <strong>bold</strong> and <em>italic</em>.</p>
	<table><tr><td>A</td><td>B</td></tr></table>
	</body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "Title") {
		t.Errorf("should contain heading, got: %q", got)
	}
	if !strings.Contains(got, "bold") {
		t.Errorf("should contain inline text, got: %q", got)
	}
}

func TestHTMLToText_NestedElements(t *testing.T) {
	input := `<html><body><div><div><p>Deep</p></div></div></body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "Deep") {
		t.Errorf("should contain nested text, got: %q", got)
	}
}

func TestHTMLToText_BrTags(t *testing.T) {
	input := `<html><body>Line 1<br>Line 2<br>Line 3</body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "Line 1") || !strings.Contains(got, "Line 2") {
		t.Errorf("should preserve text with br tags, got: %q", got)
	}
}

func TestHTMLToText_Noscript(t *testing.T) {
	input := `<html><body><noscript>Hidden</noscript><p>Visible</p></body></html>`
	got := htmlToText(strings.NewReader(input))
	if strings.Contains(got, "Hidden") {
		t.Errorf("should strip noscript content, got: %q", got)
	}
	if !strings.Contains(got, "Visible") {
		t.Errorf("should preserve visible text, got: %q", got)
	}
}

func TestHTMLToText_IFrame(t *testing.T) {
	input := `<html><body><iframe src="evil.html">Frame content</iframe><p>Safe</p></body></html>`
	got := htmlToText(strings.NewReader(input))
	if strings.Contains(got, "Frame content") {
		t.Errorf("should strip iframe content, got: %q", got)
	}
	if !strings.Contains(got, "Safe") {
		t.Errorf("should preserve safe text, got: %q", got)
	}
}

func TestHTMLToText_SVG(t *testing.T) {
	input := `<html><body><svg><circle r="10"/></svg><p>After SVG</p></body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "After SVG") {
		t.Errorf("should preserve text after svg, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// rateLimiter
// ---------------------------------------------------------------------------

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	rl := newRateLimiter(3, time.Minute)

	if !rl.Allow() {
		t.Error("first request should be allowed")
	}
	if !rl.Allow() {
		t.Error("second request should be allowed")
	}
	if !rl.Allow() {
		t.Error("third request should be allowed")
	}
	if rl.Allow() {
		t.Error("fourth request should be rate limited")
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	rl := newRateLimiter(1, 50*time.Millisecond)

	if !rl.Allow() {
		t.Error("first request should be allowed")
	}
	if rl.Allow() {
		t.Error("second request should be rate limited (same window)")
	}

	// Wait for window to expire
	time.Sleep(80 * time.Millisecond)

	if !rl.Allow() {
		t.Error("request after window expiry should be allowed")
	}
}

// ---------------------------------------------------------------------------
// isPrivateIP IPv6 mapped
// ---------------------------------------------------------------------------

func TestIsPrivateIP_IPv4Mapped(t *testing.T) {
	// Test that IPv4-mapped IPv6 addresses are detected
	tests := []struct {
		ip   string
		want bool
	}{
		{"::ffff:10.0.0.1", true},
		{"::ffff:8.8.8.8", false},
		{"fe80::1", true},
		{"fc00::1", true},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.want {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

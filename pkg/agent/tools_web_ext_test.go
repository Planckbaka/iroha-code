package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// helper: override ssrfSafeClient to bypass SSRF checks for test servers
func withTestClient(t *testing.T, fn func()) {
	t.Helper()
	original := ssrfSafeClient
	ssrfSafeClient = &http.Client{Timeout: 10 * time.Second}
	t.Cleanup(func() { ssrfSafeClient = original })
	fn()
}

// helper: reset rate limiters
func resetFetchRateLimiter() {
	webFetchRateLimiter = newRateLimiter(100, time.Minute)
}

func resetSearchRateLimiter() {
	webSearchRateLimiter = newRateLimiter(100, time.Minute)
}

// helper: create a mock tool context
func newMockToolCtx() *mockToolContext {
	return &mockToolContext{Context: context.Background()}
}

// ---------------------------------------------------------------------------
// WebFetchHandler: URL validation and SSRF checks (no server needed)
// ---------------------------------------------------------------------------

func TestWebFetchHandler_InvalidURL(t *testing.T) {
	resetFetchRateLimiter()

	_, err := WebFetchHandler(newMockToolCtx(), WebFetchArgs{URL: "ftp://bad.scheme/file"})
	if err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

func TestWebFetchHandler_PrivateIPBlocked(t *testing.T) {
	resetFetchRateLimiter()

	_, err := WebFetchHandler(newMockToolCtx(), WebFetchArgs{URL: "http://127.0.0.1/test"})
	if err == nil {
		t.Fatal("expected SSRF block for private IP")
	}
}

func TestWebFetchHandler_SchemeValidation(t *testing.T) {
	resetFetchRateLimiter()

	tests := []struct {
		name string
		url  string
	}{
		{"ftp scheme", "ftp://example.com/file"},
		{"file scheme", "file:///etc/passwd"},
		{"javascript scheme", "javascript:alert(1)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := WebFetchHandler(newMockToolCtx(), WebFetchArgs{URL: tt.url})
			if err == nil {
				t.Error("expected error for disallowed scheme")
			}
		})
	}
}

func TestWebFetchHandler_RateLimit(t *testing.T) {
	webFetchRateLimiter = newRateLimiter(1, time.Minute)
	t.Cleanup(resetFetchRateLimiter)

	// Use up the quota
	webFetchRateLimiter.Allow()

	// Should be rate limited (URL doesn't matter, check happens before fetch)
	_, err := WebFetchHandler(newMockToolCtx(), WebFetchArgs{URL: "https://example.com/page"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should mention rate limit, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseDDGResults with crafted HTML
// ---------------------------------------------------------------------------

func TestParseDDGResults(t *testing.T) {
	ddgHTML := `<html><body>
<div class="result results_links results_links_deep web-result">
  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage1&rut=abc">Example Page 1</a>
  <a class="result__snippet">This is snippet one</a>
</div>
<div class="result results_links results_links_deep web-result">
  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage2&rut=def">Example Page 2</a>
  <a class="result__snippet">This is snippet two</a>
</div>
</body></html>`

	doc, err := html.Parse(strings.NewReader(ddgHTML))
	if err != nil {
		t.Fatalf("failed to parse HTML: %v", err)
	}

	var results []SearchResult
	parseDDGResults(doc, &results, 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Title != "Example Page 1" {
		t.Errorf("results[0].Title = %q, want 'Example Page 1'", results[0].Title)
	}
	if results[0].URL != "https://example.com/page1" {
		t.Errorf("results[0].URL = %q, want 'https://example.com/page1'", results[0].URL)
	}
	if results[0].Snippet != "This is snippet one" {
		t.Errorf("results[0].Snippet = %q, want 'This is snippet one'", results[0].Snippet)
	}

	if results[1].Title != "Example Page 2" {
		t.Errorf("results[1].Title = %q, want 'Example Page 2'", results[1].Title)
	}
}

func TestParseDDGResults_MaxCount(t *testing.T) {
	ddgHTML := `<html><body>
<div class="result results_links results_links_deep web-result">
  <a class="result__a" href="https://example.com/1">Result 1</a>
  <a class="result__snippet">Snippet 1</a>
</div>
<div class="result results_links results_links_deep web-result">
  <a class="result__a" href="https://example.com/2">Result 2</a>
  <a class="result__snippet">Snippet 2</a>
</div>
<div class="result results_links results_links_deep web-result">
  <a class="result__a" href="https://example.com/3">Result 3</a>
  <a class="result__snippet">Snippet 3</a>
</div>
</body></html>`

	doc, err := html.Parse(strings.NewReader(ddgHTML))
	if err != nil {
		t.Fatalf("failed to parse HTML: %v", err)
	}

	var results []SearchResult
	parseDDGResults(doc, &results, 2)

	if len(results) != 2 {
		t.Errorf("expected max 2 results, got %d", len(results))
	}
}

func TestParseDDGResults_EmptyHTML(t *testing.T) {
	doc, err := html.Parse(strings.NewReader("<html><body></body></html>"))
	if err != nil {
		t.Fatal(err)
	}

	var results []SearchResult
	parseDDGResults(doc, &results, 10)

	if len(results) != 0 {
		t.Errorf("expected 0 results from empty HTML, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// extractDDGResult
// ---------------------------------------------------------------------------

func TestExtractDDGResult_DirectHref(t *testing.T) {
	nodeHTML := `<div><a class="result__a" href="https://direct.link/page">Direct Link</a><a class="result__snippet">Snippet text</a></div>`
	doc, err := html.Parse(strings.NewReader(nodeHTML))
	if err != nil {
		t.Fatal(err)
	}

	var sr SearchResult
	extractDDGResult(doc, &sr)

	if sr.Title != "Direct Link" {
		t.Errorf("Title = %q, want 'Direct Link'", sr.Title)
	}
	if sr.URL != "https://direct.link/page" {
		t.Errorf("URL = %q, want 'https://direct.link/page'", sr.URL)
	}
	if sr.Snippet != "Snippet text" {
		t.Errorf("Snippet = %q, want 'Snippet text'", sr.Snippet)
	}
}

func TestExtractDDGResult_NoTitleNoURL(t *testing.T) {
	htmlStr := `<html><body><div class="other"><p>Nothing useful</p></div></body></html>`
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatal(err)
	}

	var sr SearchResult
	extractDDGResult(doc, &sr)

	if sr.Title != "" {
		t.Errorf("Title should be empty, got %q", sr.Title)
	}
}

func TestExtractDDGResult_UDDGExtraction(t *testing.T) {
	// Test the uddg= parameter extraction from DDG redirect URLs
	htmlStr := `<div><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2Fnet%2Fhttp&rut=xyz">Go HTTP Package</a><a class="result__snippet">Official docs</a></div>`
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatal(err)
	}

	var sr SearchResult
	extractDDGResult(doc, &sr)

	if sr.Title != "Go HTTP Package" {
		t.Errorf("Title = %q, want 'Go HTTP Package'", sr.Title)
	}
	if sr.URL != "https://pkg.go.dev/net/http" {
		t.Errorf("URL = %q, want 'https://pkg.go.dev/net/http'", sr.URL)
	}
}

// ---------------------------------------------------------------------------
// searxngSearch against mock server (uses ssrfSafeClient, no checkSSRF call)
// ---------------------------------------------------------------------------

func TestSearXNGSearch_MockServer(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("q") != "golang testing" {
				t.Errorf("expected q='golang testing', got %q", r.URL.Query().Get("q"))
			}
			if r.URL.Query().Get("format") != "json" {
				t.Errorf("expected format=json, got %q", r.URL.Query().Get("format"))
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"title": "Go Testing Guide", "url": "https://go.dev/testing", "content": "Learn Go testing"},
					{"title": "Advanced Go Tests", "url": "https://example.com/advanced", "content": "Advanced techniques"},
				},
			})
		}))
		defer ts.Close()

		result, err := searxngSearch(ts.URL, "golang testing", 5)
		if err != nil {
			t.Fatalf("searxngSearch failed: %v", err)
		}
		if len(result.Results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result.Results))
		}
		if result.Results[0].Title != "Go Testing Guide" {
			t.Errorf("results[0].Title = %q, want 'Go Testing Guide'", result.Results[0].Title)
		}
		if result.Results[0].URL != "https://go.dev/testing" {
			t.Errorf("results[0].URL = %q, want 'https://go.dev/testing'", result.Results[0].URL)
		}
		if result.Results[0].Snippet != "Learn Go testing" {
			t.Errorf("results[0].Snippet = %q, want 'Learn Go testing'", result.Results[0].Snippet)
		}
	})
}

func TestSearXNGSearch_CountLimit(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			results := make([]map[string]any, 10)
			for i := 0; i < 10; i++ {
				results[i] = map[string]any{
					"title":   fmt.Sprintf("Result %d", i),
					"url":     fmt.Sprintf("https://example.com/%d", i),
					"content": fmt.Sprintf("Content %d", i),
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"results": results})
		}))
		defer ts.Close()

		result, err := searxngSearch(ts.URL, "test", 3)
		if err != nil {
			t.Fatalf("searxngSearch failed: %v", err)
		}
		if len(result.Results) != 3 {
			t.Errorf("expected 3 results (count limit), got %d", len(result.Results))
		}
	})
}

func TestSearXNGSearch_BadStatusCode(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		_, err := searxngSearch(ts.URL, "test", 5)
		if err == nil {
			t.Fatal("expected error for 500 response")
		}
	})
}

func TestSearXNGSearch_InvalidJSON(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not valid json"))
		}))
		defer ts.Close()

		_, err := searxngSearch(ts.URL, "test", 5)
		if err == nil {
			t.Fatal("expected error for invalid JSON response")
		}
	})
}

func TestSearXNGSearch_EmptyResults(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		}))
		defer ts.Close()

		result, err := searxngSearch(ts.URL, "obscure query", 5)
		if err != nil {
			t.Fatalf("searxngSearch failed: %v", err)
		}
		if len(result.Results) != 0 {
			t.Errorf("expected 0 results, got %d", len(result.Results))
		}
	})
}

func TestSearXNGSearch_TrailingSlash(t *testing.T) {
	resetSearchRateLimiter()
	withTestClient(t, func() {
		var requestedPath string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		}))
		defer ts.Close()

		_, err := searxngSearch(ts.URL+"/", "test", 5)
		if err != nil {
			t.Fatalf("searxngSearch failed: %v", err)
		}
		// Should strip trailing slash, so path should be /search not //search
		if requestedPath != "/search" {
			t.Errorf("requested path = %q, want '/search'", requestedPath)
		}
	})
}

// ---------------------------------------------------------------------------
// getAttr helper
// ---------------------------------------------------------------------------

func TestGetAttr(t *testing.T) {
	node := &html.Node{
		Type: html.ElementNode,
		Data: "div",
		Attr: []html.Attribute{
			{Key: "class", Val: "result"},
			{Key: "id", Val: "main"},
		},
	}

	if got := getAttr(node, "class"); got != "result" {
		t.Errorf("getAttr(class) = %q, want 'result'", got)
	}
	if got := getAttr(node, "id"); got != "main" {
		t.Errorf("getAttr(id) = %q, want 'main'", got)
	}
	if got := getAttr(node, "nonexistent"); got != "" {
		t.Errorf("getAttr(nonexistent) = %q, want empty", got)
	}
}

func TestGetAttr_NoAttributes(t *testing.T) {
	node := &html.Node{
		Type: html.ElementNode,
		Data: "br",
	}
	if got := getAttr(node, "class"); got != "" {
		t.Errorf("expected empty for node with no attributes, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// textContent helper
// ---------------------------------------------------------------------------

func TestTextContent(t *testing.T) {
	input := `<a href="/link">Hello <b>World</b></a>`
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	var findAnchor func(*html.Node) *html.Node
	findAnchor = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "a" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := findAnchor(c); found != nil {
				return found
			}
		}
		return nil
	}

	anchor := findAnchor(doc)
	if anchor == nil {
		t.Fatal("failed to find <a> node")
	}

	text := textContent(anchor)
	if text != "Hello World" {
		t.Errorf("textContent = %q, want 'Hello World'", text)
	}
}

func TestTextContent_TextNode(t *testing.T) {
	node := &html.Node{
		Type: html.TextNode,
		Data: "plain text",
	}
	got := textContent(node)
	if got != "plain text" {
		t.Errorf("textContent(TextNode) = %q, want 'plain text'", got)
	}
}

func TestTextContent_EmptyElement(t *testing.T) {
	node := &html.Node{
		Type: html.ElementNode,
		Data: "br",
	}
	got := textContent(node)
	if got != "" {
		t.Errorf("textContent(empty element) = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// SSRF checkSSRF
// ---------------------------------------------------------------------------

func TestCheckSSRF_EmptyHostname(t *testing.T) {
	u, _ := url.Parse("http:///path")
	err := checkSSRF(u)
	if err == nil {
		t.Fatal("expected error for empty hostname")
	}
}

func TestCheckSSRF_PublicIP(t *testing.T) {
	// 8.8.8.8 is a public Google DNS - this may fail in some environments
	// but should work in most CI/CD environments
	u, _ := url.Parse("http://8.8.8.8/test")
	err := checkSSRF(u)
	if err != nil {
		t.Logf("checkSSRF for 8.8.8.8 returned error (may be env-specific): %v", err)
	}
}

func TestCheckSSRF_PrivateIPs(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"loopback", "http://127.0.0.1/test"},
		{"10.x", "http://10.0.0.1/test"},
		{"172.16.x", "http://172.16.0.1/test"},
		{"192.168.x", "http://192.168.1.1/test"},
		{"link local", "http://169.254.169.254/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse(tt.url)
			err := checkSSRF(u)
			if err == nil {
				t.Error("expected SSRF block for private IP")
			}
			if !strings.Contains(err.Error(), "SSRF") {
				t.Errorf("error should mention SSRF, got: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WebSearchHandler rate limiting (direct limiter test)
// ---------------------------------------------------------------------------

func TestWebSearchHandler_RateLimit(t *testing.T) {
	webSearchRateLimiter = newRateLimiter(1, time.Minute)
	t.Cleanup(resetSearchRateLimiter)

	webSearchRateLimiter.Allow() // use up the quota

	if webSearchRateLimiter.Allow() {
		t.Error("expected rate limit to be hit")
	}
}

// ---------------------------------------------------------------------------
// htmlToText extended tests
// ---------------------------------------------------------------------------

func TestHTMLToText_TableElements(t *testing.T) {
	input := `<html><body><table><tr><td>Cell 1</td><td>Cell 2</td></tr></table></body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "Cell 1") || !strings.Contains(got, "Cell 2") {
		t.Errorf("htmlToText should contain table cells, got: %q", got)
	}
}

func TestHTMLToText_Headings(t *testing.T) {
	input := `<html><body><h1>Title</h1><h2>Subtitle</h2><p>Content</p></body></html>`
	got := htmlToText(strings.NewReader(input))
	if !strings.Contains(got, "Title") {
		t.Errorf("htmlToText missing 'Title', got: %q", got)
	}
	if !strings.Contains(got, "Subtitle") {
		t.Errorf("htmlToText missing 'Subtitle', got: %q", got)
	}
	if !strings.Contains(got, "Content") {
		t.Errorf("htmlToText missing 'Content', got: %q", got)
	}
}

func TestHTMLToText_EmptyInput(t *testing.T) {
	got := htmlToText(strings.NewReader(""))
	if got != "" {
		t.Errorf("expected empty string for empty input, got: %q", got)
	}
}

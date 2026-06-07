package agent

import (
	"context"
	"iter"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// resetCircuitBreaker resets the compaction circuit breaker state between tests.
func resetCircuitBreaker() {
	compactionCircuitBreaker.mu.Lock()
	compactionCircuitBreaker.failures = 0
	compactionCircuitBreaker.open = false
	compactionCircuitBreaker.lastFailure = time.Time{}
	compactionCircuitBreaker.mu.Unlock()
}

// --- CompactContents edge cases ---

func TestCompactContents_EmptyInput(t *testing.T) {
	resetCircuitBreaker()
	result := CompactContents(nil, "test-session")
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = CompactContents([]*genai.Content{}, "test-session")
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestCompactContents_SmallToolResponse_NotCompacted(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Tool response under 1000 chars should NOT be micro-compacted
	contents := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						Name: "file_read",
						Response: map[string]any{
							"output": "short response",
						},
					},
				},
			},
		},
	}

	compacted := CompactContents(contents, "session-small")
	if len(compacted) != 1 {
		t.Fatalf("expected 1 content, got %d", len(compacted))
	}

	respMap := compacted[0].Parts[0].FunctionResponse.Response
	outputVal := respMap["output"].(string)
	if outputVal != "short response" {
		t.Errorf("expected small response to be preserved, got: %s", outputVal)
	}
}

func TestCompactContents_StickyBlocksPreserved(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create 15 content items so summarization kicks in (>12)
	contents := make([]*genai.Content, 15)
	for i := 0; i < 15; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		text := "normal message"
		if i == 5 {
			text = "[STICKY] important context that must be preserved"
		}
		if i == 0 {
			text = "initial prompt"
		}
		contents[i] = &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: text}},
		}
	}

	compacted := CompactContents(contents, "session-sticky")

	// Check that sticky content was re-inserted
	found := false
	for _, c := range compacted {
		for _, p := range c.Parts {
			if p.Text != "" && strings.Contains(p.Text, "[STICKY]") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected sticky block to be preserved in compacted output")
	}
}

func TestCompactContents_CircuitBreakerTrips(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Use a panicking LLM — CompactContents has a recover() that catches panics
	// and sets compactionErr, which triggers the circuit breaker.
	panicLLM := &PanicLLM{}

	// Build 14 rounds to trigger summarization
	buildRounds := func() []*genai.Content {
		contents := make([]*genai.Content, 14)
		for i := 0; i < 14; i++ {
			role := "user"
			if i%2 == 1 {
				role = "model"
			}
			contents[i] = &genai.Content{
				Role:  role,
				Parts: []*genai.Part{{Text: "message"}},
			}
		}
		return contents
	}

	// First 3 failures should trip the circuit breaker
	for i := 0; i < 3; i++ {
		contents := buildRounds()
		compacted := CompactContents(contents, "session-cb-test", panicLLM)
		if len(compacted) == 0 {
			t.Errorf("iteration %d: expected non-empty compacted result", i)
		}
	}

	// Verify circuit breaker is now open
	compactionCircuitBreaker.mu.Lock()
	isOpen := compactionCircuitBreaker.open
	failures := compactionCircuitBreaker.failures
	compactionCircuitBreaker.mu.Unlock()

	if !isOpen {
		t.Errorf("expected circuit breaker to be open after 3 failures, failures=%d", failures)
	}
	if failures < 3 {
		t.Errorf("expected at least 3 failures, got %d", failures)
	}
}

func TestCompactContents_CircuitBreakerResets(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Manually set circuit breaker to open with old timestamp
	compactionCircuitBreaker.mu.Lock()
	compactionCircuitBreaker.open = true
	compactionCircuitBreaker.failures = 3
	compactionCircuitBreaker.lastFailure = time.Now().Add(-10 * time.Minute)
	compactionCircuitBreaker.mu.Unlock()

	// Build 14 rounds
	contents := make([]*genai.Content, 14)
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		contents[i] = &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: "message"}},
		}
	}

	// CompactContents should reset the circuit breaker since > 5 minutes have passed
	compacted := CompactContents(contents, "session-cb-reset")
	if len(compacted) == 0 {
		t.Error("expected non-empty compacted result")
	}

	// After reset, circuit breaker should be closed
	compactionCircuitBreaker.mu.Lock()
	isOpen := compactionCircuitBreaker.open
	compactionCircuitBreaker.mu.Unlock()

	if isOpen {
		t.Error("expected circuit breaker to be reset (closed) after timeout")
	}
}

func TestCompactContents_SuccessfulLLMResetsCircuitBreaker(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Pre-set some failures
	compactionCircuitBreaker.mu.Lock()
	compactionCircuitBreaker.failures = 2
	compactionCircuitBreaker.open = false
	compactionCircuitBreaker.mu.Unlock()

	mockLLM := &MockLLM{
		ResponseText: "LLM summary of the conversation.",
	}

	contents := make([]*genai.Content, 14)
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		contents[i] = &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: "message"}},
		}
	}

	compacted := CompactContents(contents, "session-success", mockLLM)
	if len(compacted) == 0 {
		t.Error("expected non-empty compacted result")
	}

	// After success, failures should be reset
	compactionCircuitBreaker.mu.Lock()
	failures := compactionCircuitBreaker.failures
	compactionCircuitBreaker.mu.Unlock()

	if failures != 0 {
		t.Errorf("expected failures to be reset to 0 after successful summarization, got %d", failures)
	}
}

func TestCompactContents_LLMWithSummary(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	mockLLM := &MockLLM{
		ResponseText: "This is the LLM-generated summary of the conversation.",
	}

	contents := make([]*genai.Content, 14)
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		contents[i] = &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: "message"}},
		}
	}

	compacted := CompactContents(contents, "session-llm", mockLLM)

	// Verify LLM summary is in the compacted output
	foundLLMSummary := false
	for _, c := range compacted {
		for _, p := range c.Parts {
			if p.Text != "" && strings.Contains(p.Text, "summarized by LLM") {
				foundLLMSummary = true
				break
			}
		}
	}
	if !foundLLMSummary {
		t.Error("expected LLM summary marker in compacted output")
	}
}

func TestCompactContents_EmptySessionID_Defaults(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	largeStr := strings.Repeat("X", 1100)
	contents := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						Name: "test_tool",
						Response: map[string]any{
							"output": largeStr,
						},
					},
				},
			},
		},
	}

	// Pass empty session ID — should default to "session-default"
	compacted := CompactContents(contents, "")
	if len(compacted) != 1 {
		t.Fatalf("expected 1 content, got %d", len(compacted))
	}

	// Verify archive was created with default session name
	archivePath := tempHome + "/.iroha/transcripts/session-default.jsonl"
	if _, err := os.Stat(archivePath); err != nil {
		t.Errorf("expected archive at %s: %v", archivePath, err)
	}
}

func TestCompactContents_DeepCopy_PreservesOriginal(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	largeStr := strings.Repeat("Y", 1200)
	contents := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						Name: "file_write",
						Args: map[string]any{"path": "/tmp/test.go"},
					},
				},
				{
					FunctionResponse: &genai.FunctionResponse{
						Name: "file_write",
						Response: map[string]any{
							"output": largeStr,
						},
					},
				},
			},
		},
	}

	compacted := CompactContents(contents, "session-deepcopy")

	// Original function call args should be untouched
	origArgs := contents[0].Parts[0].FunctionCall.Args
	if origArgs["path"] != "/tmp/test.go" {
		t.Errorf("original function call args modified: %v", origArgs)
	}

	// Original function response should be untouched
	origOutput := contents[0].Parts[1].FunctionResponse.Response["output"].(string)
	if origOutput != largeStr {
		t.Errorf("original function response was modified")
	}

	// Compacted version should have micro-compacted placeholder
	compactedOutput := compacted[0].Parts[1].FunctionResponse.Response["output"].(string)
	if !strings.Contains(compactedOutput, "Full output archived") {
		t.Errorf("expected micro-compacted placeholder, got: %s", compactedOutput)
	}
}

// --- extractStickyBlocks tests ---

func TestExtractStickyBlocks_Basic(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "normal text"}}},
		{Role: "model", Parts: []*genai.Part{{Text: "[STICKY] keep this"}}},
		{Role: "user", Parts: []*genai.Part{{Text: "more normal text"}}},
		{Role: "model", Parts: []*genai.Part{{Text: "[STICKY] and this too"}}},
	}

	blocks := extractStickyBlocks(contents)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 sticky blocks, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Parts[0].Text, "[STICKY]") {
		t.Errorf("first block should contain [STICKY], got: %s", blocks[0].Parts[0].Text)
	}
}

func TestExtractStickyBlocks_None(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "no sticky here"}}},
		{Role: "model", Parts: []*genai.Part{{Text: "just regular text"}}},
	}

	blocks := extractStickyBlocks(contents)
	if len(blocks) != 0 {
		t.Errorf("expected 0 sticky blocks, got %d", len(blocks))
	}
}

func TestExtractStickyBlocks_Empty(t *testing.T) {
	blocks := extractStickyBlocks(nil)
	if blocks != nil {
		t.Errorf("expected nil for nil input, got %v", blocks)
	}

	blocks = extractStickyBlocks([]*genai.Content{})
	if len(blocks) != 0 {
		t.Errorf("expected empty for empty input, got %v", blocks)
	}
}

func TestExtractStickyBlocks_OneBlockMultipleParts(t *testing.T) {
	// One content block with multiple parts, only one has [STICKY]
	contents := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{Text: "normal part"},
				{Text: "[STICKY] important"},
			},
		},
	}

	blocks := extractStickyBlocks(contents)
	if len(blocks) != 1 {
		t.Errorf("expected 1 sticky block, got %d", len(blocks))
	}
}

// --- Concurrent circuit breaker test ---

func TestCompactContents_ConcurrentSafety(t *testing.T) {
	resetCircuitBreaker()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	mockLLM := &MockLLM{
		ResponseText: "concurrent summary",
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			contents := make([]*genai.Content, 14)
			for j := 0; j < 14; j++ {
				role := "user"
				if j%2 == 1 {
					role = "model"
				}
				contents[j] = &genai.Content{
					Role:  role,
					Parts: []*genai.Part{{Text: "concurrent message"}},
				}
			}
			compacted := CompactContents(contents, "concurrent-session", mockLLM)
			if len(compacted) == 0 {
				t.Errorf("goroutine %d: got empty result", id)
			}
		}(i)
	}
	wg.Wait()
}

// --- Helper types ---

// PanicLLM is a mock LLM that panics during GenerateContent, triggering
// the circuit breaker's panic recovery path in CompactContents.
type PanicLLM struct{}

func (m *PanicLLM) Name() string { return "panic-llm" }
func (m *PanicLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		panic("intentional panic for circuit breaker test")
	}
}

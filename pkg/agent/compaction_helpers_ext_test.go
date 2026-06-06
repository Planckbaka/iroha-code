package agent

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/genai"
)

// --- truncateOnlySummary tests ---

func TestTruncateOnlySummary_EmptyRounds(t *testing.T) {
	result := truncateOnlySummary(nil)
	if !strings.Contains(result, "No previous conversation history") {
		t.Errorf("expected empty-history message, got: %s", result)
	}

	result = truncateOnlySummary([]*genai.Content{})
	if !strings.Contains(result, "No previous conversation history") {
		t.Errorf("expected empty-history message for empty slice, got: %s", result)
	}
}

func TestTruncateOnlySummary_TextOnly(t *testing.T) {
	rounds := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Hello there"}},
		},
		{
			Role:  "model",
			Parts: []*genai.Part{{Text: "I can help you."}},
		},
	}
	result := truncateOnlySummary(rounds)

	if !strings.Contains(result, "user: Hello there") {
		t.Errorf("expected 'user: Hello there' in output, got: %s", result)
	}
	if !strings.Contains(result, "assistant: I can help you.") {
		t.Errorf("model role should map to 'assistant', got: %s", result)
	}
	if !strings.Contains(result, "truncation-only mode") {
		t.Errorf("expected truncation-only mode marker, got: %s", result)
	}
}

func TestTruncateOnlySummary_EmptyRoleDefaultsToAssistant(t *testing.T) {
	rounds := []*genai.Content{
		{
			Role:  "",
			Parts: []*genai.Part{{Text: "empty role text"}},
		},
	}
	result := truncateOnlySummary(rounds)

	if !strings.Contains(result, "assistant: empty role text") {
		t.Errorf("empty role should map to assistant, got: %s", result)
	}
}

func TestTruncateOnlySummary_FunctionCallAndResponse(t *testing.T) {
	rounds := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "file_read"}},
			},
		},
		{
			Role: "user",
			Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{Name: "file_read"}},
			},
		},
	}
	result := truncateOnlySummary(rounds)

	if !strings.Contains(result, "[Called tool file_read]") {
		t.Errorf("expected function call in output, got: %s", result)
	}
	if !strings.Contains(result, "tool file_read: [responded]") {
		t.Errorf("expected function response in output, got: %s", result)
	}
}

func TestTruncateOnlySummary_Truncation(t *testing.T) {
	// Create a round with very long text to trigger the 4000-char truncation
	longText := strings.Repeat("a", 5000)
	rounds := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: longText}},
		},
	}
	result := truncateOnlySummary(rounds)

	if !strings.Contains(result, "...[truncated]") {
		t.Errorf("expected truncation marker for long transcript, got len=%d", len(result))
	}
}

func TestTruncateOnlySummary_WithStructuredSummary(t *testing.T) {
	rounds := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "file_write"}},
				{Text: "I will update main.go with the new logic"},
			},
		},
	}
	result := truncateOnlySummary(rounds)

	// Should contain [SUMMARY] block from extractStructuredSummary
	if !strings.Contains(result, "[SUMMARY]") {
		t.Errorf("expected [SUMMARY] block, got: %s", result)
	}
	if !strings.Contains(result, "[/SUMMARY]") {
		t.Errorf("expected [/SUMMARY] block, got: %s", result)
	}
}

// --- capStickyContent tests ---

func TestCapStickyContent_Empty(t *testing.T) {
	result := capStickyContent(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got: %v", result)
	}

	result = capStickyContent([]*genai.Content{})
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got: %v", result)
	}
}

func TestCapStickyContent_SmallBlocksUnchanged(t *testing.T) {
	// Small blocks should pass through unchanged
	blocks := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: "small content"}},
		},
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: "another small block"}},
		},
	}
	result := capStickyContent(blocks)
	if len(result) != 2 {
		t.Errorf("expected 2 blocks (under cap), got %d", len(result))
	}
}

func TestCapStickyContent_TrimsOldest(t *testing.T) {
	// Create blocks that exceed the maxStickyFraction of estimatedContextWindowBytes
	// maxBytes = 200000 * 0.20 = 40000
	// Need blocks where removing the oldest still leaves totalBytes > maxBytes,
	// so the algorithm is forced to drop it.
	// With 3 blocks of 25000 each = 75000 total:
	//   i=2 (newest, 25000): 75000-25000=50000 >= 40000 => drop, totalBytes=50000
	//   i=1 (mid, 25000): 50000-25000=25000 < 40000 => KEEP
	//   i=0 (oldest, 25000): 50000-25000=25000 < 40000 => KEEP
	// Result: 2 blocks kept (oldest and mid)
	oldestBlock := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: strings.Repeat("a", 25000)}},
	}
	midBlock := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: strings.Repeat("b", 25000)}},
	}
	newestBlock := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: strings.Repeat("c", 25000)}},
	}

	blocks := []*genai.Content{oldestBlock, midBlock, newestBlock}
	result := capStickyContent(blocks)

	if len(result) != 2 {
		t.Fatalf("expected 2 blocks after capping, got %d", len(result))
	}
	// The newest block should have been dropped because removing it
	// keeps totalBytes above maxBytes
	if result[0] != oldestBlock {
		t.Error("expected oldest block to be kept")
	}
	if result[1] != midBlock {
		t.Error("expected mid block to be kept")
	}
}

func TestCapStickyContent_AllBlocksFit(t *testing.T) {
	// All blocks under the limit
	blocks := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "block1"}}},
		{Role: "user", Parts: []*genai.Part{{Text: "block2"}}},
		{Role: "user", Parts: []*genai.Part{{Text: "block3"}}},
	}
	result := capStickyContent(blocks)
	if len(result) != 3 {
		t.Errorf("expected all 3 blocks to fit, got %d", len(result))
	}
}

func TestCapStickyContent_MultiplePartsPerBlock(t *testing.T) {
	// Block with multiple text parts
	blocks := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: strings.Repeat("x", 20000)},
				{Text: strings.Repeat("y", 20000)},
			},
		},
	}
	result := capStickyContent(blocks)
	// This single block is 40000 bytes, which is exactly maxBytes, so it should be kept
	if len(result) != 1 {
		t.Errorf("expected block at exactly maxBytes to be kept, got %d", len(result))
	}
}

// --- summarizeRounds with LLM tests ---

func TestSummarizeRounds_WithNilLLM(t *testing.T) {
	rounds := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Hello"}},
		},
	}
	result := summarizeRounds(rounds, nil)
	if !strings.Contains(result, "compacted") {
		t.Errorf("expected compaction message with nil LLM, got: %s", result)
	}
}

func TestSummarizeRounds_WithLLMError(t *testing.T) {
	mock := &MockLLM{
		ResponseErr: fmt.Errorf("LLM unavailable"),
	}

	rounds := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Hello"}},
		},
	}
	result := summarizeRounds(rounds, mock)
	// Should fall back to simple extraction on LLM error
	if !strings.Contains(result, "compacted") {
		t.Errorf("expected fallback compaction on LLM error, got: %s", result)
	}
}

func TestSummarizeRounds_WithLLMSuccess(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "This is a summary of the conversation.",
	}

	rounds := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Let's build a feature"}},
		},
	}
	result := summarizeRounds(rounds, mock)
	if !strings.Contains(result, "summarized by LLM") {
		t.Errorf("expected LLM summarization marker, got: %s", result)
	}
}

func TestSummarizeRounds_WithLLMEmptyResponse(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "",
	}

	rounds := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Hello"}},
		},
	}
	result := summarizeRounds(rounds, mock)
	// LLM produced nothing, should fall back
	if !strings.Contains(result, "compacted") {
		t.Errorf("expected fallback when LLM produces nothing, got: %s", result)
	}
}

func TestSummarizeRounds_LLMTruncation(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "Summary here.",
	}

	// Create very long transcript to trigger 8000-char truncation
	longText := strings.Repeat("a", 9000)
	rounds := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{Text: longText}},
		},
	}
	result := summarizeRounds(rounds, mock)
	if !strings.Contains(result, "summarized by LLM") {
		t.Errorf("expected LLM summary with long input, got: %s", result)
	}
}

func TestSummarizeRounds_WithStructuredSummary(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "A summary.",
	}

	rounds := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "file_write"}},
				{Text: "I will update main.go"},
			},
		},
	}
	result := summarizeRounds(rounds, mock)

	if !strings.Contains(result, "[SUMMARY]") {
		t.Errorf("expected [SUMMARY] block with structured data, got: %s", result)
	}
}

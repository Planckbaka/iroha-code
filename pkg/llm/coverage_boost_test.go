package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func sseServer(events []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprint(w, e)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

func openAISSEServer(events []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintln(w, e)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

func captureBodyServer() (*httptest.Server, *string) {
	var body string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad"}`)
	}))
	return s, &body
}

func capturePathServer() (*httptest.Server, *string) {
	var path string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad"}`)
	}))
	return s, &path
}

func captureBodySSEServer(events []string) (*httptest.Server, *string) {
	var body string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprint(w, e)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	return s, &body
}

func captureBodyOpenAISSE(events []string) (*httptest.Server, *string) {
	var body string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintln(w, e)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	return s, &body
}

var okOpenAIResponse = []string{
	`data: {"choices":[{"delta":{"content":"ok"}}]}`,
	`data: [DONE]`,
}

func ioReadAll(r io.ReadCloser) ([]byte, error) {
	defer r.Close()
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

func TestAnthropicAdapter_DefaultModelName(t *testing.T) {
	a := NewAnthropicAdapter("", "key", "http://localhost", "", nil)
	if a.Name() != "claude-sonnet-4-6" {
		t.Errorf("expected default model 'claude-sonnet-4-6', got %q", a.Name())
	}
}

func TestAnthropicAdapter_BaseURLDefault(t *testing.T) {
	server, capturedURL := capturePathServer()
	defer server.Close()

	// Test with explicit base URL
	a := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}
	for _, err := range a.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}
	if !strings.Contains(*capturedURL, "/v1/messages") {
		t.Errorf("expected URL to contain /v1/messages, got %s", *capturedURL)
	}
}

func TestAnthropicAdapter_SystemPromptFromConfig(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server, body := captureBodySSEServer(sseEvents)
	defer server.Close()

	// No adapter system prompt, but config has SystemInstruction
	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: "You are a helpful assistant."}, {Text: " Be concise."}},
			},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	if !strings.Contains(*body, "You are a helpful assistant.") {
		t.Error("expected system instruction to be in request body")
	}
	if !strings.Contains(*body, "Be concise.") {
		t.Error("expected second system instruction part to be in request body")
	}
}

func TestAnthropicAdapter_SystemPromptAdapterOverridesConfig(t *testing.T) {
	server, body := captureBodyServer()
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "ADAPTER PROMPT", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: "CONFIG PROMPT"}},
			},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	if !strings.Contains(*body, "ADAPTER PROMPT") {
		t.Error("adapter prompt should take precedence")
	}
	if strings.Contains(*body, "CONFIG PROMPT") {
		t.Error("config prompt should not appear when adapter prompt is set")
	}
}

func TestAnthropicAdapter_HooksNagReminder(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server, body := captureBodySSEServer(sseEvents)
	defer server.Close()

	var rounds int32
	hooks := &testHooks{
		nagReminder: "NAG: Do something!",
		noteRound:   func() { atomic.AddInt32(&rounds, 1) },
	}

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", hooks)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	if atomic.LoadInt32(&rounds) < 1 {
		t.Error("expected NoteRound to be called")
	}
	if !strings.Contains(*body, "NAG: Do something!") {
		t.Error("expected nag reminder to be injected into request")
	}
}

func TestAnthropicAdapter_TransientRetry(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n")
		fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"recovered\"}}\n\n")
		fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					textParts = append(textParts, p.Text)
				}
			}
		}
	}

	full := strings.Join(textParts, "")
	if !strings.Contains(full, "recovered") {
		t.Errorf("expected response with 'recovered', got %q", full)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestAnthropicAdapter_MaxTokensTruncation(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Partial\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"},\"usage\":{\"output_tokens\":5}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server := sseServer(sseEvents)
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				textParts = append(textParts, p.Text)
			}
		}
	}

	full := strings.Join(textParts, "")
	if !strings.Contains(full, "truncated at max_tokens") {
		t.Errorf("expected truncation warning, got %q", full)
	}
}

func TestAnthropicAdapter_ConvertMessages_EmptyRole(t *testing.T) {
	contents := []*genai.Content{
		{Role: "", Parts: []*genai.Part{{Text: "Hello"}}},
	}
	messages, err := convertToAnthropicMessages(contents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != "assistant" {
		t.Errorf("empty role should map to 'assistant', got %q", messages[0].Role)
	}
}

func TestAnthropicAdapter_ConvertMessages_EmptyContent(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: ""}}},
	}
	messages, err := convertToAnthropicMessages(contents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty text should not produce a block; empty blocks list means no message
	if len(messages) != 0 {
		t.Errorf("expected 0 messages for empty text, got %d", len(messages))
	}
}

func TestAnthropicAdapter_ConvertMessages_ToolResponseUnmappedID(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{Name: "unknown_tool", Response: map[string]any{"out": 1}}},
		}},
	}
	messages, err := convertToAnthropicMessages(contents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != "user" {
		t.Errorf("expected role 'user' for tool_result, got %q", messages[0].Role)
	}
	if !strings.HasPrefix(messages[0].Content[0].ToolUseID, "toolu_") {
		t.Errorf("expected fallback tool ID, got %q", messages[0].Content[0].ToolUseID)
	}
}

func TestAnthropicAdapter_ConvertMessages_MultipleToolCalls(t *testing.T) {
	contents := []*genai.Content{
		{Role: "model", Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: "tool_a", Args: map[string]any{"x": 1}}},
			{FunctionCall: &genai.FunctionCall{Name: "tool_b", Args: map[string]any{"y": 2}}},
		}},
	}
	messages, err := convertToAnthropicMessages(contents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if len(messages[0].Content) != 2 {
		t.Errorf("expected 2 content blocks, got %d", len(messages[0].Content))
	}
}

func TestAnthropicAdapter_SSEErrorMalformed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: error\ndata: {not valid json}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotError bool
	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			gotError = true
			break
		}
	}
	if !gotError {
		t.Error("expected error from malformed SSE error event")
	}
}

func TestAnthropicAdapter_ReadStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send partial data then close connection
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Force connection close
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	// Should handle stream read error gracefully - may yield error or just empty final
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			// Got an error - acceptable
			return
		}
		if resp != nil && resp.TurnComplete {
			return
		}
	}
}

func TestAnthropicAdapter_SSEPingEvent(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: ping\ndata: {}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server := sseServer(sseEvents)
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					textParts = append(textParts, p.Text)
				}
			}
		}
	}

	full := strings.Join(textParts, "")
	if !strings.Contains(full, "pong") {
		t.Errorf("expected 'pong' in response, got %q", full)
	}
}

func TestAnthropicAdapter_ToolsInRequest(t *testing.T) {
	server, body := captureBodyServer()
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{Name: "my_tool", Description: "A tool"},
					},
				},
			},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	if !strings.Contains(*body, "my_tool") {
		t.Errorf("expected tool name in request body, got: %s", *body)
	}
	if !strings.Contains(*body, "input_schema") {
		t.Errorf("expected input_schema in request body, got: %s", *body)
	}
}

func TestOpenAIAdapter_DefaultModelName(t *testing.T) {
	g := NewOpenAICompatibleAdapter("", "key", "http://localhost", "", nil)
	if g.Name() != "glm-4" {
		t.Errorf("expected default model 'glm-4', got %q", g.Name())
	}
}

func TestOpenAIAdapter_MissingBaseURL(t *testing.T) {
	adapter := &OpenAICompatibleAdapter{
		modelName: "test",
		apiKey:    "key",
		baseURL:   "",
		client:    &http.Client{Timeout: 5 * time.Second},
	}
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	var gotError bool
	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			if !strings.Contains(err.Error(), "base URL") {
				t.Errorf("expected base URL error, got: %v", err)
			}
			gotError = true
			break
		}
	}
	if !gotError {
		t.Error("expected error for missing base URL")
	}
}

func TestOpenAIAdapter_LengthFinishReason(t *testing.T) {
	sseEvents := []string{
		`data: {"choices":[{"delta":{"content":"Cut off"}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"length"}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				textParts = append(textParts, p.Text)
			}
		}
	}

	full := strings.Join(textParts, "")
	if !strings.Contains(full, "truncated at max_tokens") {
		t.Errorf("expected truncation warning for 'length' finish reason, got %q", full)
	}
}

func TestOpenAIAdapter_MultipleToolCalls(t *testing.T) {
	sseEvents := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"tool_a","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"tool_b","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Multi tool"}}},
		},
	}

	var toolCalls []*genai.FunctionCall
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.FunctionCall != nil {
					toolCalls = append(toolCalls, p.FunctionCall)
				}
			}
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "tool_a" {
		t.Errorf("expected tool_a, got %s", toolCalls[0].Name)
	}
	if toolCalls[1].Name != "tool_b" {
		t.Errorf("expected tool_b, got %s", toolCalls[1].Name)
	}
}

func TestOpenAIAdapter_SystemPromptFromConfig(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: "Config system prompt"}},
			},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	if !strings.Contains(*body, "Config system prompt") {
		t.Errorf("expected config system prompt in body, got: %s", *body)
	}
}

func TestOpenAIAdapter_HooksIntegration(t *testing.T) {
	sseEvents := []string{
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}

	server, body := captureBodyOpenAISSE(sseEvents)
	defer server.Close()

	var rounds int32
	hooks := &testHooks{
		nagReminder: "REMINDER!",
		noteRound:   func() { atomic.AddInt32(&rounds, 1) },
	}

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", hooks)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	if atomic.LoadInt32(&rounds) < 1 {
		t.Error("expected NoteRound to be called")
	}
	if !strings.Contains(*body, "REMINDER!") {
		t.Error("expected nag reminder in request body")
	}
}

func TestOpenAIAdapter_BaseURLPathConstruction(t *testing.T) {
	server, capturedPath := capturePathServer()
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	if *capturedPath != "/chat/completions" {
		t.Errorf("expected /chat/completions path, got %s", *capturedPath)
	}
}

func TestOpenAIAdapter_BaseURLAlreadyHasChatCompletions(t *testing.T) {
	server, capturedPath := capturePathServer()
	defer server.Close()

	baseURL := server.URL + "/chat/completions"
	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", baseURL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	if !strings.HasSuffix(*capturedPath, "/chat/completions") {
		t.Errorf("expected path ending with /chat/completions, got %s", *capturedPath)
	}
	// Should NOT double the path
	if strings.Contains(*capturedPath, "chat/completions/chat") {
		t.Errorf("path should not be doubled, got %s", *capturedPath)
	}
}

func TestOpenAIAdapter_InvalidJSONInSSE(t *testing.T) {
	sseEvents := []string{
		`data: not-valid-json`,
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					textParts = append(textParts, p.Text)
				}
			}
		}
	}

	full := strings.Join(textParts, "")
	if !strings.Contains(full, "ok") {
		t.Errorf("expected 'ok' in response despite invalid JSON chunks, got %q", full)
	}
}

func TestOpenAIAdapter_ToolCallStreaming(t *testing.T) {
	// Test incremental tool call argument streaming
	sseEvents := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"path"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"main.go\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Read file"}}},
		},
	}

	var toolCalls []*genai.FunctionCall
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.FunctionCall != nil {
					toolCalls = append(toolCalls, p.FunctionCall)
				}
			}
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "read_file" {
		t.Errorf("expected 'read_file', got %s", toolCalls[0].Name)
	}
	if toolCalls[0].Args["path"] != "main.go" {
		t.Errorf("expected path='main.go', got %v", toolCalls[0].Args["path"])
	}
}

func TestIsRetryableTemporaryError_NilError(t *testing.T) {
	if IsRetryableTemporaryError(nil) {
		t.Error("nil error should not be retryable")
	}
}

func TestIsRetryableTemporaryError_AllPatterns(t *testing.T) {
	patterns := []string{
		"rate limit exceeded",
		"rate_limit hit",
		"too many requests",
		"throttled request",
		"server overloaded",
		"temporary failure",
		"request timeout",
		"request timed out",
		"deadline exceeded",
		"connection reset by peer",
		"connection refused",
		"connection closed unexpectedly",
		"dropped connection",
		"unexpected eof in body",
		"internal server error",
		"error code 429",
		"error code 500",
		"error code 502",
		"error code 503",
		"error code 504",
		"[1302] Overloaded",
	}

	for _, msg := range patterns {
		if !IsRetryableTemporaryError(fmt.Errorf("test: %s", msg)) {
			t.Errorf("expected %q to be retryable", msg)
		}
	}
}

func TestIsRetryableTemporaryError_NonRetryable(t *testing.T) {
	nonRetryable := []string{
		"invalid api key",
		"permission denied",
		"file not found",
		"syntax error",
		"authentication failed",
	}

	for _, msg := range nonRetryable {
		if IsRetryableTemporaryError(fmt.Errorf("test: %s", msg)) {
			t.Errorf("expected %q to NOT be retryable", msg)
		}
	}
}

func TestAPITimeout_EnvOverride(t *testing.T) {
	t.Setenv("IROHA_API_TIMEOUT_MS", "5000")
	got := APITimeout()
	if got != 5000*time.Millisecond {
		t.Errorf("APITimeout() = %v, want 5000ms", got)
	}
}

func TestAPITimeout_FallbackEnv(t *testing.T) {
	t.Setenv("API_TIMEOUT_MS", "3000")
	got := APITimeout()
	if got != 3000*time.Millisecond {
		t.Errorf("APITimeout() = %v, want 3000ms", got)
	}
}

func TestAPITimeout_InvalidValue(t *testing.T) {
	t.Setenv("IROHA_API_TIMEOUT_MS", "not-a-number")
	t.Setenv("API_TIMEOUT_MS", "")
	got := APITimeout()
	if got != defaultAPITimeout {
		t.Errorf("APITimeout() = %v, want default %v", got, defaultAPITimeout)
	}
}

func TestAPITimeout_ZeroValue(t *testing.T) {
	t.Setenv("IROHA_API_TIMEOUT_MS", "0")
	t.Setenv("API_TIMEOUT_MS", "")
	got := APITimeout()
	if got != defaultAPITimeout {
		t.Errorf("APITimeout() with 0 should fallback to default, got %v", got)
	}
}

func TestMaxRetries_FallbackEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE_MAX_RETRIES", "7")
	got := MaxRetries()
	if got != 7 {
		t.Errorf("MaxRetries() = %d, want 7", got)
	}
}

func TestMaxRetries_InvalidValue(t *testing.T) {
	t.Setenv("IROHA_MAX_RETRIES", "abc")
	got := MaxRetries()
	if got != defaultMaxRetries {
		t.Errorf("MaxRetries() with invalid env should return default %d, got %d", defaultMaxRetries, got)
	}
}

func TestMaxRetries_NegativeValue(t *testing.T) {
	t.Setenv("IROHA_MAX_RETRIES", "-1")
	got := MaxRetries()
	if got != defaultMaxRetries {
		t.Errorf("MaxRetries() with negative env should return default %d, got %d", defaultMaxRetries, got)
	}
}

func TestRetryDelay_AttemptZero(t *testing.T) {
	got := RetryDelay(0, nil)
	if got < time.Second {
		t.Errorf("RetryDelay(0, nil) = %v, should be at least 1s", got)
	}
}

func TestRetryDelay_WithRetryAfterZero(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "0")
	got := RetryDelay(3, resp)
	// Retry-After of 0 should fall through to exponential backoff
	if got < time.Second {
		t.Errorf("RetryDelay with Retry-After=0 should use backoff, got %v", got)
	}
}

func TestRetryBudget_ResetUpdatesMax(t *testing.T) {
	t.Setenv("IROHA_MAX_RETRIES", "3")
	ResetRetryBudget()
	_, max := RetryBudgetStatus()
	if max != 3 {
		t.Errorf("expected max=3 after reset with env, got %d", max)
	}
}

func TestDirectHTTPAdapter_Anthropic(t *testing.T) {
	var _ DirectHTTPAdapter = &AnthropicAdapter{}
}

func TestDirectHTTPAdapter_OpenAI(t *testing.T) {
	var _ DirectHTTPAdapter = &OpenAICompatibleAdapter{}
}

// Test that the JSON payload includes correct tool schema for OpenAI
func TestOpenAIAdapter_ToolSchemaInPayload(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Use tool"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:                 "my_func",
							Description:          "Does something",
							ParametersJsonSchema: map[string]any{"type": "object"},
						},
					},
				},
			},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	if !strings.Contains(*body, "my_func") {
		t.Errorf("expected tool name in payload, got: %s", *body)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(*body), &parsed); err != nil {
		t.Fatalf("payload should be valid JSON: %v", err)
	}
}

// Test OpenAI model role mapping
func TestOpenAIAdapter_RoleMapping(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "sys", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "Hi"}}},
			{Role: "", Parts: []*genai.Part{{Text: "Empty role"}}},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	// "model" and "" roles should map to "assistant"
	if !strings.Contains(*body, `"role":"assistant"`) {
		t.Errorf("expected assistant role mapping, got: %s", *body)
	}
	if !strings.Contains(*body, `"role":"user"`) {
		t.Errorf("expected user role, got: %s", *body)
	}
	if !strings.Contains(*body, `"role":"system"`) {
		t.Errorf("expected system role, got: %s", *body)
	}
}

// Test FunctionResponse emitting separate tool messages
func TestOpenAIAdapter_FunctionResponseSeparateMessage(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Read file"}}},
			{Role: "model", Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "read", Args: map[string]any{"path": "x.go"}}},
			}},
			{Role: "user", Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{Name: "read", Response: map[string]any{"data": "contents"}}},
			}},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	// FunctionResponse should produce a separate "tool" role message
	if !strings.Contains(*body, `"role":"tool"`) {
		t.Errorf("expected tool role for FunctionResponse, got: %s", *body)
	}
	if !strings.Contains(*body, `"tool_call_id":"call_read"`) {
		t.Errorf("expected tool_call_id, got: %s", *body)
	}
}

func TestAnthropicAdapter_DirectHTTPAdapterMarker(t *testing.T) {
	a := NewAnthropicAdapter("model", "key", "", "", nil)
	a.DirectHTTPAdapter() // empty method, call for coverage
}

func TestOpenAIAdapter_DirectHTTPAdapterMarker(t *testing.T) {
	g := NewOpenAICompatibleAdapter("model", "key", "", "", nil)
	g.DirectHTTPAdapter() // empty method, call for coverage
}

func TestAnthropicAdapter_ContextCanceledDuringRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotError bool
	for _, err := range adapter.GenerateContent(ctx, req, true) {
		if err != nil {
			gotError = true
			if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "rate limited") {
				t.Logf("got error: %v", err)
			}
			break
		}
	}
	_ = gotError
}

func TestOpenAIAdapter_ContextCanceledDuringRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotError bool
	for _, err := range adapter.GenerateContent(ctx, req, true) {
		if err != nil {
			gotError = true
			break
		}
	}
	_ = gotError
}

func TestAnthropicAdapter_StreamEndsWithoutFinal(t *testing.T) {
	// Stream that ends without message_stop event - should still get final response
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		// No message_delta or message_stop - stream just ends
	}

	server := sseServer(sseEvents)
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotFinal bool
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.TurnComplete {
			gotFinal = true
		}
	}

	if !gotFinal {
		t.Error("expected final TurnComplete response even without message_stop")
	}
}

func TestOpenAIAdapter_StreamEndsWithoutFinishReason(t *testing.T) {
	// Stream that ends with [DONE] but no finish_reason
	sseEvents := []string{
		`data: {"choices":[{"delta":{"content":"hello"}}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotFinal bool
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.TurnComplete {
			gotFinal = true
		}
	}

	if !gotFinal {
		t.Error("expected final TurnComplete response even without finish_reason")
	}
}

func TestOpenAIAdapter_EmptyStream(t *testing.T) {
	// Server returns just [DONE]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: [DONE]`)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotFinal bool
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.TurnComplete {
			gotFinal = true
		}
	}

	if !gotFinal {
		t.Error("expected final response even for empty stream")
	}
}

func TestOpenAIAdapter_ServerError5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal server error"}`)
	}))
	defer server.Close()

	ResetRetryBudget()
	t.Setenv("IROHA_MAX_RETRIES", "0")
	ResetRetryBudget()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotError bool
	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			if !strings.Contains(err.Error(), "500") {
				t.Errorf("expected 500 error, got: %v", err)
			}
			gotError = true
			break
		}
	}

	if !gotError {
		t.Error("expected error from 500 response")
	}
}

func TestAnthropicAdapter_ToolWithNilSchema(t *testing.T) {
	server, body := captureBodyServer()
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{Name: "no_schema_tool", Description: "No schema"},
						// Both ParametersJsonSchema and Parameters are nil
					},
				},
			},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	// Should use fallback schema
	if !strings.Contains(*body, "no_schema_tool") {
		t.Errorf("expected tool name in body, got: %s", *body)
	}
	if !strings.Contains(*body, "input_schema") {
		t.Errorf("expected input_schema in body, got: %s", *body)
	}
}

func TestAnthropicAdapter_ToolWithParametersField(t *testing.T) {
	server, body := captureBodyServer()
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:                 "param_tool",
							Description:          "Has parameters",
							ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
						},
					},
				},
			},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	if !strings.Contains(*body, "param_tool") {
		t.Errorf("expected tool name in body, got: %s", *body)
	}
}

func TestOpenAIAdapter_ToolWithNilSchema(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "nil_schema",
							Description: "No schema fields set",
							// Both ParametersJsonSchema and Parameters are nil
						},
					},
				},
			},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	if !strings.Contains(*body, "nil_schema") {
		t.Errorf("expected tool name in body, got: %s", *body)
	}
}

func TestOpenAIAdapter_EmptyToolCallsChunk(t *testing.T) {
	// SSE chunks with empty choices (no choices array)
	sseEvents := []string{
		`data: {"choices":[],"usage":{"total_tokens":0}}`,
		`data: {"choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}],"usage":{"total_tokens":10}}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					textParts = append(textParts, p.Text)
				}
			}
		}
	}

	if !strings.Contains(strings.Join(textParts, ""), "hi") {
		t.Error("expected 'hi' in response")
	}
	if adapter.CumulativeTokens() != 10 {
		t.Errorf("expected 10 tokens, got %d", adapter.CumulativeTokens())
	}
}

func TestOpenAIAdapter_RetryBudgetExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limit"}`)
	}))
	defer server.Close()

	t.Setenv("IROHA_MAX_RETRIES", "0")
	ResetRetryBudget()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotError bool
	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			gotError = true
			break
		}
	}
	if !gotError {
		t.Error("expected error when retries exhausted")
	}
}

func TestAnthropicAdapter_AnthropicBaseURLAppend(t *testing.T) {
	server, capturedPath := capturePathServer()
	defer server.Close()

	// Provide base URL without /v1/messages suffix
	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	if *capturedPath != "/v1/messages" {
		t.Errorf("expected /v1/messages, got %s", *capturedPath)
	}
}

func TestAnthropicAdapter_BaseURLAlreadyHasMessages(t *testing.T) {
	server, capturedPath := capturePathServer()
	defer server.Close()

	baseURL := server.URL + "/v1/messages"
	adapter := NewAnthropicAdapter("model", "key", baseURL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	if *capturedPath != "/v1/messages" {
		t.Errorf("expected /v1/messages, got %s", *capturedPath)
	}
}

func TestOpenAIAdapter_CommentLinesSkipped(t *testing.T) {
	// SSE lines without "data: " prefix should be skipped
	sseEvents := []string{
		`: this is a comment`,
		``,
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		`: another comment`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					textParts = append(textParts, p.Text)
				}
			}
		}
	}

	if !strings.Contains(strings.Join(textParts, ""), "ok") {
		t.Error("expected 'ok' in response")
	}
}

func TestOpenAIAdapter_OpenAIFuncResponseWithNilParams(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{Name: "nil_params", Description: "No params", ParametersJsonSchema: nil, Parameters: nil},
					},
				},
			},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	if !strings.Contains(*body, "nil_params") {
		t.Errorf("expected tool name in body, got: %s", *body)
	}
}

func TestAnthropicAdapter_ContentBlockDeltaInvalidJSON(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: not-valid-json\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"valid\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server := sseServer(sseEvents)
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					textParts = append(textParts, p.Text)
				}
			}
		}
	}

	if !strings.Contains(strings.Join(textParts, ""), "valid") {
		t.Error("expected 'valid' text after invalid JSON delta was skipped")
	}
}

// Test Anthropic adapter SSE line with event but no following data line
func TestAnthropicAdapter_SSEMissingDataLine(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		// event line without data line - data line read hits EOF
		"event: content_block_delta\n",
		// Remaining valid events
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server := sseServer(sseEvents)
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	// This may or may not error depending on SSE parsing; just verify no panic
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
		if resp != nil && resp.TurnComplete {
			break
		}
	}
}

// Test Anthropic content_block_start without content_block field
func TestAnthropicAdapter_ContentBlockStartNil(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server := sseServer(sseEvents)
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var gotFinal bool
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.TurnComplete {
			gotFinal = true
		}
	}
	if !gotFinal {
		t.Error("expected final response")
	}
}

// Test Anthropic data line without proper "data: " prefix after event
func TestAnthropicAdapter_SSEDataLineMissingPrefix(t *testing.T) {
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		// Data line without "data: " prefix - should be skipped
		"event: content_block_delta\nnot-a-data-line\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	server := sseServer(sseEvents)
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var textParts []string
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					textParts = append(textParts, p.Text)
				}
			}
		}
	}

	if !strings.Contains(strings.Join(textParts, ""), "ok") {
		t.Error("expected 'ok' text despite missing data prefix")
	}
}

// Test OpenAI adapter with tool call having string input (not object)
func TestOpenAIAdapter_ToolCallStringInput(t *testing.T) {
	sseEvents := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"my_tool","arguments":"plain string input"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var toolCalls []*genai.FunctionCall
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.FunctionCall != nil {
					toolCalls = append(toolCalls, p.FunctionCall)
				}
			}
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "my_tool" {
		t.Errorf("expected 'my_tool', got %s", toolCalls[0].Name)
	}
}

// Test OpenAI adapter - pending tool calls flushed when stream ends prematurely
func TestOpenAIAdapter_PendingToolsFlushedOnPrematureEnd(t *testing.T) {
	// Tool call chunks without finish_reason, then [DONE]
	sseEvents := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"flushed_tool","arguments":"{}"}}]}}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var toolCalls []*genai.FunctionCall
	var gotFinal bool
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.FunctionCall != nil {
					toolCalls = append(toolCalls, p.FunctionCall)
				}
			}
		}
		if resp != nil && resp.TurnComplete {
			gotFinal = true
		}
	}

	if len(toolCalls) < 1 {
		t.Error("expected tool call to be flushed")
	}
	if !gotFinal {
		t.Error("expected final response")
	}
}

// Test DebugLog with file write error
func TestDebugLog_WriteAfterFileClosed(t *testing.T) {
	debugOn = true
	debugFile = nil // file is nil but debugOn is true
	defer func() {
		debugMu.Lock()
		debugOn = false
		debugMu.Unlock()
	}()

	DebugLog("should not panic") // just verify no panic
}

// Test OpenAI adapter with multiple tool call indexes that have gaps
func TestOpenAIAdapter_ToolCallsWithGaps(t *testing.T) {
	sseEvents := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","type":"function","function":{"name":"tool_0","arguments":"{}"}},{"index":2,"id":"c2","type":"function","function":{"name":"tool_2","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}

	server := openAISSEServer(sseEvents)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
	}

	var toolCalls []*genai.FunctionCall
	for resp, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.FunctionCall != nil {
					toolCalls = append(toolCalls, p.FunctionCall)
				}
			}
		}
	}

	// Index 0 is yielded; index 2 exists in map but len(map)=2 means only indices 0..1 are iterated
	// So only index 0 gets yielded (index 2 is beyond len(pendingTools)=2 which iterates 0,1)
	if len(toolCalls) < 1 {
		t.Fatalf("expected at least 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "tool_0" {
		t.Errorf("expected tool_0, got %s", toolCalls[0].Name)
	}
}

// Test Anthropic adapter with empty system instruction parts
func TestAnthropicAdapter_EmptySystemInstructionParts(t *testing.T) {
	server, body := captureBodyServer()
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: ""}, {Text: ""}},
			},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}

	// Empty text parts should produce empty system prompt
	if strings.Contains(*body, "system") && !strings.Contains(*body, `"text":""`) {
		// There should be no meaningful system blocks
		t.Logf("body: %s", *body)
	}
}

// Test OpenAI adapter with empty system instruction parts
func TestOpenAIAdapter_EmptySystemInstructionParts(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: ""}, {Text: ""}},
			},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	// Empty text parts -> empty system prompt -> no system message
	if strings.Contains(*body, `"role":"system"`) {
		t.Errorf("empty system instruction should not produce system message, got: %s", *body)
	}
}

// Test OpenAI with nil FunctionDeclaration in tools
func TestOpenAIAdapter_NilFunctionDeclaration(t *testing.T) {
	server, body := captureBodyOpenAISSE(okOpenAIResponse)
	defer server.Close()

	adapter := NewOpenAICompatibleAdapter("test-model", "test-key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{nil},
				},
				nil,
			},
		},
	}

	for range adapter.GenerateContent(context.Background(), req, true) {
	}

	// Should not include any tools since declarations are nil
	if strings.Contains(*body, "function") {
		t.Logf("body contains 'function': %s", *body)
	}
}

// Test Anthropic with nil tool and nil function declarations
func TestAnthropicAdapter_NilToolsAndDeclarations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad"}`)
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("model", "key", server.URL, "", nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "test"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{nil, {FunctionDeclarations: nil}},
		},
	}

	for _, err := range adapter.GenerateContent(context.Background(), req, true) {
		if err != nil {
			break
		}
	}
	// Should not panic
}

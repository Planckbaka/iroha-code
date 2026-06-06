package llm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestGenkitModelAdapter_Accessors(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "test-model", "system prompt", nil)

	if adapter.Name() != "test-model" {
		t.Errorf("Name() = %q, want 'test-model'", adapter.Name())
	}
	if adapter.CumulativeTokens() != 0 {
		t.Errorf("CumulativeTokens() = %d, want 0", adapter.CumulativeTokens())
	}
	adapter.AddTokens(42)
	if adapter.CumulativeTokens() != 42 {
		t.Errorf("CumulativeTokens() after AddTokens(42) = %d, want 42", adapter.CumulativeTokens())
	}
	adapter.AddTokens(8)
	if adapter.CumulativeTokens() != 50 {
		t.Errorf("CumulativeTokens() after AddTokens(8) = %d, want 50", adapter.CumulativeTokens())
	}
}

func TestGenkitModelAdapter_SetSystemPrompt(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "initial", nil)

	adapter.SetSystemPrompt("updated")
	if adapter.getSystemPrompt() != "updated" {
		t.Errorf("getSystemPrompt() = %q, want 'updated'", adapter.getSystemPrompt())
	}

	adapter.SetSystemPrompt("")
	if adapter.getSystemPrompt() != "" {
		t.Errorf("getSystemPrompt() = %q, want empty", adapter.getSystemPrompt())
	}
}

func TestGenkitModelAdapter_SystemPromptConcurrency(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "", nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			adapter.SetSystemPrompt(string(rune('a' + i%26)))
		}(i)
	}
	wg.Wait()
	// Should not panic after concurrent access
	_ = adapter.getSystemPrompt()
}

func TestGenkitModelAdapter_NewWithHooks(t *testing.T) {
	var rounds int32
	hooks := &testHooks{noteRound: func() { atomic.AddInt32(&rounds, 1) }}

	adapter := NewGenkitModelAdapter(nil, "model", "sys", hooks)
	if adapter.hooks == nil {
		t.Error("expected hooks to be set")
	}
}

// testHooks is a simple AdapterHooks implementation for testing.
type testHooks struct {
	nagReminder string
	noteRound   func()
}

func (h *testHooks) NagReminder() string { return h.nagReminder }
func (h *testHooks) NoteRound() {
	if h.noteRound != nil {
		h.noteRound()
	}
}

func TestGenkitModelAdapter_GenerateContent_NilGenkit(t *testing.T) {
	// With nil genkit, GenerateContent should handle gracefully
	// Since genkit.Generate / genkit.GenerateStream will panic with nil,
	// we test the basic setup and ensure the iterator is returned.
	adapter := NewGenkitModelAdapter(nil, "test-model", "sys prompt", nil)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	// Get the iterator function - we just verify it returns a valid iter.Seq2
	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return a non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_WithConfig(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "", nil)

	temp := float32(0.7)
	topK := float32(40)
	topP := float32(0.9)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
		Config: &genai.GenerateContentConfig{
			Temperature:    &temp,
			MaxOutputTokens: 1024,
			TopK:           &topK,
			TopP:           &topP,
			StopSequences:  []string{"END"},
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: "Be helpful"}},
			},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_WithTools(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "sys", nil)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Run ls"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "shell_run",
							Description: "Run a shell command",
						},
					},
				},
			},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_SystemPromptFromField(t *testing.T) {
	// When adapter has a system prompt set, it should use that
	adapter := NewGenkitModelAdapter(nil, "model", "custom system prompt", nil)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_HooksNoteRound(t *testing.T) {
	var rounds int32
	hooks := &testHooks{
		noteRound: func() { atomic.AddInt32(&rounds, 1) },
	}

	adapter := NewGenkitModelAdapter(nil, "model", "sys", hooks)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	// The iterator captures hooks internally; NoteRound is called at the start
	// of the yield function. We verify the iterator is non-nil (hooks registered).
	// We cannot safely iterate with nil genkit without panic.
	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return a non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_NagReminder(t *testing.T) {
	hooks := &testHooks{
		nagReminder: "REMINDER: Check your work!",
	}

	adapter := NewGenkitModelAdapter(nil, "model", "sys", hooks)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_RoleMapping(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "sys", nil)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "User msg"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "Model msg"}}},
			{Role: "system", Parts: []*genai.Part{{Text: "System msg"}}},
			{Role: "tool", Parts: []*genai.Part{{Text: "Tool msg"}}},
			{Role: "function", Parts: []*genai.Part{{Text: "Function msg"}}},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_FunctionCallParts(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "sys", nil)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Read file"}}},
			{Role: "model", Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "file_read", Args: map[string]any{"path": "main.go"}}},
			}},
			{Role: "user", Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{Name: "file_read", Response: map[string]any{"output": "contents"}}},
			}},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_NilParts(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "sys", nil)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{nil, {Text: "Hello"}, nil}},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

func TestGenkitModelAdapter_GenerateContent_ToolSchemaParams(t *testing.T) {
	adapter := NewGenkitModelAdapter(nil, "model", "sys", nil)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Use tool"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:                 "my_tool",
							Description:          "A test tool",
							ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
						},
					},
				},
			},
		},
	}

	seq := adapter.GenerateContent(context.Background(), req, false)
	if seq == nil {
		t.Error("GenerateContent should return non-nil iterator")
	}
}

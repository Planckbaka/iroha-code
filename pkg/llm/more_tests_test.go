package llm

import (
	"context"
	"iter"
	"os"
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestDebugLog_InitAndWrite(t *testing.T) {
	// Reset global state
	debugOn = false
	debugFile = nil

	InitDebugLog()
	defer func() {
		debugMu.Lock()
		if debugFile != nil {
			debugFile.Close()
			debugFile = nil
		}
		debugOn = false
		debugMu.Unlock()
		os.Remove(debugLogPath)
	}()

	if !debugOn {
		t.Fatal("expected debugOn after InitDebugLog")
	}

	DebugLog("test message %d", 42)

	data, err := os.ReadFile(debugLogPath)
	if err != nil {
		t.Fatalf("failed to read debug log: %v", err)
	}
	if !strings.Contains(string(data), "test message 42") {
		t.Errorf("log should contain message, got: %s", string(data))
	}
}

func TestDebugLog_InitIdempotent(t *testing.T) {
	debugOn = false
	debugFile = nil
	defer func() {
		debugMu.Lock()
		if debugFile != nil {
			debugFile.Close()
			debugFile = nil
		}
		debugOn = false
		debugMu.Unlock()
		os.Remove(debugLogPath)
	}()

	InitDebugLog()
	first := debugFile
	InitDebugLog()
	if debugFile != first {
		t.Error("second InitDebugLog should not replace file")
	}
}

func TestDebugLog_SkipsWhenOff(t *testing.T) {
	debugOn = false
	debugFile = nil
	os.Remove(debugLogPath)

	DebugLog("should not write")
	if _, err := os.Stat(debugLogPath); !os.IsNotExist(err) {
		t.Error("expected no log file when debug is off")
	}
}

func TestDumpDebugFile(t *testing.T) {
	debugOn = false
	debugFile = nil
	os.Remove(debugLogPath)

	// Off — should not write
	DumpDebugFile("test", []byte("data"))

	InitDebugLog()
	defer func() {
		debugMu.Lock()
		if debugFile != nil {
			debugFile.Close()
			debugFile = nil
		}
		debugOn = false
		debugMu.Unlock()
		os.Remove(debugLogPath)
	}()

	DumpDebugFile("test", []byte("hello"))
	data, err := os.ReadFile("/tmp/iroha-debug-test")
	if err != nil {
		t.Fatalf("failed to read dump file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("dump file content = %q, want 'hello'", string(data))
	}
	os.Remove("/tmp/iroha-debug-test")
}

func TestAnthropicAdapter_Accessors(t *testing.T) {
	a := NewAnthropicAdapter("test-model", "key", "http://localhost", "sys", nil)

	if a.Name() != "test-model" {
		t.Errorf("Name() = %q, want 'test-model'", a.Name())
	}
	if a.CumulativeTokens() != 0 {
		t.Errorf("CumulativeTokens() = %d, want 0", a.CumulativeTokens())
	}
	a.AddTokens(100)
	if a.CumulativeTokens() != 100 {
		t.Errorf("CumulativeTokens() after AddTokens(100) = %d, want 100", a.CumulativeTokens())
	}
	a.SetSystemPrompt("new prompt")
	if a.systemPrompt != "new prompt" {
		t.Error("SetSystemPrompt did not update systemPrompt")
	}
	// DirectHTTPAdapter is just a marker — call it to ensure no panic
	a.DirectHTTPAdapter()
}

func TestOpenAIAdapter_Accessors(t *testing.T) {
	g := NewOpenAICompatibleAdapter("gpt-test", "key", "http://localhost", "sys", nil)

	if g.Name() != "gpt-test" {
		t.Errorf("Name() = %q, want 'gpt-test'", g.Name())
	}
	if g.CumulativeTokens() != 0 {
		t.Errorf("CumulativeTokens() = %d, want 0", g.CumulativeTokens())
	}
	g.AddTokens(50)
	if g.CumulativeTokens() != 50 {
		t.Errorf("CumulativeTokens() after AddTokens(50) = %d, want 50", g.CumulativeTokens())
	}
	g.SetSystemPrompt("new prompt")
	if g.systemPrompt != "new prompt" {
		t.Error("SetSystemPrompt did not update systemPrompt")
	}
	g.DirectHTTPAdapter()
}

type mockLLM struct {
	responses []*model.LLMResponse
	errs      []error
	called    int
}

func (m *mockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for i, resp := range m.responses {
			m.called++
			var err error
			if i < len(m.errs) {
				err = m.errs[i]
			}
			if !yield(resp, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

func (m *mockLLM) Name() string              { return "mock" }
func (m *mockLLM) CumulativeTokens() int      { return 0 }
func (m *mockLLM) AddTokens(int)              {}

func TestCollectNonStreaming_SingleResponse(t *testing.T) {
	m := &mockLLM{
		responses: []*model.LLMResponse{
			{Content: &genai.Content{Parts: []*genai.Part{{Text: "hello "}}}},
			{Content: &genai.Content{Parts: []*genai.Part{{Text: "world"}}}},
		},
	}
	text, err := CollectNonStreaming(context.Background(), m, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world" {
		t.Errorf("text = %q, want 'hello world'", text)
	}
}

func TestCollectNonStreaming_EmptyParts(t *testing.T) {
	m := &mockLLM{
		responses: []*model.LLMResponse{
			{Content: &genai.Content{Parts: []*genai.Part{{Text: ""}}}},
			{Content: &genai.Content{Parts: []*genai.Part{{Text: "data"}}}},
		},
	}
	text, err := CollectNonStreaming(context.Background(), m, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "data" {
		t.Errorf("text = %q, want 'data'", text)
	}
}

func TestCollectNonStreaming_Error(t *testing.T) {
	m := &mockLLM{
		responses: []*model.LLMResponse{
			{Content: &genai.Content{Parts: []*genai.Part{{Text: "partial"}}}},
			{Content: &genai.Content{Parts: []*genai.Part{{Text: "more"}}}},
		},
		errs: []error{nil, context.Canceled},
	}
	text, err := CollectNonStreaming(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if text != "partial" {
		t.Errorf("text = %q, want 'partial'", text)
	}
}
func TestCollectNonStreaming_NilResponse(t *testing.T) {
	m := &mockLLM{
		responses: []*model.LLMResponse{nil},
	}
	text, err := CollectNonStreaming(context.Background(), m, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
}

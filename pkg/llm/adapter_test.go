package llm

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestNewAdapter_OpenAIFormat(t *testing.T) {
	providers := []ProviderType{ProviderGLM, ProviderOpenAI, ProviderDeepSeek, ProviderKimi, ProviderSiliconFlow}
	for _, p := range providers {
		t.Run(string(p)+"_openai", func(t *testing.T) {
			llm, err := NewAdapter(nil, p, "test-model", "key", "http://localhost", "prompt", APIFormatOpenAI, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := llm.(*OpenAICompatibleAdapter); !ok {
				t.Errorf("expected *OpenAICompatibleAdapter, got %T", llm)
			}
		})
	}
}

func TestNewAdapter_AnthropicFormat(t *testing.T) {
	providers := []ProviderType{ProviderGLM, ProviderDeepSeek}
	for _, p := range providers {
		t.Run(string(p)+"_anthropic", func(t *testing.T) {
			llm, err := NewAdapter(nil, p, "test-model", "key", "http://localhost", "prompt", APIFormatAnthropic, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := llm.(*AnthropicAdapter); !ok {
				t.Errorf("expected *AnthropicAdapter, got %T", llm)
			}
		})
	}
}

func TestNewAdapter_Claude_NilGenkit(t *testing.T) {
	llm, err := NewAdapter(nil, ProviderClaude, "claude-sonnet", "key", "", "prompt", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := llm.(*AnthropicAdapter); !ok {
		t.Errorf("expected *AnthropicAdapter for Claude with nil genkit, got %T", llm)
	}
}

func TestNewAdapter_Gemini_NilGenkit(t *testing.T) {
	_, err := NewAdapter(nil, ProviderGemini, "gemini-pro", "key", "", "prompt", "", nil)
	if err == nil {
		t.Fatal("expected error for Gemini with nil genkit, got nil")
	}
}

func TestNewAdapter_UnknownProvider(t *testing.T) {
	_, err := NewAdapter(nil, ProviderType("unknown"), "model", "key", "", "prompt", "", nil)
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestParseRetryAfter_IntegerSeconds(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "5")
	got := parseRetryAfter(resp)
	if got != 5.0 {
		t.Errorf("parseRetryAfter(integer) = %f, want 5.0", got)
	}
}

func TestParseRetryAfter_Empty(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	got := parseRetryAfter(resp)
	if got != 0 {
		t.Errorf("parseRetryAfter(empty) = %f, want 0", got)
	}
}

func TestParseRetryAfter_InvalidString(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "not-a-date")
	got := parseRetryAfter(resp)
	if got != 0 {
		t.Errorf("parseRetryAfter(invalid) = %f, want 0", got)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	resp.Header.Set("Retry-After", future)
	got := parseRetryAfter(resp)
	if got < 3.0 || got > 6.0 {
		t.Errorf("parseRetryAfter(http-date) = %f, want ~4-5", got)
	}
}

func TestParseRetryAfter_HTTPDatePast(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	past := time.Now().Add(-10 * time.Second).UTC().Format(http.TimeFormat)
	resp.Header.Set("Retry-After", past)
	got := parseRetryAfter(resp)
	if got != 1.0 {
		t.Errorf("parseRetryAfter(past http-date) = %f, want 1.0 (floor clamp)", got)
	}
}

func TestMinRetryDelay_Default(t *testing.T) {
	got := minRetryDelay()
	if got != time.Second {
		t.Errorf("minRetryDelay(default) = %v, want 1s", got)
	}
}

func TestMinRetryDelay_EnvOverride(t *testing.T) {
	t.Setenv("IROHA_MIN_RETRY_DELAY_MS", "500")
	got := minRetryDelay()
	if got != 500*time.Millisecond {
		t.Errorf("minRetryDelay(500ms) = %v, want 500ms", got)
	}
}

func TestMinRetryDelay_InvalidEnv(t *testing.T) {
	t.Setenv("IROHA_MIN_RETRY_DELAY_MS", "abc")
	got := minRetryDelay()
	if got != time.Second {
		t.Errorf("minRetryDelay(invalid) = %v, want 1s (fallback)", got)
	}
}

func TestMinRetryDelay_NegativeEnv(t *testing.T) {
	t.Setenv("IROHA_MIN_RETRY_DELAY_MS", "-10")
	got := minRetryDelay()
	if got != time.Second {
		t.Errorf("minRetryDelay(negative) = %v, want 1s (fallback)", got)
	}
}

func TestClampRetryDelay_BelowMin(t *testing.T) {
	got := clampRetryDelay(0)
	if got < time.Second {
		t.Errorf("clampRetryDelay(0) = %v, want >= 1s", got)
	}
}

func TestClampRetryDelay_AboveMax(t *testing.T) {
	got := clampRetryDelay(120 * time.Second)
	if got != maxRetryDelay {
		t.Errorf("clampRetryDelay(120s) = %v, want %v", got, maxRetryDelay)
	}
}

func TestClampRetryDelay_Normal(t *testing.T) {
	got := clampRetryDelay(5 * time.Second)
	if got != 5*time.Second {
		t.Errorf("clampRetryDelay(5s) = %v, want 5s", got)
	}
}

func TestBudgetExhaustedError(t *testing.T) {
	ResetRetryBudget()
	err := BudgetExhaustedError("test-model", errors.New("conn reset"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !contains(msg, "test-model") {
		t.Errorf("error should contain model name, got: %s", msg)
	}
	if !contains(msg, "retry budget exhausted") {
		t.Errorf("error should mention budget, got: %s", msg)
	}
	if !contains(msg, "conn reset") {
		t.Errorf("error should wrap last error, got: %s", msg)
	}
}

func TestIsRetryableHTTPStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{200, false},
	}
	for _, tt := range tests {
		got := IsRetryableHTTPStatus(tt.code)
		if got != tt.want {
			t.Errorf("IsRetryableHTTPStatus(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestRetryDelay_ExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		min     time.Duration
		max     time.Duration
	}{
		{1, time.Second, 2 * time.Second},
		{2, 2 * time.Second, 4 * time.Second},
		{3, 4 * time.Second, 8 * time.Second},
		{0, time.Second, 2 * time.Second},
	}
	for _, tt := range tests {
		got := RetryDelay(tt.attempt, nil)
		if got < tt.min || got > tt.max {
			t.Errorf("RetryDelay(%d, nil) = %v, want between %v and %v", tt.attempt, got, tt.min, tt.max)
		}
	}
}

func TestRetryNotice(t *testing.T) {
	notice := RetryNotice("test error", 2, 5, 4*time.Second)
	if notice == nil {
		t.Fatal("expected non-nil notice")
	}
	if notice.Partial != true {
		t.Error("expected Partial=true")
	}
	if notice.TurnComplete != false {
		t.Error("expected TurnComplete=false")
	}
	if len(notice.Content.Parts) == 0 {
		t.Fatal("expected at least one part")
	}
	text := notice.Content.Parts[0].Text
	if !contains(text, "test error") || !contains(text, "2/5") {
		t.Errorf("notice text should contain error and attempt info, got: %s", text)
	}
}

func TestRetryNotice_EmptyReason(t *testing.T) {
	notice := RetryNotice("", 1, 3, time.Second)
	text := notice.Content.Parts[0].Text
	if !contains(text, "temporary API error") {
		t.Errorf("empty reason should use fallback, got: %s", text)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

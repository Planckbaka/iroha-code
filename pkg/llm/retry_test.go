package llm

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryBudget(t *testing.T) {
	ResetRetryBudget()

	used, max := RetryBudgetStatus()
	if used != 0 {
		t.Errorf("expected 0 used, got %d", used)
	}
	if max != 10 {
		t.Errorf("expected max 10, got %d", max)
	}

	// Consume all retries
	for i := range 10 {
		if !ConsumeRetry() {
			t.Errorf("retry %d should succeed", i)
		}
	}

	// Should be exhausted
	if ConsumeRetry() {
		t.Error("expected retry budget to be exhausted")
	}

	// Verify status
	used, _ = RetryBudgetStatus()
	if used != 10 {
		t.Errorf("expected 10 used after exhaustion, got %d", used)
	}

	// Reset and verify
	ResetRetryBudget()
	used, _ = RetryBudgetStatus()
	if used != 0 {
		t.Errorf("expected 0 after reset, got %d", used)
	}

	// Should be able to consume again after reset
	if !ConsumeRetry() {
		t.Error("expected retry to succeed after reset")
	}
}

func TestRetryConfigEnvOverrides(t *testing.T) {
	t.Setenv("IROHA_MAX_RETRIES", "4")
	t.Setenv("IROHA_API_TIMEOUT_MS", "1234")

	if got := MaxRetries(); got != 4 {
		t.Fatalf("MaxRetries() = %d, want 4", got)
	}
	if got := APITimeout(); got != 1234*time.Millisecond {
		t.Fatalf("APITimeout() = %v, want 1234ms", got)
	}
}

func TestRetryableTemporaryErrorClassification(t *testing.T) {
	cases := []string{
		"anthropic API error: [1302][您的账户已达到速率限制，请您控制请求频率]",
		"rate limit exceeded",
		"server overloaded",
		"unexpected EOF",
		"context deadline exceeded",
	}
	for _, msg := range cases {
		if !IsRetryableTemporaryError(assertErr(msg)) {
			t.Fatalf("expected retryable error for %q", msg)
		}
	}
	if IsRetryableTemporaryError(assertErr("invalid api key")) {
		t.Fatal("auth/config errors should not be classified retryable")
	}
}

func TestRetryDelayUsesRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "2")

	if got := RetryDelay(1, resp); got != 2*time.Second {
		t.Fatalf("RetryDelay with Retry-After = %v, want 2s", got)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

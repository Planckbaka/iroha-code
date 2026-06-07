package llm

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

const (
	defaultMaxRetries = 10
	defaultAPITimeout = 600000 * time.Millisecond
	maxRetryDelay     = 60 * time.Second
)

// retryBudget tracks session-level retry consumption.
var retryBudget struct {
	mu         sync.Mutex
	used       int
	maxRetries int
}

func init() {
	retryBudget.maxRetries = MaxRetries()
}

// ConsumeRetry attempts to consume one retry from the session budget.
// Returns false if budget is exhausted.
func ConsumeRetry() bool {
	retryBudget.mu.Lock()
	defer retryBudget.mu.Unlock()
	if retryBudget.used >= retryBudget.maxRetries {
		return false
	}
	retryBudget.used++
	return true
}

// RetryBudgetStatus returns (used, max) for display purposes.
func RetryBudgetStatus() (int, int) {
	retryBudget.mu.Lock()
	defer retryBudget.mu.Unlock()
	return retryBudget.used, retryBudget.maxRetries
}

// ResetRetryBudget resets the session retry counter (e.g., on new session).
func ResetRetryBudget() {
	retryBudget.mu.Lock()
	defer retryBudget.mu.Unlock()
	retryBudget.used = 0
	retryBudget.maxRetries = MaxRetries()
}

// MaxRetries mirrors Claude Code's default retry count with Iroha-specific and
// Claude-compatible environment overrides.
func MaxRetries() int {
	for _, key := range []string{"IROHA_MAX_RETRIES", "CLAUDE_CODE_MAX_RETRIES"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n >= 0 {
			return n
		}
	}
	return defaultMaxRetries
}

// APITimeout mirrors Claude Code's API timeout default with environment
// overrides in milliseconds.
func APITimeout() time.Duration {
	for _, key := range []string{"IROHA_API_TIMEOUT_MS", "API_TIMEOUT_MS"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultAPITimeout
}

// parseRetryAfter extracts a delay in seconds from the Retry-After header.
// Returns 0 if the header is absent or unparseable.
func parseRetryAfter(resp *http.Response) float64 {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return 0
	}
	// Try integer seconds first.
	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		return float64(seconds)
	}
	// Try HTTP-date format.
	if t, err := http.ParseTime(retryAfter); err == nil {
		d := time.Until(t).Seconds()
		if d < 1.0 {
			return 1.0
		}
		return d
	}
	return 0
}

// IsRetryableHTTPStatus reports provider statuses safe to retry before output starts.
func IsRetryableHTTPStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

// IsRetryableTemporaryError classifies transient API/network failures.
func IsRetryableTemporaryError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "[1302]"),
		strings.Contains(msg, "rate limit"),
		strings.Contains(msg, "rate_limit"),
		strings.Contains(msg, "too many requests"),
		strings.Contains(msg, "throttl"),
		strings.Contains(msg, "overloaded"),
		strings.Contains(msg, "temporar"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "timed out"),
		strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "connection closed"),
		strings.Contains(msg, "dropped connection"),
		strings.Contains(msg, "unexpected eof"),
		strings.Contains(msg, "server error"),
		strings.Contains(msg, "error code 429"),
		strings.Contains(msg, "error code 500"),
		strings.Contains(msg, "error code 502"),
		strings.Contains(msg, "error code 503"),
		strings.Contains(msg, "error code 504"):
		return true
	}
	return false
}

// RetryDelay returns exponential backoff for a one-based retry attempt.
func RetryDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if raSec := parseRetryAfter(resp); raSec > 0 {
			return clampRetryDelay(time.Duration(raSec * float64(time.Second)))
		}
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(1<<uint(attempt-1)) * time.Second
	return clampRetryDelay(delay)
}

func clampRetryDelay(delay time.Duration) time.Duration {
	minDelay := minRetryDelay()
	if delay < minDelay {
		return minDelay
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func minRetryDelay() time.Duration {
	raw := strings.TrimSpace(os.Getenv("IROHA_MIN_RETRY_DELAY_MS"))
	if raw == "" {
		return time.Second
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return time.Second
	}
	return time.Duration(n) * time.Millisecond
}

// RetryNotice returns a user-visible retry status chunk.
func RetryNotice(reason string, attempt, maxRetries int, delay time.Duration) *model.LLMResponse {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "temporary API error"
	}
	return &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{Text: fmt.Sprintf("\n⚠️  [API Retry] %s — retrying in %.0fs · attempt %d/%d\n", reason, delay.Seconds(), attempt, maxRetries)},
			},
		},
		Partial:      true,
		TurnComplete: false,
	}
}

// DirectHTTPAdapter marks adapters that use direct local HTTP/SSE transport.
type DirectHTTPAdapter interface {
	DirectHTTPAdapter()
}

// budgetExhaustedError creates a descriptive error for retry budget exhaustion.
func budgetExhaustedError(modelName string, lastErr error) error {
	used, max := RetryBudgetStatus()
	return fmt.Errorf("LLM API (%s): retry budget exhausted (%d/%d retries used this session). Last error: %w", modelName, used, max, lastErr)
}

// BudgetExhaustedError exposes the shared retry-budget error for wrapper code
// outside this package.
func BudgetExhaustedError(modelName string, lastErr error) error {
	return budgetExhaustedError(modelName, lastErr)
}

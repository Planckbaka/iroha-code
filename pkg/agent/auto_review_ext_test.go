package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- callLLMForFileReview tests ---

func TestCallLLMForFileReview_SafeResponse(t *testing.T) {
	mock := &MockLLM{
		ResponseText: `{"safe": true, "reason": "Normal project file"}`,
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForFileReview(context.Background(), cfg, "file_write", "main.go", "package main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Safe {
		t.Errorf("expected safe=true, got safe=false, reason=%q", result.Reason)
	}
	if result.Reason != "Normal project file" {
		t.Errorf("expected reason 'Normal project file', got %q", result.Reason)
	}
}

func TestCallLLMForFileReview_UnsafeResponse(t *testing.T) {
	mock := &MockLLM{
		ResponseText: `{"safe": false, "reason": "Suspicious binary data"}`,
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForFileReview(context.Background(), cfg, "file_write", "payload.bin", "binary data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Safe {
		t.Error("expected safe=false, got safe=true")
	}
	if result.Reason != "Suspicious binary data" {
		t.Errorf("expected reason 'Suspicious binary data', got %q", result.Reason)
	}
}

func TestCallLLMForFileReview_LLMError(t *testing.T) {
	mock := &MockLLM{
		ResponseErr: context.DeadlineExceeded,
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForFileReview(context.Background(), cfg, "file_write", "test.bin", "data")
	if err == nil {
		t.Error("expected error from LLM failure")
	}
	if result.Safe {
		t.Error("expected zero-value result on error")
	}
}

func TestCallLLMForFileReview_InvalidJSON(t *testing.T) {
	mock := &MockLLM{
		ResponseText: `this is not JSON`,
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForFileReview(context.Background(), cfg, "file_write", "test.bin", "data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Safe {
		t.Error("expected safe=false on invalid JSON")
	}
	if !strings.Contains(result.Reason, "format error") {
		t.Errorf("expected format error in reason, got %q", result.Reason)
	}
}

func TestCallLLMForFileReview_JSONWrappedInCodeBlock(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "```json\n{\"safe\": true, \"reason\": \"Looks good\"}\n```",
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForFileReview(context.Background(), cfg, "file_write", "app.go", "code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Safe {
		t.Errorf("expected safe=true with code-wrapped JSON, got reason=%q", result.Reason)
	}
}

func TestCallLLMForFileReview_JSONInBackticks(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "```\n{\"safe\": false, \"reason\": \"Dangerous\"}\n```",
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForFileReview(context.Background(), cfg, "file_write", "evil.bin", "data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Safe {
		t.Error("expected safe=false")
	}
	if result.Reason != "Dangerous" {
		t.Errorf("expected reason 'Dangerous', got %q", result.Reason)
	}
}

// --- Additional ReviewFileOperation tests with LLM ---

func TestReviewFileOperation_WithLLMFileReview(t *testing.T) {
	mock := &MockLLM{
		ResponseText: `{"safe": true, "reason": "Normal project file"}`,
	}
	SetAutoReviewConfig(mock)
	defer func() { GlobalAutoReviewConfig = nil }()

	// Unknown extension triggers semantic review path
	result := ReviewFileOperation("file_write", "config.toml.bak", "some config data")
	// This goes through heuristic review first (unknown extension => needs semantic review),
	// then to callLLMForFileReview which returns safe=true from mock
	if !result.Safe {
		t.Errorf("expected LLM to approve, got safe=false, reason=%q", result.Reason)
	}
}

func TestReviewFileOperation_LLMFileReviewFailure(t *testing.T) {
	mock := &MockLLM{
		ResponseErr: context.DeadlineExceeded,
	}
	SetAutoReviewConfig(mock)
	defer func() { GlobalAutoReviewConfig = nil }()

	// Unknown extension triggers LLM review, which fails
	result := ReviewFileOperation("file_write", "data.bin", "binary data")
	if result.Safe {
		t.Error("expected safe=false when LLM fails, got safe=true")
	}
	if !strings.Contains(result.Reason, "LLM review failed") {
		t.Errorf("expected LLM review failure message, got %q", result.Reason)
	}
}

func TestReviewFileOperation_SafeExtensionBypassesLLM(t *testing.T) {
	// Even with LLM returning error, safe extensions should bypass
	mock := &MockLLM{
		ResponseErr: context.DeadlineExceeded,
	}
	SetAutoReviewConfig(mock)
	defer func() { GlobalAutoReviewConfig = nil }()

	result := ReviewFileOperation("file_write", "main.go", "package main")
	if !result.Safe {
		t.Errorf("safe extension should bypass LLM, got safe=false, reason=%q", result.Reason)
	}
}

// --- callLLMForReview extended tests ---

func TestCallLLMForReview_JSONCodeBlock(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "```json\n{\"safe\": true, \"reason\": \"Read-only command\"}\n```",
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForReview(context.Background(), cfg, "ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Safe {
		t.Errorf("expected safe=true from code-block-wrapped JSON, got reason=%q", result.Reason)
	}
}

func TestCallLLMForReview_PlainBacktickWrap(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "```\n{\"safe\": false, \"reason\": \"Dangerous\"}\n```",
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForReview(context.Background(), cfg, "rm -rf /")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Safe {
		t.Error("expected safe=false")
	}
}

func TestCallLLMForReview_InvalidJSON(t *testing.T) {
	mock := &MockLLM{
		ResponseText: "I think this command is safe.",
	}
	cfg := &autoReviewConfig{Model: mock}

	result, err := callLLMForReview(context.Background(), cfg, "some_cmd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Safe {
		t.Error("expected safe=false on invalid JSON response")
	}
	if !strings.Contains(result.Reason, "format error") {
		t.Errorf("expected format error in reason, got %q", result.Reason)
	}
}

func TestCallLLMForReview_ContextCancellation(t *testing.T) {
	mock := &MockLLM{
		ResponseErr: context.Canceled,
	}
	cfg := &autoReviewConfig{Model: mock}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	_, err := callLLMForReview(ctx, cfg, "ls")
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

package agent

import (
	"testing"
	"time"
)

// --- DetectStalled tests ---

func TestDetectStalled_NoTasks(t *testing.T) {
	bm := &BackgroundManager{
		tasks: make(map[string]*BackgroundTask),
	}
	stalled := bm.DetectStalled(5 * time.Minute)
	if len(stalled) != 0 {
		t.Errorf("expected no stalled tasks with empty manager, got %d", len(stalled))
	}
}

func TestDetectStalled_NoRunningTasks(t *testing.T) {
	bm := &BackgroundManager{
		tasks: map[string]*BackgroundTask{
			"task1": {
				ID:        "task1",
				Status:    "completed",
				StartedAt: time.Now().Add(-10 * time.Minute),
			},
			"task2": {
				ID:        "task2",
				Status:    "error",
				StartedAt: time.Now().Add(-10 * time.Minute),
			},
		},
	}
	stalled := bm.DetectStalled(5 * time.Minute)
	if len(stalled) != 0 {
		t.Errorf("expected no stalled tasks when none are running, got %d", len(stalled))
	}
}

func TestDetectStalled_RecentRunningTaskNotStalled(t *testing.T) {
	bm := &BackgroundManager{
		tasks: map[string]*BackgroundTask{
			"task1": {
				ID:        "task1",
				Status:    "running",
				StartedAt: time.Now().Add(-1 * time.Minute),
			},
		},
	}
	stalled := bm.DetectStalled(5 * time.Minute)
	if len(stalled) != 0 {
		t.Errorf("expected recent running task not to be stalled, got %d", len(stalled))
	}
}

func TestDetectStalled_OldRunningTaskIsStalled(t *testing.T) {
	bm := &BackgroundManager{
		tasks: map[string]*BackgroundTask{
			"task1": {
				ID:        "task1",
				Status:    "running",
				StartedAt: time.Now().Add(-10 * time.Minute),
			},
		},
	}
	stalled := bm.DetectStalled(5 * time.Minute)
	if len(stalled) != 1 {
		t.Fatalf("expected 1 stalled task, got %d", len(stalled))
	}
	if stalled[0] != "task1" {
		t.Errorf("expected stalled task ID 'task1', got %q", stalled[0])
	}
}

func TestDetectStalled_MixedTasks(t *testing.T) {
	bm := &BackgroundManager{
		tasks: map[string]*BackgroundTask{
			"completed": {
				ID:        "completed",
				Status:    "completed",
				StartedAt: time.Now().Add(-10 * time.Minute),
			},
			"stalled_old": {
				ID:        "stalled_old",
				Status:    "running",
				StartedAt: time.Now().Add(-10 * time.Minute),
			},
			"running_recent": {
				ID:        "running_recent",
				Status:    "running",
				StartedAt: time.Now().Add(-1 * time.Minute),
			},
			"stalled_very_old": {
				ID:        "stalled_very_old",
				Status:    "running",
				StartedAt: time.Now().Add(-1 * time.Hour),
			},
		},
	}
	stalled := bm.DetectStalled(5 * time.Minute)
	if len(stalled) != 2 {
		t.Fatalf("expected 2 stalled tasks, got %d: %v", len(stalled), stalled)
	}
	// Check that the correct tasks are identified
	stalledSet := make(map[string]bool)
	for _, id := range stalled {
		stalledSet[id] = true
	}
	if !stalledSet["stalled_old"] {
		t.Error("expected 'stalled_old' to be in stalled list")
	}
	if !stalledSet["stalled_very_old"] {
		t.Error("expected 'stalled_very_old' to be in stalled list")
	}
}

func TestDetectStalled_ExactThreshold(t *testing.T) {
	// Task started exactly at the threshold boundary
	bm := &BackgroundManager{
		tasks: map[string]*BackgroundTask{
			"task1": {
				ID:        "task1",
				Status:    "running",
				StartedAt: time.Now().Add(-5 * time.Minute),
			},
		},
	}
	// Using a threshold slightly less than 5 minutes, the task should be stalled
	stalled := bm.DetectStalled(4*time.Minute + 59*time.Second)
	if len(stalled) != 1 {
		t.Errorf("expected task at boundary to be stalled, got %d", len(stalled))
	}

	// Using exactly the same duration, it should NOT be stalled (> not >=)
	stalled2 := bm.DetectStalled(10 * time.Minute)
	if len(stalled2) != 0 {
		t.Errorf("expected task within threshold not to be stalled, got %d", len(stalled2))
	}
}

func TestDetectStalled_TimeoutTaskNotRunning(t *testing.T) {
	bm := &BackgroundManager{
		tasks: map[string]*BackgroundTask{
			"task1": {
				ID:        "task1",
				Status:    "timeout",
				StartedAt: time.Now().Add(-10 * time.Minute),
			},
		},
	}
	stalled := bm.DetectStalled(5 * time.Minute)
	if len(stalled) != 0 {
		t.Errorf("timeout task should not be detected as stalled, got %d", len(stalled))
	}
}

// --- preview tests ---

func TestPreview_ShortOutput(t *testing.T) {
	bm := &BackgroundManager{}
	result := bm.preview("hello world", 500)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestPreview_LongOutput(t *testing.T) {
	bm := &BackgroundManager{}
	longOutput := "a " + string(make([]byte, 600))
	result := bm.preview(longOutput, 500)
	if len(result) > 500 {
		t.Errorf("expected preview to be at most 500 chars, got %d", len(result))
	}
}

func TestPreview_CompactsWhitespace(t *testing.T) {
	bm := &BackgroundManager{}
	result := bm.preview("hello   world\n\nfoo  bar", 500)
	if result != "hello world foo bar" {
		t.Errorf("expected whitespace compaction, got %q", result)
	}
}

// --- truncateString tests ---

func TestTruncateString_Short(t *testing.T) {
	result := truncateString("hello", 10)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncateString_ExactLength(t *testing.T) {
	result := truncateString("hello", 5)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncateString_Long(t *testing.T) {
	result := truncateString("hello world", 5)
	if result != "hello..." {
		t.Errorf("expected 'hello...', got %q", result)
	}
}

func TestTruncateString_Empty(t *testing.T) {
	result := truncateString("", 5)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

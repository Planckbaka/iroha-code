package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// helper: create a Watchdog with a temp state file for isolation
func newTestWatchdog(t *testing.T, name string, budget int, window time.Duration) *Watchdog {
	t.Helper()
	tmpDir := t.TempDir()
	w := &Watchdog{
		teammateName:      name,
		crashBudget:       budget,
		crashWindow:       window,
		crashes:           make([]CrashRecord, 0),
		deadLetterQueue:   make([]IPCMessage, 0),
		heartbeatInterval: 10 * time.Second,
		stateFile:         filepath.Join(tmpDir, name+".json"),
	}
	return w
}

// ---------------------------------------------------------------------------
// NewWatchdog constructor
// ---------------------------------------------------------------------------

func TestNewWatchdog(t *testing.T) {
	w := NewWatchdog("test-agent", 5, time.Minute)
	if w == nil {
		t.Fatal("NewWatchdog returned nil")
	}
	if w.teammateName != "test-agent" {
		t.Errorf("teammateName = %q, want %q", w.teammateName, "test-agent")
	}
	if w.crashBudget != 5 {
		t.Errorf("crashBudget = %d, want 5", w.crashBudget)
	}
	if w.crashWindow != time.Minute {
		t.Errorf("crashWindow = %v, want 1m", w.crashWindow)
	}
	if w.heartbeatInterval != 10*time.Second {
		t.Errorf("heartbeatInterval = %v, want 10s", w.heartbeatInterval)
	}
	if w.stateFile == "" {
		t.Error("stateFile should not be empty")
	}
}

// ---------------------------------------------------------------------------
// RecordCrash budget enforcement
// ---------------------------------------------------------------------------

func TestWatchdog_RecordCrash_WithinBudget(t *testing.T) {
	w := newTestWatchdog(t, "agent", 3, time.Minute)

	for i := 0; i < 3; i++ {
		if !w.RecordCrash("crash reason") {
			t.Fatalf("crash %d should be within budget", i+1)
		}
	}

	// The 4th crash should exceed budget
	if w.RecordCrash("one too many") {
		t.Error("expected crash to exceed budget")
	}
}

func TestWatchdog_RecordCrash_WindowExpiry(t *testing.T) {
	w := newTestWatchdog(t, "agent", 1, 50*time.Millisecond)

	// First crash
	if !w.RecordCrash("first") {
		t.Fatal("first crash should be within budget")
	}

	// Wait for the window to expire
	time.Sleep(80 * time.Millisecond)

	// After window expires, the old crash should be pruned, so budget resets
	if !w.RecordCrash("after window") {
		t.Error("crash after window expiry should be within budget")
	}
}

func TestWatchdog_RecordCrash_ExceedsBudget(t *testing.T) {
	w := newTestWatchdog(t, "agent", 2, time.Minute)

	if !w.RecordCrash("crash1") {
		t.Fatal("crash 1 should be within budget")
	}
	if !w.RecordCrash("crash2") {
		t.Fatal("crash 2 should be within budget (count 2 <= budget 2)")
	}
	// 3rd crash: count 3 > budget 2, should exceed
	if w.RecordCrash("crash3") {
		t.Error("crash 3 should exceed budget (count 3 > budget 2)")
	}
}

// ---------------------------------------------------------------------------
// Checkpoint / Recover round-trip
// ---------------------------------------------------------------------------

func TestWatchdog_CheckpointRecover(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	state := map[string]any{
		"step":    42,
		"message": "hello",
	}

	if err := w.Checkpoint(state); err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}

	cp, err := w.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if cp == nil {
		t.Fatal("Recover returned nil, expected checkpoint data")
	}
	if cp.AgentName != "agent" {
		t.Errorf("AgentName = %q, want %q", cp.AgentName, "agent")
	}
	if cp.SavedAt.IsZero() {
		t.Error("SavedAt should not be zero")
	}

	// Verify the checkpoint data round-trips
	var restored map[string]any
	if err := json.Unmarshal(cp.Checkpoint, &restored); err != nil {
		t.Fatalf("failed to unmarshal checkpoint: %v", err)
	}
	if restored["step"].(float64) != 42 {
		t.Errorf("step = %v, want 42", restored["step"])
	}
	if restored["message"] != "hello" {
		t.Errorf("message = %v, want 'hello'", restored["message"])
	}
}

func TestWatchdog_Recover_NoFile(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	cp, err := w.Recover()
	if err != nil {
		t.Fatalf("Recover on missing file should not error: %v", err)
	}
	if cp != nil {
		t.Error("expected nil checkpoint when no file exists")
	}
}

func TestWatchdog_Recover_InvalidJSON(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	// Write garbage to the state file
	if err := os.WriteFile(w.stateFile, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	cp, err := w.Recover()
	if err == nil {
		t.Error("expected error from Recover with invalid JSON")
	}
	if cp != nil {
		t.Error("expected nil checkpoint on parse error")
	}
}

// ---------------------------------------------------------------------------
// Dead letter queue
// ---------------------------------------------------------------------------

func TestWatchdog_EnqueueDrainDeadLetters(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	msgs := []IPCMessage{
		{Type: "task_assign", From: "parent", To: "agent", ID: "msg-1", Payload: json.RawMessage(`{"x":1}`)},
		{Type: "message", From: "parent", To: "agent", ID: "msg-2", Payload: json.RawMessage(`{"x":2}`)},
	}

	for _, m := range msgs {
		w.EnqueueDeadLetter(m)
	}

	// Verify persisted to disk
	dlPath := w.deadLetterPath()
	if _, err := os.Stat(dlPath); os.IsNotExist(err) {
		t.Error("dead letters should be persisted to disk")
	}

	// Drain should return all messages
	drained := w.DrainDeadLetters()
	if len(drained) != 2 {
		t.Fatalf("expected 2 drained messages, got %d", len(drained))
	}
	if drained[0].ID != "msg-1" {
		t.Errorf("drained[0].ID = %q, want 'msg-1'", drained[0].ID)
	}
	if drained[1].ID != "msg-2" {
		t.Errorf("drained[1].ID = %q, want 'msg-2'", drained[1].ID)
	}

	// After draining, file should be removed
	if _, err := os.Stat(dlPath); !os.IsNotExist(err) {
		t.Error("dead letter file should be removed after drain")
	}

	// Second drain should be empty
	drained2 := w.DrainDeadLetters()
	if len(drained2) != 0 {
		t.Errorf("expected 0 after drain, got %d", len(drained2))
	}
}

func TestWatchdog_DeadLetterPersistenceRoundTrip(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	// Enqueue a message
	w.EnqueueDeadLetter(IPCMessage{
		Type:    "message",
		From:    "parent",
		To:      "agent",
		ID:      "persist-test",
		Payload: json.RawMessage(`{"data":"hello"}`),
	})

	// Create a new watchdog with the same state file to simulate restart
	w2 := &Watchdog{
		teammateName:    "agent",
		crashBudget:     5,
		crashWindow:     time.Minute,
		crashes:         make([]CrashRecord, 0),
		deadLetterQueue: make([]IPCMessage, 0),
		stateFile:       w.stateFile,
	}

	// Load dead letters from disk
	w2.loadDeadLetters()

	if len(w2.deadLetterQueue) != 1 {
		t.Fatalf("expected 1 loaded dead letter, got %d", len(w2.deadLetterQueue))
	}
	if w2.deadLetterQueue[0].ID != "persist-test" {
		t.Errorf("loaded ID = %q, want 'persist-test'", w2.deadLetterQueue[0].ID)
	}
}

func TestWatchdog_DeadLetterEmptyQueue(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	// Calling persistDeadLettersLocked with empty queue should be a no-op
	w.persistDeadLettersLocked()

	dlPath := w.deadLetterPath()
	if _, err := os.Stat(dlPath); err == nil {
		t.Error("no file should be written for empty dead letter queue")
	}
}

// ---------------------------------------------------------------------------
// IsRunning state
// ---------------------------------------------------------------------------

func TestWatchdog_IsRunning(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	if w.IsRunning() {
		t.Error("expected IsRunning=false initially")
	}

	// Simulate process running state
	w.mu.Lock()
	w.processRunning = true
	w.mu.Unlock()

	if !w.IsRunning() {
		t.Error("expected IsRunning=true after setting processRunning")
	}
}

// ---------------------------------------------------------------------------
// deadLetterPath
// ---------------------------------------------------------------------------

func TestWatchdog_DeadLetterPath(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	expected := w.stateFile + ".deadletters"
	if got := w.deadLetterPath(); got != expected {
		t.Errorf("deadLetterPath = %q, want %q", got, expected)
	}
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

func TestWatchdog_ConcurrentRecordCrash(t *testing.T) {
	w := newTestWatchdog(t, "agent", 100, time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.RecordCrash("concurrent crash")
		}()
	}
	wg.Wait()

	w.mu.Lock()
	count := len(w.crashes)
	w.mu.Unlock()

	if count != 50 {
		t.Errorf("expected 50 crashes, got %d", count)
	}
}

func TestWatchdog_ConcurrentDeadLetters(t *testing.T) {
	w := newTestWatchdog(t, "agent", 5, time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.EnqueueDeadLetter(IPCMessage{
				Type: "msg",
				ID:   "concurrent-msg",
				Payload: json.RawMessage(`{"i":0}`),
			})
		}(i)
	}
	wg.Wait()

	drained := w.DrainDeadLetters()
	if len(drained) != 20 {
		t.Errorf("expected 20 dead letters, got %d", len(drained))
	}
}

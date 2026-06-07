package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newTestTeamManager(t *testing.T) *TeamManager {
	tmpDir, err := os.MkdirTemp("/tmp", "tp-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	tm := &TeamManager{
		teamDir:     tmpDir,
		teammates:   make(map[string]*Teammate),
		activeLoops: make(map[string]chan struct{}),
		watchdogs:   make(map[string]*Watchdog),
		cancelFuncs: make(map[string]context.CancelFunc),
	}

	// Create inbox directory so AppendToInbox works
	if err := os.MkdirAll(filepath.Join(tmpDir, "inbox"), 0755); err != nil {
		t.Fatal(err)
	}

	// Register a test teammate
	tm.teammates["test-agent"] = &Teammate{
		Name:       "test-agent",
		Role:       "Tester",
		Status:     "idle",
		LastActive: time.Now(),
	}

	return tm
}

func TestIntegration_TeamProcess_StartStopLoop(t *testing.T) {
	tm := newTestTeamManager(t)

	var processed atomic.Int32
	tm.ProcessMessage = func(teammate *Teammate, msg TeamMessage) (string, error) {
		processed.Add(1)
		return "processed: " + msg.Content, nil
	}

	// Write a message to inbox
	msg := TeamMessage{
		Sender:    "coordinator",
		Content:   "hello teammate",
		Timestamp: float64(time.Now().Unix()),
	}
	if err := tm.AppendToInbox("test-agent", msg); err != nil {
		t.Fatal(err)
	}

	// Start loop
	if err := tm.StartTeammateLoop("test-agent"); err != nil {
		t.Fatalf("StartTeammateLoop failed: %v", err)
	}

	// The loop ticks every 2s and calls GetTeammate which calls LoadConfig.
	// We need to save config first so GetTeammate succeeds.
	if err := tm.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Wait for processing (loop ticks every 2 seconds)
	deadline := time.After(6 * time.Second)
	for processed.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for message processing")
		case <-time.After(100 * time.Millisecond):
		}
	}

	// Stop loop
	tm.StopTeammateLoop("test-agent")

	if processed.Load() != 1 {
		t.Errorf("expected 1 processed message, got %d", processed.Load())
	}
}

func TestIntegration_TeamProcess_StartLoopIdempotent(t *testing.T) {
	tm := newTestTeamManager(t)

	// Starting twice should not error
	if err := tm.StartTeammateLoop("test-agent"); err != nil {
		t.Fatalf("first StartTeammateLoop failed: %v", err)
	}
	if err := tm.StartTeammateLoop("test-agent"); err != nil {
		t.Fatalf("second StartTeammateLoop failed: %v", err)
	}

	// Should only have one active loop
	tm.mu.RLock()
	count := len(tm.activeLoops)
	tm.mu.RUnlock()
	if count != 1 {
		t.Errorf("expected 1 active loop, got %d", count)
	}

	tm.StopTeammateLoop("test-agent")
}

func TestIntegration_TeamProcess_StopLoopNotActive(t *testing.T) {
	tm := newTestTeamManager(t)

	// Stopping non-existent teammate should be a no-op, not panic
	tm.StopTeammateLoop("nonexistent-agent")
}

func TestIntegration_TeamProcess_EnableProcessIsolation(t *testing.T) {
	// Use a short temp path to avoid Unix socket path length limits
	tmpDir, err := os.MkdirTemp("/tmp", "tp-iso-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tm := &TeamManager{
		teamDir:     tmpDir,
		teammates:   make(map[string]*Teammate),
		activeLoops: make(map[string]chan struct{}),
		watchdogs:   make(map[string]*Watchdog),
		cancelFuncs: make(map[string]context.CancelFunc),
	}
	os.MkdirAll(filepath.Join(tmpDir, "inbox"), 0755)

	// Use the test binary itself as the "binary" (it won't actually be spawned)
	err = tm.EnableProcessIsolation(os.Args[0])
	if err != nil {
		t.Fatalf("EnableProcessIsolation failed: %v", err)
	}

	if !tm.isolationMode {
		t.Error("expected isolationMode to be true")
	}
	if tm.binaryPath == "" {
		t.Error("expected binaryPath to be set")
	}
	if tm.ipcBridge == nil {
		t.Error("expected ipcBridge to be created")
	}

	// Socket dir should exist
	socketDir := filepath.Join(tm.teamDir, "sockets")
	if _, err := os.Stat(socketDir); os.IsNotExist(err) {
		t.Error("expected socket directory to be created")
	}

	// Cleanup
	tm.ipcBridge.Close()
}

func TestIntegration_TeamProcess_StartProcessNoBinary(t *testing.T) {
	tm := newTestTeamManager(t)
	// Don't call EnableProcessIsolation, so binaryPath is empty

	err := tm.StartTeammateProcess(context.Background(), "test-agent")
	if err == nil {
		t.Error("expected error when starting process without binary path")
	}
}

func TestIntegration_TeamProcess_StopProcessCleansUp(t *testing.T) {
	tm := newTestTeamManager(t)

	// Set up state manually to simulate a running process
	_, cancel := context.WithCancel(context.Background())
	stopChan := make(chan struct{})
	tm.activeLoops["test-agent"] = stopChan
	tm.cancelFuncs["test-agent"] = cancel
	tm.watchdogs["test-agent"] = NewWatchdog("test-agent", 3, 60*time.Second)

	tm.StopTeammateProcess("test-agent")

	// Verify cleanup
	tm.mu.RLock()
	_, hasLoop := tm.activeLoops["test-agent"]
	_, hasCancel := tm.cancelFuncs["test-agent"]
	_, hasWatchdog := tm.watchdogs["test-agent"]
	tm.mu.RUnlock()

	if hasLoop {
		t.Error("expected activeLoop to be removed")
	}
	if hasCancel {
		t.Error("expected cancelFunc to be removed")
	}
	if hasWatchdog {
		t.Error("expected watchdog to be removed")
	}

	// Verify teammate status set to offline
	if tm.teammates["test-agent"].Status != "offline" {
		t.Errorf("expected status 'offline', got %q", tm.teammates["test-agent"].Status)
	}
}

func TestIntegration_TeamProcess_HandleIPCMessageTypes(t *testing.T) {
	tm := newTestTeamManager(t)

	// Test "message" type
	payload, _ := json.Marshal(TeamMessage{
		Sender:  "test-agent",
		Content: "hello",
	})
	tm.handleIPCMessage(IPCMessage{
		Type:    "message",
		From:    "test-agent",
		Payload: payload,
	})

	// Test "heartbeat" type — should update last active
	oldActive := tm.teammates["test-agent"].LastActive
	tm.handleIPCMessage(IPCMessage{
		Type: "heartbeat",
		From: "test-agent",
	})
	if !tm.teammates["test-agent"].LastActive.After(oldActive) {
		t.Error("expected LastActive to be updated after heartbeat")
	}

	// Test "shutdown" type — should not panic
	tm.handleIPCMessage(IPCMessage{
		Type: "shutdown",
		From: "test-agent",
	})

	// Test "task_complete" type
	taskPayload, _ := json.Marshal(TeamMessage{
		Sender:  "test-agent",
		Content: "task done",
	})
	tm.handleIPCMessage(IPCMessage{
		Type:    "task_complete",
		From:    "test-agent",
		Payload: taskPayload,
	})
}

func TestIntegration_TeamProcess_HandleIPCMessageInvalidPayload(t *testing.T) {
	tm := newTestTeamManager(t)

	// Invalid JSON payload should not panic
	tm.handleIPCMessage(IPCMessage{
		Type:    "message",
		From:    "test-agent",
		Payload: json.RawMessage(`{invalid json`),
	})

	// Empty sender in valid JSON — should not append
	payload, _ := json.Marshal(TeamMessage{
		Sender:  "",
		Content: "orphan",
	})
	tm.handleIPCMessage(IPCMessage{
		Type:    "message",
		From:    "test-agent",
		Payload: payload,
	})
}

func TestIntegration_TeamProcess_HeartbeatChecker(t *testing.T) {
	tm := newTestTeamManager(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// heartbeatChecker should exit when context is cancelled
	done := make(chan struct{})
	go func() {
		tm.heartbeatChecker(ctx, "test-agent")
		close(done)
	}()

	// Cancel after a brief wait
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Success — heartbeatChecker exited
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatChecker did not exit on context cancel")
	}
}

func TestIntegration_TeamProcess_StopTeammateLoopIsolation(t *testing.T) {
	tm := newTestTeamManager(t)

	// Manually set isolation mode
	tm.mu.Lock()
	tm.isolationMode = true
	tm.mu.Unlock()

	// Set up state for process-based stop
	_, cancel := context.WithCancel(context.Background())
	tm.activeLoops["test-agent"] = make(chan struct{})
	tm.cancelFuncs["test-agent"] = cancel
	tm.watchdogs["test-agent"] = NewWatchdog("test-agent", 3, 60*time.Second)

	// StopTeammateLoop should route to StopTeammateProcess when isolationMode=true
	tm.StopTeammateLoop("test-agent")

	tm.mu.RLock()
	_, hasLoop := tm.activeLoops["test-agent"]
	tm.mu.RUnlock()
	if hasLoop {
		t.Error("expected activeLoop to be removed")
	}
}

func TestIntegration_TeamProcess_ResolveTeammateSocketDir(t *testing.T) {
	tm := newTestTeamManager(t)

	socketDir := tm.ResolveTeammateSocketDir()
	expected := filepath.Join(tm.teamDir, "sockets")
	if socketDir != expected {
		t.Errorf("expected %q, got %q", expected, socketDir)
	}
}

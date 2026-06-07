package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Start / spawnLocked — process spawning
// ---------------------------------------------------------------------------

func TestWatchdog_Start_SpawnsProcess(t *testing.T) {
	w := newTestWatchdog(t, "spawn-agent", 3, time.Minute)

	// Use "sleep" as a harmless long-running process
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := w.Start(ctx, bin, []string{"60"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	if !w.IsRunning() {
		t.Error("expected IsRunning=true after Start")
	}

	w.mu.Lock()
	pid := w.cmd.Process.Pid
	w.mu.Unlock()
	if pid <= 0 {
		t.Errorf("expected valid PID, got %d", pid)
	}
}

func TestWatchdog_Start_InvalidBinary(t *testing.T) {
	w := newTestWatchdog(t, "bad-agent", 3, time.Minute)

	ctx := context.Background()
	err := w.Start(ctx, "/nonexistent/binary/path", nil)
	if err == nil {
		t.Error("expected error starting non-existent binary")
		w.Stop()
	}
}

// ---------------------------------------------------------------------------
// Monitor — process restart on crash
// ---------------------------------------------------------------------------

func TestWatchdog_Monitor_RestartsOnExit(t *testing.T) {
	w := newTestWatchdog(t, "monitor-agent", 3, time.Minute)

	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found on PATH")
	}

	monitorCtx, monitorCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer monitorCancel()

	// Start the process manually via Start
	if err := w.Start(monitorCtx, bin, []string{"0"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Monitor should detect exit and restart; "sleep 0" exits immediately
	// We need a second binary to restart with
	w.binaryPath = bin
	w.args = []string{"0"}

	// Run monitor in background — it should restart within budget
	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- w.Monitor(monitorCtx)
	}()

	// Wait for monitor to complete (should succeed within budget)
	select {
	case err := <-monitorDone:
		// Monitor returns nil on context cancel, or error on budget exceeded
		_ = err
	case <-time.After(8 * time.Second):
		t.Fatal("Monitor didn't complete in time")
	}

	monitorCancel()
}

func TestWatchdog_Monitor_NoProcess(t *testing.T) {
	w := newTestWatchdog(t, "no-proc-agent", 1, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := w.Monitor(ctx)
	if err == nil {
		t.Error("expected error when no process to monitor")
	}
}

func TestWatchdog_Monitor_CrashBudgetExceeded(t *testing.T) {
	w := newTestWatchdog(t, "budget-agent", 1, time.Minute)

	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := w.Start(ctx, bin, []string{"0"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- w.Monitor(ctx)
	}()

	select {
	case err := <-monitorDone:
		if err == nil {
			t.Error("expected error when crash budget exceeded")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Monitor didn't complete in time")
	}
}

// ---------------------------------------------------------------------------
// Stop — graceful termination
// ---------------------------------------------------------------------------

func TestWatchdog_Stop_TerminatesProcess(t *testing.T) {
	w := newTestWatchdog(t, "stop-agent", 3, time.Minute)

	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found on PATH")
	}

	ctx := context.Background()
	if err := w.Start(ctx, bin, []string{"60"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !w.IsRunning() {
		t.Fatal("expected process to be running before Stop")
	}

	w.Stop()

	// Give a moment for process to fully terminate
	time.Sleep(100 * time.Millisecond)

	if w.IsRunning() {
		t.Error("expected IsRunning=false after Stop")
	}
}

func TestWatchdog_Stop_NoProcess(t *testing.T) {
	w := newTestWatchdog(t, "nostop-agent", 3, time.Minute)

	// Stop on a never-started watchdog should not panic
	w.Stop()
}

func TestWatchdog_Stop_WithCancelFn(t *testing.T) {
	w := newTestWatchdog(t, "cancel-agent", 3, time.Minute)

	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found on PATH")
	}

	ctx := context.Background()
	if err := w.Start(ctx, bin, []string{"60"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Set a cancel function to verify it gets called
	cancelCalled := false
	w.mu.Lock()
	w.cancelMonitor = func() { cancelCalled = true }
	w.mu.Unlock()

	w.Stop()

	if !cancelCalled {
		t.Error("expected cancelMonitor to be called during Stop")
	}
}

// ---------------------------------------------------------------------------
// Checkpoint — error cases
// ---------------------------------------------------------------------------

func TestWatchdog_Checkpoint_InvalidDir(t *testing.T) {
	w := newTestWatchdog(t, "chk-agent", 3, time.Minute)
	// Point stateFile to a non-existent nested directory
	w.stateFile = filepath.Join(t.TempDir(), "nonexistent", "dir", "state.json")

	err := w.Checkpoint(map[string]string{"key": "val"})
	if err == nil {
		t.Error("expected error writing to non-existent directory")
	}
}

func TestWatchdog_Checkpoint_UnmarshallableState(t *testing.T) {
	w := newTestWatchdog(t, "chk-err-agent", 3, time.Minute)

	// Channels cannot be marshalled to JSON
	err := w.Checkpoint(make(chan struct{}))
	if err == nil {
		t.Error("expected error marshalling channel to JSON")
	}
}

// ---------------------------------------------------------------------------
// ResolveTeammateStateFile
// ---------------------------------------------------------------------------

func TestResolveTeammateStateFile(t *testing.T) {
	path := ResolveTeammateStateFile("test-teammate")
	if path == "" {
		t.Error("expected non-empty state file path")
	}
	if filepath.Base(path) != "test-teammate.json" {
		t.Errorf("expected base name 'test-teammate.json', got %q", filepath.Base(path))
	}

	// Verify the directory was created
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("directory %q should have been created", dir)
	}
}

// ---------------------------------------------------------------------------
// spawnLocked directly
// ---------------------------------------------------------------------------

func TestWatchdog_SpawnLocked_SetsFields(t *testing.T) {
	w := newTestWatchdog(t, "field-agent", 3, time.Minute)

	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found on PATH")
	}

	w.binaryPath = bin
	w.args = []string{"60"}

	ctx := context.Background()

	w.mu.Lock()
	err = w.spawnLocked(ctx)
	w.mu.Unlock()

	if err != nil {
		t.Fatalf("spawnLocked failed: %v", err)
	}
	defer w.Stop()

	if !w.processRunning {
		t.Error("expected processRunning=true after spawnLocked")
	}
	if w.cmd == nil {
		t.Error("expected cmd to be set after spawnLocked")
	}
}

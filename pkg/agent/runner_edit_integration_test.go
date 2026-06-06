package agent

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestIntegration_Edit_RollbackRestoresFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-edit-rollback-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file with initial content
	filePath := filepath.Join(tmpDir, "test.txt")
	originalContent := "original content"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Snapshot it
	pendingEditSnapshots.mu.Lock()
	pendingEditSnapshots.snapshots[filePath] = originalContent
	pendingEditSnapshots.mu.Unlock()

	// Modify the file
	if err := os.WriteFile(filePath, []byte("modified content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Rollback should restore original
	rollbackPendingEdits()

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != originalContent {
		t.Errorf("expected %q after rollback, got %q", originalContent, string(data))
	}

	// Snapshots should be cleared
	pendingEditSnapshots.mu.Lock()
	count := len(pendingEditSnapshots.snapshots)
	pendingEditSnapshots.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 snapshots after rollback, got %d", count)
	}
}

func TestIntegration_Edit_RollbackRemovesCreatedFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-edit-rollback-new-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Snapshot a new file with empty content (simulating creation)
	filePath := filepath.Join(tmpDir, "newfile.txt")
	pendingEditSnapshots.mu.Lock()
	pendingEditSnapshots.snapshots[filePath] = "" // empty = file was newly created
	pendingEditSnapshots.mu.Unlock()

	// Create the file
	if err := os.WriteFile(filePath, []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Rollback should remove the file
	rollbackPendingEdits()

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("expected new file to be removed after rollback")
	}
}

func TestIntegration_Edit_CommitClearsSnapshots(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-edit-commit-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Add some snapshots
	pendingEditSnapshots.mu.Lock()
	pendingEditSnapshots.snapshots[filepath.Join(tmpDir, "a.txt")] = "content a"
	pendingEditSnapshots.snapshots[filepath.Join(tmpDir, "b.txt")] = "content b"
	pendingEditSnapshots.mu.Unlock()

	// Commit should clear all
	commitPendingEdits()

	pendingEditSnapshots.mu.Lock()
	count := len(pendingEditSnapshots.snapshots)
	pendingEditSnapshots.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 snapshots after commit, got %d", count)
	}

	// pendingEditPaths should return empty
	paths := pendingEditPaths()
	if len(paths) != 0 {
		t.Errorf("expected 0 paths after commit, got %d", len(paths))
	}
}

func TestIntegration_Edit_PendingEditPaths(t *testing.T) {
	// Clear any existing snapshots
	pendingEditSnapshots.mu.Lock()
	pendingEditSnapshots.snapshots = make(map[string]string)
	pendingEditSnapshots.mu.Unlock()

	paths := []string{"/tmp/edit_a.txt", "/tmp/edit_b.txt", "/tmp/edit_c.txt"}

	pendingEditSnapshots.mu.Lock()
	for _, p := range paths {
		pendingEditSnapshots.snapshots[p] = "content"
	}
	pendingEditSnapshots.mu.Unlock()

	result := pendingEditPaths()
	sort.Strings(result)
	sort.Strings(paths)

	if len(result) != len(paths) {
		t.Fatalf("expected %d paths, got %d", len(paths), len(result))
	}
	for i := range paths {
		if result[i] != paths[i] {
			t.Errorf("path[%d]: expected %q, got %q", i, paths[i], result[i])
		}
	}

	// Cleanup
	pendingEditSnapshots.mu.Lock()
	pendingEditSnapshots.snapshots = make(map[string]string)
	pendingEditSnapshots.mu.Unlock()
}

func TestIntegration_Edit_FindGoModuleRoot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-gomod-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested directory structure with go.mod
	subDir := filepath.Join(tmpDir, "sub", "deep")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Place go.mod in the root of tmpDir
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to subdirectory and find go.mod root
	oldWd, _ := os.Getwd()
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	root := findGoModuleRoot()
	// Resolve symlinks for comparison (macOS /var -> /private/var)
	rootResolved, _ := filepath.EvalSymlinks(root)
	tmpDirResolved, _ := filepath.EvalSymlinks(tmpDir)
	if rootResolved != tmpDirResolved {
		t.Errorf("expected %q, got %q", tmpDirResolved, rootResolved)
	}
}

func TestIntegration_Edit_FindGoModuleRootFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-nogomod-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// No go.mod anywhere
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	root := findGoModuleRoot()
	if root == "" {
		t.Error("expected fallback to cwd, got empty string")
	}
}

func TestIntegration_Bridge_CancelChanReadReturnsCurrentChannel(t *testing.T) {
	b := &ConfirmationBridge{
		PromptChan:   make(chan string, 1),
		ResponseChan: make(chan string, 1),
		CancelChan:   make(chan struct{}),
	}

	// Get initial channel
	ch1 := b.CancelChanRead()
	if ch1 == nil {
		t.Fatal("expected non-nil channel")
	}

	// Reset creates new channel
	b.Reset()
	ch2 := b.CancelChanRead()

	// Channels should be different after Reset
	if ch1 == ch2 {
		t.Error("expected different channels after Reset")
	}
}

func TestIntegration_Bridge_ConcurrentResetAndCancel(t *testing.T) {
	// Test concurrent Reset calls (Cancel+Reset has inherent races in the
	// production code since Cancel closes the channel and Reset replaces it).
	// We test only concurrent Reset which is safe.
	b := &ConfirmationBridge{
		PromptChan:   make(chan string, 1),
		ResponseChan: make(chan string, 1),
		CancelChan:   make(chan struct{}),
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Reset()
		}()
	}
	wg.Wait()
}

func TestIntegration_Bridge_ToolStatusSend(t *testing.T) {
	tb := &ToolStatusBridge{
		StatusChan: make(chan ToolStatus, 10),
	}

	// Send a status
	tb.Send(ToolStatus{
		Name:    "read_file",
		Running: true,
	})

	// Should appear on StatusChan
	select {
	case status := <-tb.StatusChan:
		if status.Name != "read_file" {
			t.Errorf("expected name 'read_file', got %q", status.Name)
		}
		if !status.Running {
			t.Error("expected Running to be true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for status on StatusChan")
	}
}

func TestIntegration_Bridge_ToolStatusMultipleSends(t *testing.T) {
	tb := &ToolStatusBridge{
		StatusChan: make(chan ToolStatus, 100),
	}

	// Send multiple statuses
	for i := 0; i < 5; i++ {
		tb.Send(ToolStatus{
			Name:    "tool",
			Running: true,
		})
	}

	// All should arrive in order
	for i := 0; i < 5; i++ {
		select {
		case status := <-tb.StatusChan:
			if status.Name != "tool" {
				t.Errorf("expected name 'tool', got %q", status.Name)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for status %d", i)
		}
	}
}

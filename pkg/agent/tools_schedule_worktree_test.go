package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Schedule handler tests

func TestScheduleCreateHandler_Success(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	result, err := ScheduleCreateHandler(nil, ScheduleCreateArgs{
		CronExpr:  "*/5 * * * *",
		Prompt:    "run tests",
		Recurring: true,
		Durable:   false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
	if !strings.Contains(result.Message, "Created task") {
		t.Errorf("expected message to contain 'Created task', got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "recurring") {
		t.Errorf("expected message to contain 'recurring', got: %s", result.Message)
	}
}

func TestScheduleCreateHandler_OneShot(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	result, err := ScheduleCreateHandler(nil, ScheduleCreateArgs{
		CronExpr:  "0 9 * * 1",
		Prompt:    "weekly report",
		Recurring: false,
		Durable:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Message, "one-shot") {
		t.Errorf("expected message to contain 'one-shot', got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "durable") {
		t.Errorf("expected message to contain 'durable', got: %s", result.Message)
	}
}

func TestScheduleCreateHandler_InvalidCronExpr(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	_, err := ScheduleCreateHandler(nil, ScheduleCreateArgs{
		CronExpr: "bad expr",
		Prompt:   "test",
	})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
	if !strings.Contains(err.Error(), "invalid cron expression") {
		t.Errorf("expected error about invalid cron expression, got: %v", err)
	}
}

func TestScheduleCreateHandler_TooManyFields(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	_, err := ScheduleCreateHandler(nil, ScheduleCreateArgs{
		CronExpr: "* * * * * *",
		Prompt:   "test",
	})
	if err == nil {
		t.Fatal("expected error for cron with too many fields")
	}
}

func TestScheduleListHandler_Empty(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	result, err := ScheduleListHandler(nil, ScheduleListArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ActiveTasks != "No scheduled tasks." {
		t.Errorf("expected 'No scheduled tasks.', got: %s", result.ActiveTasks)
	}
}

func TestScheduleListHandler_WithTasks(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	// Create a task first
	_, _ = ScheduleCreateHandler(nil, ScheduleCreateArgs{
		CronExpr:  "*/5 * * * *",
		Prompt:    "run tests",
		Recurring: true,
	})

	result, err := ScheduleListHandler(nil, ScheduleListArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ActiveTasks == "No scheduled tasks." {
		t.Error("expected tasks to be listed, got 'No scheduled tasks.'")
	}
	if !strings.Contains(result.ActiveTasks, "*/5 * * * *") {
		t.Errorf("expected listing to contain cron expression, got: %s", result.ActiveTasks)
	}
	if !strings.Contains(result.ActiveTasks, "run tests") {
		t.Errorf("expected listing to contain prompt, got: %s", result.ActiveTasks)
	}
}

func TestScheduleDeleteHandler_Success(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	// Create a task
	createResult, err := ScheduleCreateHandler(nil, ScheduleCreateArgs{
		CronExpr: "0 * * * *",
		Prompt:   "hourly check",
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Extract task ID from message ("Created task XXXXX ...")
	parts := strings.Split(createResult.Message, " ")
	if len(parts) < 3 {
		t.Fatalf("unexpected message format: %s", createResult.Message)
	}
	taskID := parts[2]

	// Delete the task
	delResult, err := ScheduleDeleteHandler(nil, ScheduleDeleteArgs{TaskID: taskID})
	if err != nil {
		t.Fatalf("unexpected error deleting task: %v", err)
	}
	if !strings.Contains(delResult.Message, "Deleted task") {
		t.Errorf("expected 'Deleted task' in message, got: %s", delResult.Message)
	}

	// Verify it's gone
	listResult, _ := ScheduleListHandler(nil, ScheduleListArgs{})
	if listResult.ActiveTasks != "No scheduled tasks." {
		t.Errorf("expected no tasks after deletion, got: %s", listResult.ActiveTasks)
	}
}

func TestScheduleDeleteHandler_NotFound(t *testing.T) {
	origScheduler := GlobalCronScheduler
	defer func() { GlobalCronScheduler = origScheduler }()

	GlobalCronScheduler = NewCronScheduler()

	_, err := ScheduleDeleteHandler(nil, ScheduleDeleteArgs{TaskID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error when deleting nonexistent task")
	}
	if !strings.Contains(err.Error(), "task not found") {
		t.Errorf("expected 'task not found' in error, got: %v", err)
	}
}

// Worktree handler tests

func newTestWorktreeManager(t *testing.T) (*WorktreeManager, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "go-claude-worktree-handler-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	wtDir := filepath.Join(tempDir, ".worktrees")
	_ = os.MkdirAll(wtDir, 0755)

	wm := &WorktreeManager{
		worktreesDir: wtDir,
		indexPath:    filepath.Join(wtDir, "index.json"),
		eventsPath:   filepath.Join(wtDir, "events.jsonl"),
		entries:      make(map[string]*WorktreeEntry),
	}
	wm.GitCommand = func(args ...string) ([]byte, error) {
		return []byte("mock git success"), nil
	}
	return wm, tempDir
}

func TestWorktreeCreateHandler_Success(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	// Override task manager dir so task lookups don't fail
	origTasksDir := GlobalTaskManager.tasksDir
	GlobalTaskManager.tasksDir = filepath.Join(tempDir, ".tasks")
	_ = os.MkdirAll(GlobalTaskManager.tasksDir, 0755)
	defer func() { GlobalTaskManager.tasksDir = origTasksDir }()

	result, err := WorktreeCreateHandler(nil, WorktreeCreateArgs{
		Name:   "feat-auth",
		TaskID: "t1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.Path == "" {
		t.Error("expected non-empty path")
	}
	if result.Branch != "wt/feat-auth" {
		t.Errorf("expected branch 'wt/feat-auth', got: %s", result.Branch)
	}
}

func TestWorktreeCreateHandler_EmptyName(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	_, err := WorktreeCreateHandler(nil, WorktreeCreateArgs{
		Name: "",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "worktree name is required") {
		t.Errorf("expected 'worktree name is required' in error, got: %v", err)
	}
}

func TestWorktreeCreateHandler_Duplicate(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	// Create first
	_, _ = WorktreeCreateHandler(nil, WorktreeCreateArgs{Name: "dup-wt"})

	// Create duplicate should fail
	_, err := WorktreeCreateHandler(nil, WorktreeCreateArgs{Name: "dup-wt"})
	if err == nil {
		t.Fatal("expected error for duplicate worktree")
	}
	if !strings.Contains(err.Error(), "already active") {
		t.Errorf("expected 'already active' in error, got: %v", err)
	}
}

func TestWorktreeCreateHandler_GitFailure(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	wm.GitCommand = func(args ...string) ([]byte, error) {
		return nil, os.ErrPermission
	}
	GlobalWorktreeManager = wm

	_, err := WorktreeCreateHandler(nil, WorktreeCreateArgs{Name: "fail-wt"})
	if err == nil {
		t.Fatal("expected error when git command fails")
	}
}

func TestWorktreeListHandler_Empty(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	result, err := WorktreeListHandler(nil, WorktreeListArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Worktrees) != 0 {
		t.Errorf("expected empty list, got %d entries", len(result.Worktrees))
	}
}

func TestWorktreeListHandler_WithEntries(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	// Seed an entry directly
	wm.entries["test-wt"] = &WorktreeEntry{
		Name:   "test-wt",
		Path:   "/some/path",
		Branch: "wt/test-wt",
		Status: "active",
	}

	result, err := WorktreeListHandler(nil, WorktreeListArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Worktrees) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(result.Worktrees))
	}
	if result.Worktrees[0].Name != "test-wt" {
		t.Errorf("expected name 'test-wt', got: %s", result.Worktrees[0].Name)
	}
}

func TestWorktreeStatusHandler_Found(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	wm.entries["status-wt"] = &WorktreeEntry{
		Name:   "status-wt",
		Status: "active",
		TaskID: "t42",
	}

	result, err := WorktreeStatusHandler(nil, WorktreeStatusArgs{Name: "status-wt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "status-wt" {
		t.Errorf("expected name 'status-wt', got: %s", result.Name)
	}
	if result.Status != "active" {
		t.Errorf("expected status 'active', got: %s", result.Status)
	}
	if result.TaskID != "t42" {
		t.Errorf("expected taskID 't42', got: %s", result.TaskID)
	}
}

func TestWorktreeStatusHandler_NotFound(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	_, err := WorktreeStatusHandler(nil, WorktreeStatusArgs{Name: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent worktree")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestWorktreeEnterHandler_Success(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	wm.entries["enter-wt"] = &WorktreeEntry{
		Name:   "enter-wt",
		Status: "active",
	}

	result, err := WorktreeEnterHandler(nil, WorktreeEnterArgs{Name: "enter-wt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

func TestWorktreeEnterHandler_NotFound(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	_, err := WorktreeEnterHandler(nil, WorktreeEnterArgs{Name: "no-such-wt"})
	if err == nil {
		t.Fatal("expected error for nonexistent worktree")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestWorktreeCloseoutHandler_Keep(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	wm.entries["close-wt"] = &WorktreeEntry{
		Name:   "close-wt",
		Status: "active",
	}

	result, err := WorktreeCloseoutHandler(nil, WorktreeCloseoutArgs{
		Name:   "close-wt",
		Action: "keep",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if wm.entries["close-wt"].Status != "kept" {
		t.Errorf("expected status 'kept', got: %s", wm.entries["close-wt"].Status)
	}
}

func TestWorktreeCloseoutHandler_Remove(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	wm.entries["rm-wt"] = &WorktreeEntry{
		Name:   "rm-wt",
		Path:   filepath.Join(tempDir, ".worktrees", "rm-wt"),
		Branch: "wt/rm-wt",
		Status: "active",
	}

	result, err := WorktreeCloseoutHandler(nil, WorktreeCloseoutArgs{
		Name:   "rm-wt",
		Action: "remove",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if wm.entries["rm-wt"].Status != "removed" {
		t.Errorf("expected status 'removed', got: %s", wm.entries["rm-wt"].Status)
	}
}

func TestWorktreeCloseoutHandler_NotFound(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	_, err := WorktreeCloseoutHandler(nil, WorktreeCloseoutArgs{
		Name:   "ghost-wt",
		Action: "keep",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent worktree")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestWorktreeCloseoutHandler_InvalidAction(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	wm.entries["badaction-wt"] = &WorktreeEntry{
		Name:   "badaction-wt",
		Status: "active",
	}

	_, err := WorktreeCloseoutHandler(nil, WorktreeCloseoutArgs{
		Name:   "badaction-wt",
		Action: "explode",
	})
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
	if !strings.Contains(err.Error(), "invalid closeout action") {
		t.Errorf("expected 'invalid closeout action' in error, got: %v", err)
	}
}

func TestWorktreeCloseoutHandler_RemoveWithTaskCompletion(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	GlobalWorktreeManager = wm

	// Set up task manager with temp dir
	origTasksDir := GlobalTaskManager.tasksDir
	GlobalTaskManager.tasksDir = filepath.Join(tempDir, ".tasks")
	_ = os.MkdirAll(GlobalTaskManager.tasksDir, 0755)
	defer func() { GlobalTaskManager.tasksDir = origTasksDir }()

	// Create a task
	task := &TaskRecord{ID: "t-closeout", Subject: "test task", Status: "in_progress", Owner: "agent"}
	_ = GlobalTaskManager.SaveTask(task)

	wm.entries["task-wt"] = &WorktreeEntry{
		Name:   "task-wt",
		Path:   filepath.Join(tempDir, ".worktrees", "task-wt"),
		Branch: "wt/task-wt",
		TaskID: "t-closeout",
		Status: "active",
	}

	result, err := WorktreeCloseoutHandler(nil, WorktreeCloseoutArgs{
		Name:         "task-wt",
		Action:       "remove",
		CompleteTask: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}

	// Verify task was completed
	updated, _ := GlobalTaskManager.GetTask("t-closeout")
	if updated.Status != "completed" {
		t.Errorf("expected task status 'completed', got: %s", updated.Status)
	}
}

func TestWorktreeCloseoutHandler_RemoveGitFailure(t *testing.T) {
	origWM := GlobalWorktreeManager
	defer func() { GlobalWorktreeManager = origWM }()

	wm, tempDir := newTestWorktreeManager(t)
	defer os.RemoveAll(tempDir)
	wm.GitCommand = func(args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "remove" {
			return nil, os.ErrPermission
		}
		return []byte("ok"), nil
	}
	GlobalWorktreeManager = wm

	wm.entries["gitfail-wt"] = &WorktreeEntry{
		Name:   "gitfail-wt",
		Path:   filepath.Join(tempDir, ".worktrees", "gitfail-wt"),
		Branch: "wt/gitfail-wt",
		Status: "active",
	}

	_, err := WorktreeCloseoutHandler(nil, WorktreeCloseoutArgs{
		Name:   "gitfail-wt",
		Action: "remove",
	})
	if err == nil {
		t.Fatal("expected error when git remove fails")
	}
}

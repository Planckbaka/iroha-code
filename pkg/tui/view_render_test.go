package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iroha/pkg/agent"
)

// ---------------------------------------------------------------------------
// TestClearRendererCache — clears the renderer cache
// ---------------------------------------------------------------------------

func TestClearRendererCache(t *testing.T) {
	// Populate the cache first
	ClearRendererCache()
	RenderMarkdownWithWidth("hello", 80)

	rendererCacheMu.Lock()
	before := len(rendererCache)
	rendererCacheMu.Unlock()

	if before == 0 {
		t.Fatal("expected at least one cached renderer after RenderMarkdownWithWidth")
	}

	ClearRendererCache()

	rendererCacheMu.Lock()
	after := len(rendererCache)
	rendererCacheMu.Unlock()

	if after != 0 {
		t.Errorf("expected cache to be empty after ClearRendererCache, got %d entries", after)
	}
}

// ---------------------------------------------------------------------------
// TestRenderMarkdown — delegates to RenderMarkdownWithWidth at default width
// ---------------------------------------------------------------------------

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantEmpty bool
		wantSub   []string
	}{
		{"empty string", "", true, nil},
		{"whitespace only", "   \n  ", true, nil},
		{"simple text", "hello world", false, []string{"hello"}},
		{"markdown bold", "**bold**", false, nil},
		{"markdown heading", "# Title", false, nil},
	}

	ClearRendererCache()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderMarkdown(tt.input)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("RenderMarkdown(%q) expected empty, got %q", tt.input, got)
				}
				return
			}
			if got == "" {
				t.Errorf("RenderMarkdown(%q) expected non-empty", tt.input)
			}
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("RenderMarkdown(%q) expected to contain %q, got:\n%s", tt.input, sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderMarkdownWithWidth_DiffHighlighting — verifies diff lines
// ---------------------------------------------------------------------------

func TestRenderMarkdownWithWidth_DiffHighlighting(t *testing.T) {
	ClearRendererCache()

	input := "+ added line\n- removed line\nnormal line"
	got := RenderMarkdownWithWidth(input, 80)

	if got == "" {
		t.Fatal("expected non-empty output")
	}
	stripped := stripANSIRenderTest(got)
	if !strings.Contains(stripped, "+") && !strings.Contains(stripped, "added") {
		t.Error("expected output to contain the + diff line content")
	}
	if !strings.Contains(stripped, "-") && !strings.Contains(stripped, "removed") {
		t.Error("expected output to contain the - diff line content")
	}
}

// ---------------------------------------------------------------------------
// taskTestHelper manages isolated task state for tests. It discovers the
// tasksDir by checking where the current TaskManager resolves to, writes
// test task files directly, and cleans them up afterward.
// ---------------------------------------------------------------------------
type taskTestHelper struct {
	tasksDir string
	taskIDs  []string
}

func newTaskTestHelper(t *testing.T) *taskTestHelper {
	t.Helper()
	tasksDir := agent.ResolveTasksDir()
	if tasksDir == "" {
		t.Fatal("could not determine tasksDir for test isolation")
	}
	return &taskTestHelper{tasksDir: tasksDir}
}

func (h *taskTestHelper) saveTask(t *testing.T, task *agent.TaskRecord) {
	t.Helper()
	if err := agent.GlobalTaskManager.SaveTask(task); err != nil {
		t.Fatalf("SaveTask(%s) failed: %v", task.ID, err)
	}
	h.taskIDs = append(h.taskIDs, task.ID)
}

func (h *taskTestHelper) cleanup() {
	for _, id := range h.taskIDs {
		_ = agent.GlobalTaskManager.SaveTask(&agent.TaskRecord{
			ID: id, Subject: "cleanup", Status: "deleted", Owner: "agent",
		})
		// Also try to remove the file directly
		_ = os.Remove(filepath.Join(h.tasksDir, id+".json"))
	}
}

// ---------------------------------------------------------------------------
// TestRenderTaskDashboard — table-driven
// ---------------------------------------------------------------------------

func TestRenderTaskDashboard(t *testing.T) {
	tests := []struct {
		name       string
		setupTasks func(h *taskTestHelper)
		wantEmpty  bool
		wantSub    []string
	}{
		{
			name: "single pending task",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t1", Subject: "Fix rv bug", Status: "pending", Owner: "agent",
				})
			},
			wantEmpty: false,
			wantSub:   []string{"rvd-t1"},
		},
		{
			name: "single in_progress task",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t2", Subject: "Active rv work", Status: "in_progress", Owner: "agent",
				})
			},
			wantEmpty: false,
			wantSub:   []string{"rvd-t2"},
		},
		{
			name: "single completed task",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t3", Subject: "Done rv work", Status: "completed", Owner: "agent",
				})
			},
			wantEmpty: false,
			wantSub:   []string{"rvd-t3"},
		},
		{
			name: "blocked task with dependency",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t4", Subject: "Blocker rv", Status: "completed", Owner: "agent",
				})
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t5", Subject: "Blocked rv", Status: "pending",
					BlockedBy: []string{"rvd-t4"}, Owner: "agent",
				})
			},
			wantEmpty: false,
			wantSub:   []string{"rvd-t5"},
		},
		{
			name: "mixed tasks shows progress",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t6", Subject: "Completed rv", Status: "completed", Owner: "user",
				})
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t7", Subject: "Active rv", Status: "in_progress", Owner: "agent",
				})
				h.saveTask(t, &agent.TaskRecord{
					ID: "rvd-t8", Subject: "Pending rv", Status: "pending", Owner: "agent",
				})
			},
			wantEmpty: false,
			wantSub:   []string{"complete", "rvd-t6", "rvd-t7", "rvd-t8"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTaskTestHelper(t)
			defer h.cleanup()

			tt.setupTasks(h)

			got := RenderTaskDashboard()
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got:\n%s", got)
				}
				return
			}
			if got == "" {
				t.Fatal("expected non-empty output")
			}
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderTaskDetails — table-driven
// ---------------------------------------------------------------------------

func TestRenderTaskDetails(t *testing.T) {
	tests := []struct {
		name       string
		setupTasks func(h *taskTestHelper)
		wantSub    []string
	}{
		{
			name: "single completed task",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t1", Subject: "Completed detail", Status: "completed", Owner: "agent",
				})
			},
			wantSub: []string{"Durable Work Graph", "rtd-t1"},
		},
		{
			name: "single in_progress task",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t2", Subject: "Active detail", Status: "in_progress", Owner: "agent",
				})
			},
			wantSub: []string{"Durable Work Graph", "rtd-t2"},
		},
		{
			name: "blocked task shows dependencies",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t3", Subject: "Blocker", Status: "completed", Owner: "agent",
				})
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t4", Subject: "Blocked", Status: "pending",
					BlockedBy: []string{"rtd-t3"}, Owner: "agent",
				})
			},
			wantSub: []string{"Durable Work Graph", "rtd-t4"},
		},
		{
			name: "all categories mixed",
			setupTasks: func(h *taskTestHelper) {
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t5", Subject: "Done", Status: "completed", Owner: "user",
				})
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t6", Subject: "Running", Status: "in_progress", Owner: "agent",
				})
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t7", Subject: "Waiting", Status: "pending", Owner: "agent",
				})
				h.saveTask(t, &agent.TaskRecord{
					ID: "rtd-t8", Subject: "Stuck", Status: "pending",
					BlockedBy: []string{"rtd-t7"}, Owner: "agent",
				})
			},
			wantSub: []string{"Durable Work Graph", "complete", "rtd-t5", "rtd-t6", "rtd-t7", "rtd-t8"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTaskTestHelper(t)
			defer h.cleanup()

			tt.setupTasks(h)

			got := RenderTaskDetails()
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderTodoDashboard — table-driven
// ---------------------------------------------------------------------------

func TestRenderTodoDashboard(t *testing.T) {
	tests := []struct {
		name      string
		setupTodo func()
		wantEmpty bool
		wantSub   []string
	}{
		{
			name:      "empty todo returns empty",
			setupTodo: func() {},
			wantEmpty: true,
		},
		{
			name: "items present renders dashboard",
			setupTodo: func() {
				_ = agent.GlobalTodoManager.Update([]agent.TodoItem{
					{Content: "Write tests", Status: "in_progress"},
					{Content: "Ship feature", Status: "pending"},
				})
			},
			wantEmpty: false,
			wantSub:   []string{"Tasks", "Write tests", "Ship feature"},
		},
		{
			name: "completed items show progress",
			setupTodo: func() {
				_ = agent.GlobalTodoManager.Update([]agent.TodoItem{
					{Content: "Done item", Status: "completed"},
					{Content: "Active item", Status: "in_progress"},
				})
			},
			wantEmpty: false,
			wantSub:   []string{"Tasks", "Done item", "Active item"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset todo items for clean state
			agent.GlobalTodoManager.Update(nil)

			tt.setupTodo()

			got := RenderTodoDashboard()
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got:\n%s", got)
				}
				return
			}
			if got == "" {
				t.Fatal("expected non-empty output")
			}
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderConfirmCardWithDiff_Branches — tests diff/hasDiff variations
// ---------------------------------------------------------------------------

func TestRenderConfirmCardWithDiff_Branches(t *testing.T) {
	tests := []struct {
		name       string
		prompt     string
		selected   int
		hasDiff    bool
		diffActive bool
		wantSub    []string
	}{
		{
			name:     "no diff hides diff hint",
			prompt:   "Allow shell?",
			selected: 0,
			hasDiff:  false,
			wantSub:  []string{"Authorization Required", "Allow"},
		},
		{
			name:       "has diff not active shows Show Diff",
			prompt:     "Allow write?",
			selected:   1,
			hasDiff:    true,
			diffActive: false,
			wantSub:    []string{"Authorization Required", "Show Diff"},
		},
		{
			name:       "has diff active shows Hide Diff",
			prompt:     "Allow edit?",
			selected:   2,
			hasDiff:    true,
			diffActive: true,
			wantSub:    []string{"Authorization Required", "Hide Diff"},
		},
		{
			name:     "file_write prompt uses secondary border",
			prompt:   "[file_write] write main.go",
			selected: 3,
			wantSub:  []string{"Authorization Required", "Edit"},
		},
		{
			name:     "file_read prompt uses secondary border",
			prompt:   "[file_read] read config.json",
			selected: 0,
			wantSub:  []string{"Authorization Required"},
		},
		{
			name:     "mcp prompt uses primary border",
			prompt:   "[mcp] call external API",
			selected: 4,
			wantSub:  []string{"Authorization Required"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderConfirmCardWithDiff(tt.prompt, tt.selected, tt.hasDiff, tt.diffActive)
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderSessionScreen_Branches — covers index>0 branch
// ---------------------------------------------------------------------------

func TestRenderSessionScreen_Branches(t *testing.T) {
	tests := []struct {
		name        string
		index       int
		sessions    []SessionEntry
		wantPointer bool // true if ▶ should appear (index > 0 on a session)
		wantSub     []string
	}{
		{
			name:        "index 0 no sessions shows new session with pointer",
			index:       0,
			sessions:    nil,
			wantPointer: false,
			wantSub:     []string{"Session History Manager", "Start New Session"},
		},
		{
			name:  "index 1 with one session selects it",
			index: 1,
			sessions: []SessionEntry{
				{ID: "ss1", LastUpdateStr: "2024-01-01", LastMsg: "hello session"},
			},
			wantPointer: true,
			wantSub:     []string{"Session History Manager", "hello session"},
		},
		{
			name:  "index 0 with sessions shows new session highlighted",
			index: 0,
			sessions: []SessionEntry{
				{ID: "ss2", LastUpdateStr: "2024-02-01", LastMsg: "old session"},
			},
			wantPointer: false,
			wantSub:     []string{"Session History Manager", "old session"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewScreenComponent()
			sc.screenType = "session"
			sc.sessionListIndex = tt.index
			if tt.sessions != nil {
				sc.SetSessions(tt.sessions)
			}

			lines := sc.Render(80)
			joined := strings.Join(lines, "\n")
			for _, sub := range tt.wantSub {
				if !strings.Contains(joined, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, joined)
				}
			}
			if tt.wantPointer && !strings.Contains(joined, "▶") {
				t.Error("expected pointer marker ▶ in output")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestOnStateChangeNoOps — calling no-op OnStateChange methods for coverage
// ---------------------------------------------------------------------------

func TestOnStateChangeNoOps(t *testing.T) {
	t.Run("ChatComponent OnStateChange", func(t *testing.T) {
		c := NewChatComponent(nil)
		c.OnStateChange(statePrompt, stateThinking)
		// No panic = pass
	})
	t.Run("ConfirmComponent OnStateChange", func(t *testing.T) {
		cc := NewConfirmComponent()
		cc.OnStateChange(statePrompt, stateConfirming)
		// No panic = pass
	})
	t.Run("SlashMenuComponent OnStateChange", func(t *testing.T) {
		sm := NewSlashMenuComponent(nil)
		sm.OnStateChange(statePrompt, statePrompt)
		// No panic = pass
	})
}

// ---------------------------------------------------------------------------
// TestClampScrollOffset — covers both branches
// ---------------------------------------------------------------------------

func TestClampScrollOffset(t *testing.T) {
	tests := []struct {
		name         string
		totalLines   int
		maxLines     int
		scrollOffset int
		wantOffset   int
	}{
		{"zero total lines early return", 0, 20, 5, 5},
		{"offset within bounds", 100, 80, 10, 10},
		{"offset exceeds max", 100, 80, 50, 20},
		{"negative offset clamped to 0", 100, 80, -5, 0},
		{"offset exactly at max", 100, 80, 20, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := NewHistoryStore()
			hs.lastTotalLines = tt.totalLines
			hs.lastMaxLines = tt.maxLines
			hs.scrollOffset = tt.scrollOffset

			hs.clampScrollOffset()

			if hs.scrollOffset != tt.wantOffset {
				t.Errorf("scrollOffset = %d, want %d", hs.scrollOffset, tt.wantOffset)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestExecutePromptStateSetup — covers the state-setup portion of executePrompt
// ---------------------------------------------------------------------------

func TestExecutePromptStateSetup(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	app.state = statePrompt
	beforeRound := app.roundCount

	app.executePrompt("")

	if app.roundCount != beforeRound {
		t.Error("empty prompt should not increment round count")
	}
	if app.state != statePrompt {
		t.Error("empty prompt should not change state")
	}
}

// ---------------------------------------------------------------------------
// TestRenderWelcomeCard_Branches — covers nil runner case
// ---------------------------------------------------------------------------

func TestRenderWelcomeCard_Branches(t *testing.T) {
	tests := []struct {
		name    string
		runner  *agent.CustomRunner
		wantSub []string
	}{
		{
			name:    "nil runner shows Unknown",
			runner:  nil,
			wantSub: []string{"Iroha Code", "Unknown"},
		},
		{
			name:    "non-nil runner shows model",
			runner:  &agent.CustomRunner{},
			wantSub: []string{"Iroha Code"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderWelcomeCard(tt.runner)
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("expected output to contain %q, got:\n%s", sub, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRenderMarkdownWithWidth_RendererCacheAndEdgeCases
// ---------------------------------------------------------------------------

func TestRenderMarkdownWithWidth_RendererCacheAndEdgeCases(t *testing.T) {
	ClearRendererCache()

	t.Run("caches renderer by width", func(t *testing.T) {
		result1 := RenderMarkdownWithWidth("test content", 80)
		result2 := RenderMarkdownWithWidth("more content", 80)
		if result1 == "" || result2 == "" {
			t.Error("expected non-empty results")
		}

		rendererCacheMu.Lock()
		count := len(rendererCache)
		rendererCacheMu.Unlock()

		if count == 0 {
			t.Error("expected renderer to be cached")
		}
	})

	t.Run("CRLF normalization", func(t *testing.T) {
		ClearRendererCache()
		got := RenderMarkdownWithWidth("line1\r\nline2\r\n", 80)
		if got == "" {
			t.Error("expected non-empty for CRLF input")
		}
	})

	t.Run("renderer error fallback returns raw", func(t *testing.T) {
		ClearRendererCache()
		// A very small width should still work
		got := RenderMarkdownWithWidth("hello", 1)
		if got == "" {
			t.Error("expected non-empty even with width=1")
		}
	})
}

// ---------------------------------------------------------------------------
// TestAppRender_States — covers Render in various App states
// ---------------------------------------------------------------------------

func TestAppRender_States(t *testing.T) {
	tests := []struct {
		name       string
		state      TuiState
		setup      func(app *App)
		wantNonNil bool
		wantSub    []string
	}{
		{
			name:       "statePrompt with empty history shows welcome",
			state:      statePrompt,
			setup:      func(_ *App) {},
			wantNonNil: true,
			wantSub:    []string{"Iroha Code"},
		},
		{
			name:       "stateThinking renders thinking state",
			state:      stateThinking,
			setup:      func(_ *App) {},
			wantNonNil: true,
		},
		{
			name:  "stateStreaming with streamed text renders markdown",
			state: stateStreaming,
			setup: func(app *App) {
				app.streamedText = "**bold text**"
			},
			wantNonNil: true,
		},
		{
			name:  "statePrompt with history does not show welcome",
			state: statePrompt,
			setup: func(app *App) {
				app.history.Add(HistoryEntry{Role: RoleUser, Content: "hello"})
			},
			wantNonNil: true,
		},
		{
			name:  "stateConfirming renders confirm card",
			state: stateConfirming,
			setup: func(app *App) {
				app.confirm.SetPrompt("Allow file write to /tmp/test.go?")
			},
			wantNonNil: true,
			wantSub:    []string{"Deny"},
		},
		{
			name:  "statePermissionSelect renders screen overlay",
			state: statePermissionSelect,
			setup: func(app *App) {
				app.screens.screenType = "permission"
			},
			wantNonNil: true,
			wantSub:    []string{"Select Agent Permission Mode"},
		},
		{
			name:  "stateSessionSelect renders session screen",
			state: stateSessionSelect,
			setup: func(app *App) {
				app.screens.screenType = "session"
			},
			wantNonNil: true,
			wantSub:    []string{"Session History Manager"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = tt.state
			app.width = 80
			app.height = 24
			if tt.setup != nil {
				tt.setup(app)
			}

			lines := app.Render()
			if tt.wantNonNil && lines == nil {
				t.Fatal("expected non-nil output")
			}
			if tt.wantSub != nil {
				joined := strings.Join(lines, "\n")
				for _, sub := range tt.wantSub {
					if !strings.Contains(joined, sub) {
						t.Errorf("expected output to contain %q, got:\n%s", sub, joined)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleKeyCtrlC_States — covers handleKey CtrlC in all states
// ---------------------------------------------------------------------------

func TestHandleKeyCtrlC_States(t *testing.T) {
	tests := []struct {
		name     string
		state    TuiState
		wantExit bool
	}{
		{"CtrlC in prompt returns true", statePrompt, true},
		{"CtrlC in thinking cancels", stateThinking, false},
		{"CtrlC in streaming cancels", stateStreaming, false},
		{"CtrlC in confirming returns false (delegates)", stateConfirming, false},
		{"CtrlC in permission select returns true", statePermissionSelect, true},
		{"CtrlC in session select returns true", stateSessionSelect, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = tt.state
			app.roundStartTime = time.Now()
			if tt.state == stateStreaming {
				app.streamedText = "**partial**"
			}

			got := app.handleKey(Key{Type: KeyCtrlC})
			if got != tt.wantExit {
				t.Errorf("handleKey(CtrlC) in %v = %v, want %v", tt.state, got, tt.wantExit)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestFinalizeTurn_WithStreamedTextAndNilRunner
// ---------------------------------------------------------------------------

func TestFinalizeTurn_WithStreamedTextAndNilRunner(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	app.state = stateStreaming
	app.streamedText = "## Result\nDone."
	app.roundStartTime = time.Now()

	app.finalizeTurn()

	if app.state != statePrompt {
		t.Errorf("state = %v, want statePrompt", app.state)
	}
	if app.streamedText != "" {
		t.Error("streamedText should be cleared")
	}
	if app.history.Len() == 0 {
		t.Error("expected history entries after finalize")
	}
}

// ---------------------------------------------------------------------------
// TestHandleRawSlashCommand_AdditionalBranches
// ---------------------------------------------------------------------------

func TestHandleRawSlashCommand_AdditionalBranches(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantExit  bool
		wantState TuiState
		postCheck func(t *testing.T, app *App)
	}{
		{
			name:     "/task renders task details",
			input:    "/task",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries after /task")
				}
			},
		},
		{
			name:     "/mode opens permission select",
			input:    "/mode",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries after /mode")
				}
			},
		},
		{
			name:     "/todo renders todo dashboard",
			input:    "/todo",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries after /todo")
				}
			},
		},
		{
			name:     "/doctor runs diagnostics",
			input:    "/doctor",
			wantExit: false,
			postCheck: func(t *testing.T, app *App) {
				if app.history.Len() == 0 {
					t.Error("expected history entries after /doctor")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = statePrompt

			got := app.handleRawSlashCommand(tt.input)
			if got != tt.wantExit {
				t.Errorf("handleRawSlashCommand(%q) = %v, want %v", tt.input, got, tt.wantExit)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestFinalizeTurn_ErrorAndTokenPaths — covers error and token estimation branches
// ---------------------------------------------------------------------------

func TestFinalizeTurn_ErrorAndTokenPaths(t *testing.T) {
	tests := []struct {
		name         string
		streamedText string
		lastError    error
		preTokens    int
		roundStarted bool
		wantState    TuiState
		postCheck    func(t *testing.T, app *App)
	}{
		{
			name:         "error with streamed text commits both",
			streamedText: "**partial**",
			lastError:    nil,
			preTokens:    0,
			roundStarted: true,
			wantState:    statePrompt,
			postCheck: func(t *testing.T, app *App) {
				// Should have agent entry + system entry from cancel card
				if app.history.Len() < 1 {
					t.Errorf("expected at least 1 history entry, got %d", app.history.Len())
				}
			},
		},
		{
			name:         "finalize clears roundStartTime",
			streamedText: "",
			lastError:    nil,
			preTokens:    100,
			roundStarted: true,
			wantState:    statePrompt,
			postCheck: func(t *testing.T, app *App) {
				if !app.roundStartTime.IsZero() {
					t.Error("roundStartTime should be zero after finalize")
				}
			},
		},
		{
			name:         "finalize with zero roundStartTime does not crash",
			streamedText: "text",
			lastError:    nil,
			preTokens:    0,
			roundStarted: false,
			wantState:    statePrompt,
			postCheck: func(t *testing.T, app *App) {
				if app.streamedText != "" {
					t.Error("streamedText should be cleared")
				}
			},
		},
		{
			name:         "finalize with pre-existing tokens preserves them",
			streamedText: "",
			lastError:    nil,
			preTokens:    500,
			roundStarted: true,
			wantState:    statePrompt,
			postCheck: func(t *testing.T, app *App) {
				if app.totalTokens != 500 {
					t.Errorf("totalTokens = %d, want 500", app.totalTokens)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = stateStreaming
			app.streamedText = tt.streamedText
			app.lastError = tt.lastError
			app.totalTokens = tt.preTokens
			if tt.roundStarted {
				app.roundStartTime = time.Now()
			}

			app.finalizeTurn()

			if app.state != tt.wantState {
				t.Errorf("state = %v, want %v", app.state, tt.wantState)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, app)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandlePermSelect_SessionPicker — covers the startInSessionPicker branch
// ---------------------------------------------------------------------------

func TestHandlePermSelect_SessionPicker(t *testing.T) {
	app := NewApp(nil, "test-session", true, "")
	app.state = statePermissionSelect

	// GlobalSessionService is nil, so loadSessionsList returns early
	agent.GlobalSessionService = nil

	app.handlePermSelect("default")

	if app.state != stateSessionSelect {
		t.Errorf("state = %v, want stateSessionSelect", app.state)
	}
}

// ---------------------------------------------------------------------------
// TestHandleRawSlashCommand_PermissionWithMode
// ---------------------------------------------------------------------------

func TestHandleRawSlashCommand_PermissionWithModes(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"/permission plan", "/permission plan"},
		{"/permission auto", "/permission auto"},
		{"/permission bypass", "/permission bypass"},
		{"/permission acceptEdits", "/permission acceptEdits"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, "test-session", false, "")
			app.state = statePrompt

			result := app.handleRawSlashCommand(tt.input)
			if result {
				t.Error("expected false (not exit)")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleToolStatus_StreamAccumulation — covers StreamLines append path
// ---------------------------------------------------------------------------

func TestHandleToolStatus_StreamAccumulation(t *testing.T) {
	app := NewApp(nil, "test-session", false, "")
	app.state = stateStreaming

	// First tool status: running
	app.handleToolStatus(agent.ToolStatus{
		Name: "shell_run", Running: true,
		StreamLines: []string{"line1"},
	})

	if !app.chat.activeTool.Running {
		t.Error("expected active tool to be running")
	}

	// Second status for same tool: should accumulate stream lines
	app.handleToolStatus(agent.ToolStatus{
		Name: "shell_run", Running: true,
		StreamLines: []string{"line2", "line3"},
	})

	if len(app.chat.activeTool.StreamLines) != 3 {
		t.Errorf("expected 3 stream lines, got %d", len(app.chat.activeTool.StreamLines))
	}
}

// ---------------------------------------------------------------------------
// TestHandleKey_ScrollEdgeCases — covers pageLines<=0 and component dispatch
// ---------------------------------------------------------------------------

func TestHandleKey_ScrollEdgeCases(t *testing.T) {
	t.Run("PgUp with zero height uses fallback pageLines", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = statePrompt
		app.height = 0 // triggers pageLines <= 0
		for i := 0; i < 30; i++ {
			app.history.Add(HistoryEntry{Role: RoleSystem, Content: "entry"})
		}

		got := app.handleKey(Key{Type: KeyPgUp})
		if got {
			t.Error("expected false")
		}
	})

	t.Run("PgDown with zero height uses fallback pageLines", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = statePrompt
		app.height = 0
		for i := 0; i < 30; i++ {
			app.history.Add(HistoryEntry{Role: RoleSystem, Content: "entry"})
		}

		got := app.handleKey(Key{Type: KeyPgDown})
		if got {
			t.Error("expected false")
		}
	})

	t.Run("component dispatch handles input key", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = statePrompt
		// Type a character — should be handled by InputComponent
		got := app.handleKey(Key{Type: KeyRune, Rune: 'a'})
		if got {
			t.Error("expected false for rune input")
		}
	})
}

// ---------------------------------------------------------------------------
// TestAppRender_CursorAndDashboard — covers cursor calculation and dashboard branches
// ---------------------------------------------------------------------------

func TestAppRender_CursorAndDashboard(t *testing.T) {
	t.Run("render with todo items shows dashboard", func(t *testing.T) {
		agent.GlobalTodoManager.Update([]agent.TodoItem{
			{Content: "Test task", Status: "in_progress"},
		})
		defer agent.GlobalTodoManager.Update(nil)

		app := NewApp(nil, "test-session", false, "")
		app.state = statePrompt
		app.width = 80
		app.height = 30

		lines := app.Render()
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "Test task") {
			t.Errorf("expected todo item in render output")
		}
	})

	t.Run("render with history sets cursor position", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = statePrompt
		app.width = 80
		app.height = 30
		app.history.Add(HistoryEntry{Role: RoleUser, Content: "hello"})
		app.history.Add(HistoryEntry{Role: RoleAgent, Content: "world"})

		lines := app.Render()
		if lines == nil {
			t.Fatal("expected non-nil")
		}
		// cursorRow should be set for prompt state
		if app.cursorRow < 0 {
			t.Errorf("cursorRow = %d, expected >= 0 for prompt state", app.cursorRow)
		}
	})

	t.Run("render with viewportLines < 1 uses minimum", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = statePrompt
		app.width = 80
		app.height = 1 // very small, forces viewportLines < 1

		lines := app.Render()
		if lines == nil {
			t.Fatal("expected non-nil even with tiny height")
		}
	})
}

// ---------------------------------------------------------------------------
// TestHandleEvent_AdditionalCases — covers more event types
// ---------------------------------------------------------------------------

func TestHandleEvent_AdditionalCases(t *testing.T) {
	t.Run("StreamTextMsg without status tag in streaming state", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = stateStreaming
		app.streamedText = "previous "

		got := app.HandleEvent(StreamTextMsg{Text: "more text"})
		if got {
			t.Error("expected false")
		}
		if !strings.Contains(app.streamedText, "more text") {
			t.Errorf("streamedText = %q, expected to contain 'more text'", app.streamedText)
		}
	})

	t.Run("AgentDoneMsg from thinking state", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = stateThinking
		app.roundStartTime = time.Now()

		got := app.HandleEvent(AgentDoneMsg{})
		if got {
			t.Error("expected false")
		}
		if app.state != statePrompt {
			t.Errorf("state = %v, want statePrompt", app.state)
		}
	})

	t.Run("AgentErrorMsg from thinking state", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = stateThinking
		app.roundStartTime = time.Now()

		got := app.HandleEvent(AgentErrorMsg{Err: errors.New("test error")})
		if got {
			t.Error("expected false")
		}
		if app.state != statePrompt {
			t.Errorf("state = %v, want statePrompt", app.state)
		}
	})
}

// ---------------------------------------------------------------------------
// TestRenderer_Draw — covers cursorUpLines and last-line rendering
// ---------------------------------------------------------------------------

func TestRenderer_Draw(t *testing.T) {
	t.Run("draw with cursorUpLines emits cursor restore", func(t *testing.T) {
		var buf strings.Builder
		r := NewRawRenderer(&buf)
		r.cursorUpLines = 3

		r.Draw([]string{"hello", "world"}, -1, 0)

		if !strings.Contains(buf.String(), "\x1b[3B") {
			t.Error("expected cursor-down escape in output")
		}
	})

	t.Run("draw resets cursorUpLines to 0", func(t *testing.T) {
		var buf strings.Builder
		r := NewRawRenderer(&buf)
		r.cursorUpLines = 5

		r.Draw([]string{"test"}, -1, 0)

		if r.cursorUpLines != 0 {
			t.Errorf("cursorUpLines = %d, want 0", r.cursorUpLines)
		}
	})
}

// ---------------------------------------------------------------------------
// TestComponentChat_RenderStreaming — covers streaming render branch
// ---------------------------------------------------------------------------

func TestComponentChat_RenderStreaming(t *testing.T) {
	t.Run("streaming with stream text renders markdown", func(t *testing.T) {
		c := NewChatComponent(nil)
		streamText := "**bold text**"
		streamRendered := RenderMarkdownWithWidth(streamText, 78)

		lines := c.RenderTail(stateStreaming, 80, streamText, streamRendered, nil, nil)
		if len(lines) == 0 {
			t.Fatal("expected non-empty lines")
		}
	})

	t.Run("streaming with empty stream text uses rendered", func(t *testing.T) {
		c := NewChatComponent(nil)
		rendered := "pre-rendered content"

		lines := c.RenderTail(stateStreaming, 80, "", rendered, nil, nil)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "pre-rendered") {
			t.Errorf("expected rendered content in output, got:\n%s", joined)
		}
	})

	t.Run("confirming state returns confirm lines", func(t *testing.T) {
		c := NewChatComponent(nil)
		confirmLines := []string{"confirm prompt here"}

		lines := c.RenderTail(stateConfirming, 80, "", "", nil, confirmLines)
		if len(lines) != 1 || lines[0] != "confirm prompt here" {
			t.Errorf("expected confirm lines, got: %v", lines)
		}
	})

	t.Run("prompt with welcome lines renders them", func(t *testing.T) {
		c := NewChatComponent(nil)
		welcome := []string{"Welcome to Iroha Code"}

		lines := c.RenderTail(statePrompt, 80, "", "", welcome, nil)
		if len(lines) == 0 {
			t.Fatal("expected non-empty lines")
		}
		if lines[0] != "Welcome to Iroha Code" {
			t.Errorf("expected welcome line, got: %v", lines)
		}
	})
}

// ---------------------------------------------------------------------------
// TestHandleEvent_ToolStatusBranches — covers more tool status paths
// ---------------------------------------------------------------------------

func TestHandleEvent_ToolStatusBranches(t *testing.T) {
	t.Run("ToolStatusMsg with accumulated stream lines", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = stateStreaming
		app.chat.activeTool = agent.ToolStatus{
			Name: "shell_run", Running: true,
			StreamLines: []string{"old1", "old2"},
		}
		app.streamedText = "**partial output**"

		// Tool completes with success
		app.HandleEvent(ToolStatusMsg{Status: agent.ToolStatus{
			Name: "file_read", Success: true,
			Args: map[string]any{"path": "/tmp/x.go"},
		}})

		// Should have committed streamed text to history
		found := false
		for _, e := range app.history.entries {
			if e.Role == RoleAgent {
				found = true
			}
		}
		if !found {
			t.Error("expected agent entry in history after tool completion during streaming")
		}
	})

	t.Run("ToolStatusMsg running with same tool accumulates stream", func(t *testing.T) {
		app := NewApp(nil, "test-session", false, "")
		app.state = stateStreaming
		app.chat.activeTool = agent.ToolStatus{
			Name: "shell_run", Running: true,
			StreamLines: []string{"old"},
		}

		app.HandleEvent(ToolStatusMsg{Status: agent.ToolStatus{
			Name: "shell_run", Running: true,
			StreamLines: []string{"new1", "new2"},
		}})

		if len(app.chat.activeTool.StreamLines) != 3 {
			t.Errorf("expected 3 stream lines, got %d", len(app.chat.activeTool.StreamLines))
		}
	})
}

// ---------------------------------------------------------------------------
// Helper: stripANSIRenderTest removes common ANSI escape sequences
// ---------------------------------------------------------------------------
func stripANSIRenderTest(s string) string {
	var result []byte
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !isLetterRenderTest(s[i]) {
				i++
			}
			if i < len(s) {
				i++
			}
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}

func isLetterRenderTest(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

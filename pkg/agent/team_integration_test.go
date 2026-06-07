package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTeamEnv creates a temp directory and initializes a TeamManager with
// inbox directory. It also resets GlobalProtocolManager and GlobalAutonomyManager
// to temp-backed instances so integration tests do not touch the real filesystem.
func setupTeamEnv(t *testing.T) (*TeamManager, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "team-integration-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	_ = os.MkdirAll(filepath.Join(tempDir, "inbox"), 0755)

	tm := &TeamManager{
		teamDir:     tempDir,
		teammates:   make(map[string]*Teammate),
		activeLoops: make(map[string]chan struct{}),
	}

	return tm, tempDir
}

// setupProtocolEnv resets GlobalProtocolManager to use a temp directory.
func setupProtocolEnv(t *testing.T) *ProtocolManager {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "protocol-integration-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	pm := &ProtocolManager{
		requestsDir: tempDir,
	}
	return pm
}

// ─── Full Team Lifecycle Integration ──────────────────────────────────────────

func TestIntegration_TeamLifecycle(t *testing.T) {
	tm, _ := setupTeamEnv(t)

	// Step 1: Register a teammate
	tmate, err := tm.RegisterTeammate("worker-alice", "Developer", "Write code.", "executor")
	if err != nil {
		t.Fatalf("RegisterTeammate failed: %v", err)
	}
	if tmate.Status != "idle" {
		t.Errorf("expected status idle, got %s", tmate.Status)
	}

	// Step 2: Set up ProcessMessage callback and start the loop
	processed := make(chan TeamMessage, 1)
	tm.ProcessMessage = func(teammate *Teammate, msg TeamMessage) (string, error) {
		processed <- msg
		return "processed: " + msg.Content, nil
	}

	if err := tm.StartTeammateLoop("worker-alice"); err != nil {
		t.Fatalf("StartTeammateLoop failed: %v", err)
	}

	// Step 3: Send a message to the teammate's inbox
	req := TeamMessage{
		Sender:    "coordinator",
		Content:   "Build the auth module",
		Timestamp: float64(time.Now().Unix()),
	}
	if err := tm.AppendToInbox("worker-alice", req); err != nil {
		t.Fatalf("AppendToInbox failed: %v", err)
	}

	// Step 4: Wait for the loop to pick up and process the message
	select {
	case msg := <-processed:
		if msg.Content != "Build the auth module" {
			t.Errorf("unexpected processed message content: %s", msg.Content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message processing in background loop")
	}

	// Step 5: Check that reply landed in sender's inbox
	time.Sleep(200 * time.Millisecond)
	replies, err := tm.ReadAndClearInbox("coordinator")
	if err != nil {
		t.Fatalf("ReadAndClearInbox coordinator failed: %v", err)
	}
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(replies))
	}
	if replies[0].Sender != "worker-alice" || replies[0].Content != "processed: Build the auth module" {
		t.Errorf("unexpected reply: %+v", replies[0])
	}

	// Step 6: Stop the loop and verify status
	tm.StopTeammateLoop("worker-alice")
	time.Sleep(100 * time.Millisecond)

	teammate, err := tm.GetTeammate("worker-alice")
	if err != nil {
		t.Fatalf("GetTeammate failed: %v", err)
	}
	if teammate.Status != "offline" {
		t.Errorf("expected status offline after stop, got %s", teammate.Status)
	}
}

// ─── Broadcast Integration ────────────────────────────────────────────────────

func TestIntegration_BroadcastWithPeekInbox(t *testing.T) {
	tm, _ := setupTeamEnv(t)

	// Register 3 teammates
	_, _ = tm.RegisterTeammate("alice", "Dev", "", "executor")
	_, _ = tm.RegisterTeammate("bob", "QA", "", "reviewer")
	_, _ = tm.RegisterTeammate("carol", "PM", "", "planner")

	// Broadcast from alice
	if err := tm.Broadcast("alice", "Sprint review at 3pm"); err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	// Verify bob and carol received the message via PeekInbox (should NOT clear)
	bobMsgs, err := tm.PeekInbox("bob")
	if err != nil {
		t.Fatalf("PeekInbox bob failed: %v", err)
	}
	if len(bobMsgs) != 1 || bobMsgs[0].Content != "Sprint review at 3pm" || bobMsgs[0].Sender != "alice" {
		t.Errorf("bob got unexpected messages via PeekInbox: %+v", bobMsgs)
	}

	carolMsgs, err := tm.PeekInbox("carol")
	if err != nil {
		t.Fatalf("PeekInbox carol failed: %v", err)
	}
	if len(carolMsgs) != 1 || carolMsgs[0].Content != "Sprint review at 3pm" {
		t.Errorf("carol got unexpected messages via PeekInbox: %+v", carolMsgs)
	}

	// Verify alice's inbox is empty (sender is excluded from broadcast)
	aliceMsgs, err := tm.PeekInbox("alice")
	if err != nil {
		t.Fatalf("PeekInbox alice failed: %v", err)
	}
	if len(aliceMsgs) != 0 {
		t.Errorf("alice should not have received broadcast, got %d messages", len(aliceMsgs))
	}

	// Now ReadAndClearInbox should still have messages (PeekInbox didn't clear)
	bobMsgs2, err := tm.ReadAndClearInbox("bob")
	if err != nil {
		t.Fatalf("ReadAndClearInbox bob failed: %v", err)
	}
	if len(bobMsgs2) != 1 {
		t.Errorf("expected 1 message after PeekInbox, got %d", len(bobMsgs2))
	}

	// After clearing, PeekInbox should be empty
	bobMsgs3, err := tm.PeekInbox("bob")
	if err != nil {
		t.Fatalf("PeekInbox bob (after clear) failed: %v", err)
	}
	if len(bobMsgs3) != 0 {
		t.Errorf("expected empty inbox after clear, got %d messages", len(bobMsgs3))
	}
}

// ─── Protocol Request/Response Integration ────────────────────────────────────

func TestIntegration_ProtocolShutdownFlow(t *testing.T) {
	pm := setupProtocolEnv(t)

	// Create a shutdown request
	req, err := pm.CreateRequest("shutdown", "lead-dev", "architect", map[string]any{"reason": "task completed"})
	if err != nil {
		t.Fatalf("CreateRequest failed: %v", err)
	}
	if req.Status != "pending" {
		t.Errorf("expected pending, got %s", req.Status)
	}

	// Retrieve and verify
	fetched, err := pm.GetRequest(req.RequestID)
	if err != nil {
		t.Fatalf("GetRequest failed: %v", err)
	}
	if fetched.Type != "shutdown" || fetched.Sender != "lead-dev" || fetched.Receiver != "architect" {
		t.Errorf("unexpected request data: %+v", fetched)
	}

	// Approve the shutdown request
	resp, err := pm.RespondToRequest(req.RequestID, true, "Approved. Good work.")
	if err != nil {
		t.Fatalf("RespondToRequest approve failed: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("shutdown approval should yield 'completed', got %s", resp.Status)
	}
	if resp.Comment != "Approved. Good work." {
		t.Errorf("unexpected comment: %s", resp.Comment)
	}
}

func TestIntegration_ProtocolPlanApprovalFlow(t *testing.T) {
	pm := setupProtocolEnv(t)

	// Create a plan_approval request
	req, err := pm.CreateRequest("plan_approval", "planner", "reviewer", map[string]any{"plan": "Refactor the TUI layer"})
	if err != nil {
		t.Fatalf("CreateRequest failed: %v", err)
	}
	if req.Status != "pending" {
		t.Errorf("expected pending, got %s", req.Status)
	}

	// Approve
	resp, err := pm.RespondToRequest(req.RequestID, true, "LGTM")
	if err != nil {
		t.Fatalf("RespondToRequest approve failed: %v", err)
	}
	if resp.Status != "approved" {
		t.Errorf("plan approval should yield 'approved', got %s", resp.Status)
	}
}

func TestIntegration_ProtocolRejectionFlow(t *testing.T) {
	pm := setupProtocolEnv(t)

	req, err := pm.CreateRequest("plan_approval", "planner", "lead", map[string]any{"plan": "Rewrite everything in Rust"})
	if err != nil {
		t.Fatalf("CreateRequest failed: %v", err)
	}

	resp, err := pm.RespondToRequest(req.RequestID, false, "Too risky")
	if err != nil {
		t.Fatalf("RespondToRequest reject failed: %v", err)
	}
	if resp.Status != "rejected" {
		t.Errorf("expected rejected, got %s", resp.Status)
	}
}

func TestIntegration_ProtocolDuplicateResponseRejected(t *testing.T) {
	pm := setupProtocolEnv(t)

	req, err := pm.CreateRequest("shutdown", "dev1", "dev2", map[string]any{"reason": "done"})
	if err != nil {
		t.Fatalf("CreateRequest failed: %v", err)
	}

	// First response succeeds
	_, err = pm.RespondToRequest(req.RequestID, true, "ok")
	if err != nil {
		t.Fatalf("first RespondToRequest failed: %v", err)
	}

	// Second response should fail
	_, err = pm.RespondToRequest(req.RequestID, false, "changed my mind")
	if err == nil {
		t.Error("expected second response to fail, but it succeeded")
	}
}

// ─── Protocol Tool Handlers via GlobalProtocolManager ──────────────────────────

func TestIntegration_ProtocolShutdownHandlers(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "protocol-handler-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Replace global singleton
	origPM := GlobalProtocolManager
	GlobalProtocolManager = &ProtocolManager{requestsDir: tempDir}
	defer func() { GlobalProtocolManager = origPM }()

	// Test ProtocolShutdownRequestHandler
	reqArgs := ProtocolShutdownRequestArgs{
		Sender:   "dev1",
		Receiver: "dev2",
		Reason:   "Task done",
	}
	result, err := ProtocolShutdownRequestHandler(nil, reqArgs)
	if err != nil {
		t.Fatalf("ProtocolShutdownRequestHandler failed: %v", err)
	}
	if result.Status != "pending" {
		t.Errorf("expected pending, got %s", result.Status)
	}
	if result.RequestID == "" {
		t.Error("expected non-empty request ID")
	}

	// Test ProtocolShutdownResponseHandler
	respArgs := ProtocolShutdownResponseArgs{
		RequestID: result.RequestID,
		Approved:  true,
		Comment:   "OK",
	}
	respResult, err := ProtocolShutdownResponseHandler(nil, respArgs)
	if err != nil {
		t.Fatalf("ProtocolShutdownResponseHandler failed: %v", err)
	}
	if respResult.Status != "completed" {
		t.Errorf("expected completed, got %s", respResult.Status)
	}
}

func TestIntegration_ProtocolPlanApprovalHandlers(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plan-handler-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	origPM := GlobalProtocolManager
	GlobalProtocolManager = &ProtocolManager{requestsDir: tempDir}
	defer func() { GlobalProtocolManager = origPM }()

	reqArgs := ProtocolPlanApprovalRequestArgs{
		Sender:   "planner",
		Receiver: "lead",
		Plan:     "Step 1: refactor. Step 2: test.",
	}
	result, err := ProtocolPlanApprovalRequestHandler(nil, reqArgs)
	if err != nil {
		t.Fatalf("ProtocolPlanApprovalRequestHandler failed: %v", err)
	}
	if result.Status != "pending" {
		t.Errorf("expected pending, got %s", result.Status)
	}

	respArgs := ProtocolPlanApprovalResponseArgs{
		RequestID: result.RequestID,
		Approved:  true,
		Comment:   "Approved",
	}
	respResult, err := ProtocolPlanApprovalResponseHandler(nil, respArgs)
	if err != nil {
		t.Fatalf("ProtocolPlanApprovalResponseHandler failed: %v", err)
	}
	if respResult.Status != "approved" {
		t.Errorf("expected approved, got %s", respResult.Status)
	}
}

// ─── Autonomy Integration with Tasks ──────────────────────────────────────────

func TestIntegration_AutonomyClaimTaskHandler(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "autonomy-handler-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Override GlobalTaskManager tasksDir
	origTasksDir := GlobalTaskManager.tasksDir
	GlobalTaskManager.tasksDir = tempDir
	defer func() { GlobalTaskManager.tasksDir = origTasksDir }()

	// Create a pending task
	task := &TaskRecord{
		ID:      "task-1",
		Subject: "Fix authentication bug in login module",
		Status:  "pending",
		Owner:   "agent",
	}
	if err := GlobalTaskManager.SaveTask(task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Override GlobalAutonomyManager
	origAM := GlobalAutonomyManager
	GlobalAutonomyManager = &AutonomousManager{state: StateIdle}
	defer func() { GlobalAutonomyManager = origAM }()

	// Test AgentSetStateHandler
	setStateArgs := AgentSetStateArgs{State: "WORK"}
	setStateResult, err := AgentSetStateHandler(nil, setStateArgs)
	if err != nil {
		t.Fatalf("AgentSetStateHandler failed: %v", err)
	}
	if !setStateResult.Success || setStateResult.State != "WORK" {
		t.Errorf("expected success WORK, got %+v", setStateResult)
	}
	if GlobalAutonomyManager.GetState() != StateWork {
		t.Errorf("expected global state WORK, got %s", GlobalAutonomyManager.GetState())
	}

	// Test AgentClaimTaskHandler
	claimArgs := AgentClaimTaskArgs{
		TeammateName: "auth-specialist",
		Keywords:     []string{"authentication"},
	}
	claimResult, err := AgentClaimTaskHandler(nil, claimArgs)
	if err != nil {
		t.Fatalf("AgentClaimTaskHandler failed: %v", err)
	}
	if len(claimResult.ClaimedTasks) != 1 || claimResult.ClaimedTasks[0] != "task-1" {
		t.Errorf("expected task-1 claimed, got %v", claimResult.ClaimedTasks)
	}

	// Verify the task status changed
	refreshed, err := GlobalTaskManager.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if refreshed.Status != "in_progress" || refreshed.Owner != "auth-specialist" {
		t.Errorf("expected in_progress / auth-specialist, got %s / %s", refreshed.Status, refreshed.Owner)
	}
}

func TestIntegration_AgentSetStateHandler_InvalidState(t *testing.T) {
	origAM := GlobalAutonomyManager
	GlobalAutonomyManager = &AutonomousManager{state: StateIdle}
	defer func() { GlobalAutonomyManager = origAM }()

	_, err := AgentSetStateHandler(nil, AgentSetStateArgs{State: "INVALID"})
	if err == nil {
		t.Error("expected error for invalid state, got nil")
	}
}

// ─── Autonomy Auto-Polling Integration ────────────────────────────────────────

func TestIntegration_AutonomyAutoPolling(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "autonomy-poll-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	origTasksDir := GlobalTaskManager.tasksDir
	GlobalTaskManager.tasksDir = tempDir
	defer func() { GlobalTaskManager.tasksDir = origTasksDir }()

	am := &AutonomousManager{
		state: StateIdle,
	}

	// Start auto-polling with 50ms interval
	am.StartAutoPolling("poll-worker", []string{"database"}, 50*time.Millisecond)
	defer am.StopAutoPolling()

	// Create a matching pending task AFTER polling starts
	task := &TaskRecord{
		ID:      "poll-task-1",
		Subject: "Optimize database queries for performance",
		Status:  "pending",
		Owner:   "agent",
	}
	if err := GlobalTaskManager.SaveTask(task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Wait up to 1s for auto-poll to claim
	claimed := false
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		refreshed, err := GlobalTaskManager.GetTask("poll-task-1")
		if err == nil && refreshed.Status == "in_progress" && refreshed.Owner == "poll-worker" {
			claimed = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !claimed {
		t.Error("expected task to be auto-claimed via polling")
	}
}

// ─── IPC Message Handling Integration ─────────────────────────────────────────

func TestIntegration_IPCMessageHandling(t *testing.T) {
	tm, tempDir := setupTeamEnv(t)

	// Register a teammate for IPC tests
	_, _ = tm.RegisterTeammate("ipc-worker", "Dev", "", "executor")

	// Test "message" type: should append to sender's inbox
	teamMsg := TeamMessage{
		Sender:    "ipc-worker",
		Content:   "Hello from IPC",
		Timestamp: float64(time.Now().Unix()),
	}
	payload, _ := json.Marshal(teamMsg)
	msg := IPCMessage{
		Type:    "message",
		From:    "ipc-worker",
		Payload: payload,
	}
	tm.handleIPCMessage(msg)

	// Check the sender's inbox for the forwarded message
	inbox, err := tm.PeekInbox("ipc-worker")
	if err != nil {
		t.Fatalf("PeekInbox failed: %v", err)
	}
	if len(inbox) != 1 || inbox[0].Content != "Hello from IPC" {
		t.Errorf("expected forwarded message in inbox, got: %+v", inbox)
	}

	_ = tempDir // keep reference
}

func TestIntegration_IPCHeartbeatMessage(t *testing.T) {
	tm, _ := setupTeamEnv(t)
	_, _ = tm.RegisterTeammate("heartbeat-worker", "Dev", "", "executor")

	// Send a heartbeat IPC message
	before := time.Now()
	msg := IPCMessage{
		Type: "heartbeat",
		From: "heartbeat-worker",
	}
	tm.handleIPCMessage(msg)

	// Verify LastActive updated and Status set to "working"
	tm.mu.RLock()
	hbWorker, ok := tm.teammates["heartbeat-worker"]
	tm.mu.RUnlock()
	if !ok {
		t.Fatal("heartbeat-worker not found")
	}
	if hbWorker.Status != "working" {
		t.Errorf("expected status working after heartbeat, got %s", hbWorker.Status)
	}
	if hbWorker.LastActive.Before(before) {
		t.Error("expected LastActive to be updated")
	}
}

func TestIntegration_IPCShutdownMessage(t *testing.T) {
	tm, _ := setupTeamEnv(t)
	_, _ = tm.RegisterTeammate("shutdown-worker", "Dev", "", "executor")

	// Send a shutdown IPC message - should not panic
	msg := IPCMessage{
		Type: "shutdown",
		From: "shutdown-worker",
	}
	// Just verify it doesn't panic
	tm.handleIPCMessage(msg)
}

// ─── splitJSONLines helper integration ────────────────────────────────────────

func TestIntegration_SplitJSONLines(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect int
	}{
		{"three lines", "line1\nline2\nline3", 3},
		{"trailing newline", "line1\nline2\n", 2},
		{"empty input", "", 0},
		{"single line no newline", "onlyline", 1},
		{"just newlines", "\n\n\n", 0},
		{"mixed", "a\nb\nc\n", 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := splitJSONLines([]byte(tc.input))
			if len(result) != tc.expect {
				t.Errorf("expected %d lines, got %d (input=%q)", tc.expect, len(result), tc.input)
			}
		})
	}
}

// ─── Tool Handlers via GlobalTeamManager ───────────────────────────────────────

func TestIntegration_SpawnAndListTeammates(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "team-handler-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	_ = os.MkdirAll(filepath.Join(tempDir, "inbox"), 0755)

	origTM := GlobalTeamManager
	GlobalTeamManager = &TeamManager{
		teamDir:     tempDir,
		teammates:   make(map[string]*Teammate),
		activeLoops: make(map[string]chan struct{}),
	}
	defer func() { GlobalTeamManager = origTM }()

	// Spawn a teammate via the handler
	spawnResult, err := SpawnTeammateHandler(nil, SpawnTeammateArgs{
		Name:         "integ-worker",
		Role:         "Tester",
		AgentType:    "reviewer",
		SystemPrompt: "Test all the things.",
	})
	if err != nil {
		t.Fatalf("SpawnTeammateHandler failed: %v", err)
	}
	if !spawnResult.Success {
		t.Errorf("expected success, got %+v", spawnResult)
	}

	// List teammates
	listResult, err := ListTeammatesHandler(nil, ListTeammatesArgs{})
	if err != nil {
		t.Fatalf("ListTeammatesHandler failed: %v", err)
	}
	if len(listResult.Teammates) != 1 || listResult.Teammates[0].Name != "integ-worker" {
		t.Errorf("expected 1 teammate 'integ-worker', got %+v", listResult.Teammates)
	}

	// Clean up the background loop
	GlobalTeamManager.StopTeammateLoop("integ-worker")
}

func TestIntegration_SendMessageAndReadInbox(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "msg-handler-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	_ = os.MkdirAll(filepath.Join(tempDir, "inbox"), 0755)

	origTM := GlobalTeamManager
	GlobalTeamManager = &TeamManager{
		teamDir:   tempDir,
		teammates: make(map[string]*Teammate),
	}
	defer func() { GlobalTeamManager = origTM }()

	// Register recipient first
	_, _ = GlobalTeamManager.RegisterTeammate("recipient", "Dev", "", "")

	// Send message via handler
	sendResult, err := SendMessageHandler(nil, SendMessageArgs{
		Recipient: "recipient",
		Content:   "Hello from handler",
	})
	if err != nil {
		t.Fatalf("SendMessageHandler failed: %v", err)
	}
	if !sendResult.Success {
		t.Errorf("expected success, got %+v", sendResult)
	}

	// Read inbox via handler
	readResult, err := ReadInboxHandler(nil, ReadInboxArgs{Name: "recipient"})
	if err != nil {
		t.Fatalf("ReadInboxHandler failed: %v", err)
	}
	if len(readResult.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(readResult.Messages))
	}
	if readResult.Messages[0].Content != "Hello from handler" {
		t.Errorf("unexpected message content: %s", readResult.Messages[0].Content)
	}
}

func TestIntegration_BroadcastHandler(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "broadcast-handler-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	_ = os.MkdirAll(filepath.Join(tempDir, "inbox"), 0755)

	origTM := GlobalTeamManager
	GlobalTeamManager = &TeamManager{
		teamDir:   tempDir,
		teammates: make(map[string]*Teammate),
	}
	defer func() { GlobalTeamManager = origTM }()

	_, _ = GlobalTeamManager.RegisterTeammate("a", "Dev", "", "")
	_, _ = GlobalTeamManager.RegisterTeammate("b", "Dev", "", "")
	_, _ = GlobalTeamManager.RegisterTeammate("c", "Dev", "", "")

	bcResult, err := BroadcastHandler(nil, BroadcastArgs{Content: "Fire drill!"})
	if err != nil {
		t.Fatalf("BroadcastHandler failed: %v", err)
	}
	if !bcResult.Success {
		t.Errorf("expected success, got %+v", bcResult)
	}

	// Verify all 3 teammates received the broadcast
	for _, name := range []string{"a", "b", "c"} {
		msgs, _ := GlobalTeamManager.ReadAndClearInbox(name)
		if len(msgs) != 1 || msgs[0].Content != "Fire drill!" {
			t.Errorf("%s received unexpected messages: %+v", name, msgs)
		}
	}
}

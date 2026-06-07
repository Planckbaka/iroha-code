package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/session"
)

func TestPoolTypePromptPrefix(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		want     bool // true if expect non-empty prefix
	}{
		{"explore returns prefix", "explore", true},
		{"planner returns prefix", "planner", true},
		{"reviewer returns prefix", "reviewer", true},
		{"executor returns prefix", "executor", true},
		{"researcher returns prefix", "researcher", true},
		{"unknown returns empty", "unknown", false},
		{"empty returns empty", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TypePromptPrefix(tc.typeName)
			if tc.want && got == "" {
				t.Errorf("TypePromptPrefix(%q) = empty, expected non-empty", tc.typeName)
			}
			if !tc.want && got != "" {
				t.Errorf("TypePromptPrefix(%q) = %q, expected empty", tc.typeName, got)
			}
		})
	}

	// Verify known prefixes contain role hints
	for _, typeName := range []string{"explore", "planner", "reviewer", "executor", "researcher"} {
		prefix := TypePromptPrefix(typeName)
		if !strings.Contains(prefix, "agent") {
			t.Errorf("TypePromptPrefix(%q) = %q, expected to contain 'agent'", typeName, prefix)
		}
	}
}

func TestSkillsLoadFromProjectDir(t *testing.T) {
	// Create a temp project skills directory
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".iroha", "skills", "myskill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	manifest := SkillManifest{
		ID:               "test-skill-1",
		Name:             "Test Skill",
		Description:      "A test skill",
		Triggers:         []string{"test"},
		Type:             SkillTypeModelInvoked,
		InstructionsFile: "SKILL.md",
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "skill.json"), data, 0644); err != nil {
		t.Fatalf("failed to write skill.json: %v", err)
	}

	// Load directly via discoverSkillsInDir
	skills, err := discoverSkillsInDir(filepath.Join(tmpDir, ".iroha", "skills"))
	if err != nil {
		t.Fatalf("discoverSkillsInDir failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].ID != "test-skill-1" {
		t.Errorf("expected skill ID test-skill-1, got %s", skills[0].ID)
	}
	if skills[0].BaseDir != skillsDir {
		t.Errorf("expected BaseDir %s, got %s", skillsDir, skills[0].BaseDir)
	}
}

func TestSkillsLoadNonexistentDir(t *testing.T) {
	skills, err := discoverSkillsInDir("/tmp/iroha-nonexistent-dir-12345")
	if err != nil {
		t.Errorf("expected nil error for nonexistent dir, got: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for nonexistent dir, got %d", len(skills))
	}
}

func TestSkillsLoadInvalidManifest(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "bad-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	// Write invalid JSON
	if err := os.WriteFile(filepath.Join(skillsDir, "skill.json"), []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write skill.json: %v", err)
	}

	skills, err := discoverSkillsInDir(tmpDir)
	if err != nil {
		t.Errorf("expected nil error (skip bad manifests), got: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for invalid manifest, got %d", len(skills))
	}
}

func TestSkillsLoadMissingID(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "no-id-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	manifest := map[string]any{
		"name":        "No ID Skill",
		"description": "Missing id field",
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(skillsDir, "skill.json"), data, 0644); err != nil {
		t.Fatalf("failed to write skill.json: %v", err)
	}

	skills, err := discoverSkillsInDir(tmpDir)
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for missing ID, got %d", len(skills))
	}
}

func TestSkillsLoadDefaultTypeAndInstructions(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "defaults-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	// Omit type and instructions_file
	manifest := map[string]any{
		"id":   "defaults-test",
		"name": "Defaults Test",
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(skillsDir, "skill.json"), data, 0644); err != nil {
		t.Fatalf("failed to write skill.json: %v", err)
	}

	skills, err := discoverSkillsInDir(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Type != SkillTypeModelInvoked {
		t.Errorf("expected default type model_invoked, got %s", skills[0].Type)
	}
	if skills[0].InstructionsFile != "SKILL.md" {
		t.Errorf("expected default InstructionsFile SKILL.md, got %s", skills[0].InstructionsFile)
	}
}

func TestSkillsMatchTriggers(t *testing.T) {
	sm := &SkillManager{
		skills: []*SkillManifest{
			{ID: "s1", Name: "Skill 1", Type: SkillTypeModelInvoked, Triggers: []string{"deploy", "ship"}},
			{ID: "s2", Name: "Skill 2", Type: SkillTypeModelInvoked, Triggers: []string{"review"}},
			{ID: "s3", Name: "Skill 3", Type: SkillTypeAlways, Triggers: []string{"deploy"}},
		},
		byID: map[string]*SkillManifest{
			"s1": {ID: "s1", Name: "Skill 1", Type: SkillTypeModelInvoked, Triggers: []string{"deploy", "ship"}},
			"s2": {ID: "s2", Name: "Skill 2", Type: SkillTypeModelInvoked, Triggers: []string{"review"}},
			"s3": {ID: "s3", Name: "Skill 3", Type: SkillTypeAlways, Triggers: []string{"deploy"}},
		},
	}

	matched := sm.MatchTriggers("please deploy the app")
	if len(matched) != 1 {
		t.Errorf("expected 1 match for 'deploy', got %d", len(matched))
	}
	if len(matched) > 0 && matched[0].ID != "s1" {
		t.Errorf("expected s1, got %s", matched[0].ID)
	}

	// Case insensitive
	matched2 := sm.MatchTriggers("DEPLOY NOW")
	if len(matched2) != 1 {
		t.Errorf("expected 1 match for 'DEPLOY', got %d", len(matched2))
	}

	// No match
	matched3 := sm.MatchTriggers("random text no triggers")
	if len(matched3) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matched3))
	}

	// Always-type skills should not match even with trigger word
	matched4 := sm.MatchTriggers("deploy")
	for _, s := range matched4 {
		if s.Type == SkillTypeAlways {
			t.Errorf("always-type skill should not match in MatchTriggers")
		}
	}
}

func TestSkillsGetByID(t *testing.T) {
	sm := &SkillManager{
		byID: map[string]*SkillManifest{
			"my-skill": {ID: "my-skill", Name: "My Skill"},
		},
	}

	s := sm.GetSkillByID("my-skill")
	if s == nil {
		t.Fatal("expected to find my-skill")
	}
	if s.Name != "My Skill" {
		t.Errorf("expected My Skill, got %s", s.Name)
	}

	if sm.GetSkillByID("nonexistent") != nil {
		t.Error("expected nil for unknown ID")
	}
}

func TestSkillsGetAlways(t *testing.T) {
	sm := &SkillManager{
		skills: []*SkillManifest{
			{ID: "a1", Type: SkillTypeAlways},
			{ID: "m1", Type: SkillTypeModelInvoked},
			{ID: "a2", Type: SkillTypeAlways},
		},
	}

	always := sm.GetAlwaysSkills()
	if len(always) != 2 {
		t.Errorf("expected 2 always skills, got %d", len(always))
	}
}

func TestSkillsGetUserInvoked(t *testing.T) {
	sm := &SkillManager{
		skills: []*SkillManifest{
			{ID: "u1", Type: SkillTypeUserInvoked},
			{ID: "m1", Type: SkillTypeModelInvoked},
		},
	}

	user := sm.GetUserInvokedSkills()
	if len(user) != 1 {
		t.Errorf("expected 1 user-invoked skill, got %d", len(user))
	}
}

func TestSkillsAllSkills(t *testing.T) {
	skills := []*SkillManifest{
		{ID: "s1"},
		{ID: "s2"},
	}
	sm := &SkillManager{skills: skills}

	all := sm.AllSkills()
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
	// Verify it returns a copy
	all[0] = nil
	if sm.skills[0] == nil {
		t.Error("AllSkills should return a copy, not reference the original slice")
	}
}

func TestShellBackgroundRunHandler(t *testing.T) {
	// Save and restore global
	origBM := GlobalBackgroundManager
	defer func() { GlobalBackgroundManager = origBM }()

	bm := NewBackgroundManager()
	defer os.RemoveAll(bm.dir)
	GlobalBackgroundManager = bm

	ctx := &mockToolContext{Context: context.Background()}

	result, err := BackgroundRunHandler(ctx, BackgroundRunArgs{Command: "echo hello"})
	if err != nil {
		t.Fatalf("BackgroundRunHandler failed: %v", err)
	}
	if !strings.Contains(result.Message, "started") {
		t.Errorf("expected message to contain 'started', got: %s", result.Message)
	}
}

func TestShellCheckBackgroundHandlerListAll(t *testing.T) {
	origBM := GlobalBackgroundManager
	defer func() { GlobalBackgroundManager = origBM }()

	bm := NewBackgroundManager()
	defer os.RemoveAll(bm.dir)
	GlobalBackgroundManager = bm

	ctx := &mockToolContext{Context: context.Background()}

	// No tasks — should return empty list output
	result, err := CheckBackgroundHandler(ctx, CheckBackgroundArgs{})
	if err != nil {
		t.Fatalf("CheckBackgroundHandler with no tasks failed: %v", err)
	}
	// With no tasks, output should be empty or indicate no tasks
	if result.Output == "" {
		t.Error("expected some output for empty task list")
	}
}

func TestShellCheckBackgroundHandlerSpecificTask(t *testing.T) {
	origBM := GlobalBackgroundManager
	defer func() { GlobalBackgroundManager = origBM }()

	bm := NewBackgroundManager()
	defer os.RemoveAll(bm.dir)
	GlobalBackgroundManager = bm

	ctx := &mockToolContext{Context: context.Background()}

	// Start a background task
	runResult, err := BackgroundRunHandler(ctx, BackgroundRunArgs{Command: "echo test"})
	if err != nil {
		t.Fatalf("BackgroundRunHandler failed: %v", err)
	}

	// Extract task ID from message (format: "Background task XXXXXXXX started: ...")
	parts := strings.Split(runResult.Message, " ")
	if len(parts) < 3 {
		t.Fatalf("unexpected message format: %s", runResult.Message)
	}
	taskID := parts[2]

	// Wait briefly for task to register
	time.Sleep(200 * time.Millisecond)

	// Check specific task
	checkResult, err := CheckBackgroundHandler(ctx, CheckBackgroundArgs{TaskID: taskID})
	if err != nil {
		t.Fatalf("CheckBackgroundHandler failed: %v", err)
	}
	if !strings.Contains(checkResult.Output, taskID) {
		t.Errorf("expected output to contain task ID %s, got: %s", taskID, checkResult.Output)
	}
}

func TestShellCheckBackgroundHandlerUnknownTask(t *testing.T) {
	origBM := GlobalBackgroundManager
	defer func() { GlobalBackgroundManager = origBM }()

	bm := NewBackgroundManager()
	defer os.RemoveAll(bm.dir)
	GlobalBackgroundManager = bm

	ctx := &mockToolContext{Context: context.Background()}

	_, err := CheckBackgroundHandler(ctx, CheckBackgroundArgs{TaskID: "nonexistent-id"})
	if err == nil {
		t.Error("expected error for unknown task ID")
	}
	if !strings.Contains(err.Error(), "unknown task") && !strings.Contains(err.Error(), "check_background") {
		t.Errorf("expected error about unknown task, got: %v", err)
	}
}

func TestSessionStoreList(t *testing.T) {
	tmpDir := t.TempDir()
	delegate := session.InMemoryService()
	svc := NewPersistentSessionService(delegate, tmpDir)

	ctx := context.Background()

	// Create a couple of sessions so List has something to return
	_, err := delegate.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "test-user",
		SessionID: "list-sess-1",
	})
	if err != nil {
		t.Fatalf("failed to create session 1: %v", err)
	}
	_, err = delegate.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "test-user",
		SessionID: "list-sess-2",
	})
	if err != nil {
		t.Fatalf("failed to create session 2: %v", err)
	}

	resp, err := svc.List(ctx, &session.ListRequest{
		AppName: "test-app",
		UserID:  "test-user",
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestSessionStoreListSavedSessionsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	delegate := session.InMemoryService()
	svc := NewPersistentSessionService(delegate, tmpDir)

	sessions, err := svc.ListSavedSessions()
	if err != nil {
		t.Fatalf("ListSavedSessions on empty dir failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestSessionStoreListSavedSessionsWithData(t *testing.T) {
	tmpDir := t.TempDir()
	delegate := session.InMemoryService()
	svc := NewPersistentSessionService(delegate, tmpDir)

	// Write a session JSON file manually
	serialized := SerializedSession{
		ID:             "test-sess-1",
		AppName:        "test-app",
		UserID:         "test-user",
		LastUpdateTime: time.Now(),
		State:          map[string]any{},
		CWD:            "/tmp",
		FirstPrompt:    "hello world",
		TotalTokens:    100,
		TotalCost:      0.0002,
	}
	data, err := json.MarshalIndent(serialized, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "test-sess-1.json"), data, 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	sessions, err := svc.ListSavedSessions()
	if err != nil {
		t.Fatalf("ListSavedSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "test-sess-1" {
		t.Errorf("expected ID test-sess-1, got %s", sessions[0].ID)
	}
	if sessions[0].FirstPrompt != "hello world" {
		t.Errorf("expected FirstPrompt 'hello world', got %s", sessions[0].FirstPrompt)
	}
}

func TestSessionStoreListSavedSessionsSortedByTime(t *testing.T) {
	tmpDir := t.TempDir()
	delegate := session.InMemoryService()
	svc := NewPersistentSessionService(delegate, tmpDir)

	now := time.Now()
	for i, offset := range []time.Duration{0, 2 * time.Hour, 1 * time.Hour} {
		serialized := SerializedSession{
			ID:             fmt.Sprintf("sess-%d", i),
			AppName:        "test-app",
			UserID:         "test-user",
			LastUpdateTime: now.Add(offset),
			State:          map[string]any{},
		}
		data, _ := json.MarshalIndent(serialized, "", "  ")
		if err := os.WriteFile(filepath.Join(tmpDir, serialized.ID+".json"), data, 0644); err != nil {
			t.Fatalf("failed to write session file: %v", err)
		}
	}

	sessions, err := svc.ListSavedSessions()
	if err != nil {
		t.Fatalf("ListSavedSessions failed: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	// Should be sorted descending by LastUpdateTime
	// sess-1 (now+2h) > sess-2 (now+1h) > sess-0 (now)
	if sessions[0].ID != "sess-1" {
		t.Errorf("expected first session to be sess-1 (most recent), got %s", sessions[0].ID)
	}
	if sessions[2].ID != "sess-0" {
		t.Errorf("expected last session to be sess-0 (oldest), got %s", sessions[2].ID)
	}
}

func TestSubagentSpawnHandlerValidation(t *testing.T) {
	// SpawnSubagentHandler delegates to GlobalSubagentManager.RunSubagent
	// which requires a valid ADK runner context. We test that the handler
	// correctly wraps errors from the subagent manager.
	ctx := &mockToolContext{Context: context.Background()}

	// Running with empty spec should return an error
	_, err := SpawnSubagentHandler(ctx, SubagentSpec{
		Name:   "",
		Type:   SubagentTypeExplore,
		Prompt: "test",
	})
	if err == nil {
		t.Error("expected error for empty subagent name, got nil")
	}
	// Error should mention the subagent or name requirement
	errMsg := err.Error()
	if !strings.Contains(errMsg, "subagent") && !strings.Contains(errMsg, "name") {
		t.Errorf("expected error to mention 'subagent' or 'name', got: %v", errMsg)
	}
}

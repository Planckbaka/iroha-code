package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// LoadInstructions - additional edge cases
// ---------------------------------------------------------------------------

func TestLoadInstructions_MissingFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-nofile-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	skill := &SkillManifest{
		ID:               "missing-file-skill",
		Name:             "Missing File",
		InstructionsFile: "NONEXISTENT.md",
		BaseDir:          tmpDir,
	}

	_, err = LoadInstructions(skill)
	if err == nil {
		t.Error("expected error for missing instructions file")
	}
}

func TestLoadInstructions_CustomInstructionsFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-custom-instr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	content := "# Custom Instructions\n\nCustom content here."
	if err := os.WriteFile(filepath.Join(tmpDir, "CUSTOM.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	skill := &SkillManifest{
		ID:               "custom-skill",
		Name:             "Custom",
		InstructionsFile: "CUSTOM.md",
		BaseDir:          tmpDir,
	}

	result, err := LoadInstructions(skill)
	if err != nil {
		t.Fatalf("LoadInstructions failed: %v", err)
	}
	if result != content {
		t.Errorf("expected %q, got %q", content, result)
	}
}

func TestLoadInstructions_EmptyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-empty-file-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "SKILL.md"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	skill := &SkillManifest{
		ID:               "empty-skill",
		Name:             "Empty",
		InstructionsFile: "SKILL.md",
		BaseDir:          tmpDir,
	}

	result, err := LoadInstructions(skill)
	if err != nil {
		t.Fatalf("LoadInstructions failed: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// loadSkillManifest - additional edge cases
// ---------------------------------------------------------------------------

func TestLoadSkillManifest_InvalidJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-badjson-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "skill.json"), []byte("{bad json}"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadSkillManifest_NonexistentFile(t *testing.T) {
	_, err := loadSkillManifest("/nonexistent/skill.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadSkillManifest_EmptyID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-emptyid-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillManifest(t, tmpDir, SkillManifest{
		ID:   "",
		Name: "No ID",
	})

	_, err = loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestLoadSkillManifest_WhitespaceID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-wsid-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillManifest(t, tmpDir, SkillManifest{
		ID:   "   ",
		Name: "Whitespace ID",
	})

	_, err = loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err == nil {
		t.Error("expected error for whitespace-only ID")
	}
}

func TestLoadSkillManifest_EmptyName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-noname-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillManifest(t, tmpDir, SkillManifest{
		ID:   "has-id",
		Name: "",
	})

	_, err = loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err == nil {
		t.Error("expected error for empty Name")
	}
}

func TestLoadSkillManifest_WhitespaceName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-wsname-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillManifest(t, tmpDir, SkillManifest{
		ID:   "has-id",
		Name: "   ",
	})

	_, err = loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err == nil {
		t.Error("expected error for whitespace-only Name")
	}
}

func TestLoadSkillManifest_WithTriggers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-triggers-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillManifest(t, tmpDir, SkillManifest{
		ID:       "trigger-skill",
		Name:     "Trigger Skill",
		Triggers: []string{"deploy", "release"},
		Type:     SkillTypeUserInvoked,
	})

	loaded, err := loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err != nil {
		t.Fatalf("loadSkillManifest failed: %v", err)
	}
	if len(loaded.Triggers) != 2 {
		t.Errorf("expected 2 triggers, got %d", len(loaded.Triggers))
	}
	if loaded.Triggers[0] != "deploy" || loaded.Triggers[1] != "release" {
		t.Errorf("triggers = %v, want [deploy, release]", loaded.Triggers)
	}
}

func TestLoadSkillManifest_WithTags(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-tags-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillManifest(t, tmpDir, SkillManifest{
		ID:   "tagged-skill",
		Name: "Tagged Skill",
		Tags: []string{"dev", "testing"},
	})

	loaded, err := loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err != nil {
		t.Fatalf("loadSkillManifest failed: %v", err)
	}
	if len(loaded.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(loaded.Tags))
	}
}

// ---------------------------------------------------------------------------
// discoverSkillsInDir - additional edge cases
// ---------------------------------------------------------------------------

func TestDiscoverSkillsInDir_EmptyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-emptydir-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	skills, err := discoverSkillsInDir(tmpDir)
	if err != nil {
		t.Fatalf("discoverSkillsInDir failed: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills in empty dir, got %d", len(skills))
	}
}

func TestDiscoverSkillsInDir_MixedValidAndInvalid(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-mixed-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Valid skill
	validDir := filepath.Join(tmpDir, "valid-skill")
	os.MkdirAll(validDir, 0755)
	writeSkillManifest(t, validDir, SkillManifest{
		ID:   "valid-skill",
		Name: "Valid Skill",
		Type: SkillTypeModelInvoked,
	})

	// Invalid skill (no skill.json)
	invalidDir := filepath.Join(tmpDir, "invalid-skill")
	os.MkdirAll(invalidDir, 0755)

	// File (not directory)
	os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("not a skill"), 0644)

	skills, err := discoverSkillsInDir(tmpDir)
	if err != nil {
		t.Fatalf("discoverSkillsInDir failed: %v", err)
	}
	if len(skills) != 1 {
		t.Errorf("expected 1 valid skill, got %d", len(skills))
	}
	if skills[0].ID != "valid-skill" {
		t.Errorf("expected 'valid-skill', got %q", skills[0].ID)
	}
}

// ---------------------------------------------------------------------------
// SkillType constants
// ---------------------------------------------------------------------------

func TestSkillTypeConstants(t *testing.T) {
	if SkillTypeModelInvoked != "model_invoked" {
		t.Errorf("SkillTypeModelInvoked = %q, want 'model_invoked'", SkillTypeModelInvoked)
	}
	if SkillTypeUserInvoked != "user_invoked" {
		t.Errorf("SkillTypeUserInvoked = %q, want 'user_invoked'", SkillTypeUserInvoked)
	}
	if SkillTypeAlways != "always" {
		t.Errorf("SkillTypeAlways = %q, want 'always'", SkillTypeAlways)
	}
}

// ---------------------------------------------------------------------------
// MatchTriggers - additional edge cases
// ---------------------------------------------------------------------------

func TestMatchTriggers_MultipleTriggersMatch(t *testing.T) {
	sm := newTestSkillManager()

	sm.mu.Lock()
	sm.skills = append(sm.skills, &SkillManifest{
		ID:       "multi-trigger",
		Name:     "Multi Trigger",
		Triggers: []string{"alpha", "beta", "gamma"},
		Type:     SkillTypeModelInvoked,
	})
	sm.byID["multi-trigger"] = sm.skills[0]
	sm.mu.Unlock()

	// Should match on first matching trigger and stop
	matched := sm.MatchTriggers("I want to use beta mode")
	if len(matched) != 1 || matched[0].ID != "multi-trigger" {
		t.Errorf("expected multi-trigger match, got %v", matched)
	}
}

func TestMatchTriggers_EmptyPrompt(t *testing.T) {
	sm := newTestSkillManager()

	sm.mu.Lock()
	sm.skills = append(sm.skills, &SkillManifest{
		ID:       "some-skill",
		Name:     "Some",
		Triggers: []string{"test"},
		Type:     SkillTypeModelInvoked,
	})
	sm.byID["some-skill"] = sm.skills[0]
	sm.mu.Unlock()

	matched := sm.MatchTriggers("")
	if len(matched) != 0 {
		t.Errorf("expected 0 matches for empty prompt, got %d", len(matched))
	}
}

func TestMatchTriggers_NoTriggers(t *testing.T) {
	sm := newTestSkillManager()

	sm.mu.Lock()
	sm.skills = append(sm.skills, &SkillManifest{
		ID:       "no-triggers",
		Name:     "No Triggers",
		Triggers: nil,
		Type:     SkillTypeModelInvoked,
	})
	sm.byID["no-triggers"] = sm.skills[0]
	sm.mu.Unlock()

	matched := sm.MatchTriggers("test anything")
	if len(matched) != 0 {
		t.Errorf("expected 0 matches for skill with no triggers, got %d", len(matched))
	}
}

// ---------------------------------------------------------------------------
// GetSkillByID - not found
// ---------------------------------------------------------------------------

func TestGetSkillByID_NotFound(t *testing.T) {
	sm := newTestSkillManager()
	result := sm.GetSkillByID("does-not-exist")
	if result != nil {
		t.Error("expected nil for nonexistent ID")
	}
}

// ---------------------------------------------------------------------------
// AllSkills - empty manager
// ---------------------------------------------------------------------------

func TestAllSkills_EmptyManager(t *testing.T) {
	sm := newTestSkillManager()
	all := sm.AllSkills()
	if len(all) != 0 {
		t.Errorf("expected empty slice, got %v", all)
	}
}

// ---------------------------------------------------------------------------
// GetAlwaysSkills - no always skills
// ---------------------------------------------------------------------------

func TestGetAlwaysSkills_None(t *testing.T) {
	sm := newTestSkillManager()

	sm.mu.Lock()
	sm.skills = append(sm.skills, &SkillManifest{
		ID:   "model-skill",
		Name: "Model",
		Type: SkillTypeModelInvoked,
	})
	sm.byID["model-skill"] = sm.skills[0]
	sm.mu.Unlock()

	always := sm.GetAlwaysSkills()
	if len(always) != 0 {
		t.Errorf("expected 0 always skills, got %d", len(always))
	}
}

// ---------------------------------------------------------------------------
// GetUserInvokedSkills - no user skills
// ---------------------------------------------------------------------------

func TestGetUserInvokedSkills_None(t *testing.T) {
	sm := newTestSkillManager()

	sm.mu.Lock()
	sm.skills = append(sm.skills, &SkillManifest{
		ID:   "model-skill",
		Name: "Model",
		Type: SkillTypeModelInvoked,
	})
	sm.byID["model-skill"] = sm.skills[0]
	sm.mu.Unlock()

	user := sm.GetUserInvokedSkills()
	if len(user) != 0 {
		t.Errorf("expected 0 user_invoked skills, got %d", len(user))
	}
}

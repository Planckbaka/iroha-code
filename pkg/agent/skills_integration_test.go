package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_Skills_LoadSkillsFromTempDirs(t *testing.T) {
	// Create global skills directory
	tmpHome, err := os.MkdirTemp("", "iroha-skills-home-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	globalSkillsDir := filepath.Join(tmpHome, ".iroha", "skills")
	skill1Dir := filepath.Join(globalSkillsDir, "skill-alpha")
	if err := os.MkdirAll(skill1Dir, 0755); err != nil {
		t.Fatal(err)
	}

	manifest1 := SkillManifest{
		ID:       "skill-alpha",
		Name:     "Alpha Skill",
		Type:     SkillTypeModelInvoked,
		Triggers: []string{"alpha"},
	}
	writeSkillManifest(t, skill1Dir, manifest1)

	// Create project skills directory
	tmpProject, err := os.MkdirTemp("", "iroha-skills-project-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpProject)

	projectSkillsDir := filepath.Join(tmpProject, ".iroha", "skills")
	skill2Dir := filepath.Join(projectSkillsDir, "skill-beta")
	if err := os.MkdirAll(skill2Dir, 0755); err != nil {
		t.Fatal(err)
	}

	manifest2 := SkillManifest{
		ID:       "skill-beta",
		Name:     "Beta Skill",
		Type:     SkillTypeUserInvoked,
		Triggers: []string{"beta"},
	}
	writeSkillManifest(t, skill2Dir, manifest2)

	// Test discovery on each dir individually
	globalSkills, err := discoverSkillsInDir(globalSkillsDir)
	if err != nil {
		t.Fatalf("discoverSkillsInDir global failed: %v", err)
	}
	if len(globalSkills) != 1 || globalSkills[0].ID != "skill-alpha" {
		t.Errorf("expected 1 global skill (skill-alpha), got %v", globalSkills)
	}

	projectSkills, err := discoverSkillsInDir(projectSkillsDir)
	if err != nil {
		t.Fatalf("discoverSkillsInDir project failed: %v", err)
	}
	if len(projectSkills) != 1 || projectSkills[0].ID != "skill-beta" {
		t.Errorf("expected 1 project skill (skill-beta), got %v", projectSkills)
	}
}

func TestIntegration_Skills_ProjectOverridesGlobal(t *testing.T) {
	sm := newTestSkillManager()

	// Manually simulate LoadSkills dedup logic
	global := &SkillManifest{
		ID:   "shared-skill",
		Name: "Global Version",
		Type: SkillTypeModelInvoked,
	}
	global.BaseDir = "/global/shared-skill"

	project := &SkillManifest{
		ID:   "shared-skill",
		Name: "Project Version",
		Type: SkillTypeModelInvoked,
	}
	project.BaseDir = "/project/shared-skill"

	// Simulate the merge: project overrides global
	sm.mu.Lock()
	sm.byID[global.ID] = global
	sm.byID[project.ID] = project // overwrites global
	sm.skills = []*SkillManifest{project}
	sm.mu.Unlock()

	skill := sm.GetSkillByID("shared-skill")
	if skill == nil {
		t.Fatal("expected to find shared-skill")
	}
	if skill.Name != "Project Version" {
		t.Errorf("expected 'Project Version', got %q", skill.Name)
	}
	if skill.BaseDir != "/project/shared-skill" {
		t.Errorf("expected project BaseDir, got %q", skill.BaseDir)
	}
}

func TestIntegration_Skills_DiscoverSkillsInDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-disc-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create two skill subdirectories with valid manifests
	for _, name := range []string{"skill-a", "skill-b"} {
		skillDir := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		manifest := SkillManifest{
			ID:   name,
			Name: "Skill " + strings.ToUpper(name),
			Type: SkillTypeModelInvoked,
		}
		writeSkillManifest(t, skillDir, manifest)
	}

	// Create a non-directory entry (should be skipped)
	if err := os.WriteFile(filepath.Join(tmpDir, "not-a-dir.txt"), []byte("text"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a directory without skill.json (should be skipped)
	emptyDir := filepath.Join(tmpDir, "empty-skill")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatal(err)
	}

	skills, err := discoverSkillsInDir(tmpDir)
	if err != nil {
		t.Fatalf("discoverSkillsInDir failed: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	ids := map[string]bool{}
	for _, s := range skills {
		ids[s.ID] = true
		if s.BaseDir != filepath.Join(tmpDir, s.ID) {
			t.Errorf("expected BaseDir to be %q, got %q", filepath.Join(tmpDir, s.ID), s.BaseDir)
		}
	}
	if !ids["skill-a"] || !ids["skill-b"] {
		t.Errorf("expected skill-a and skill-b, got ids: %v", ids)
	}
}

func TestIntegration_Skills_DiscoverSkillsInDirNonExistent(t *testing.T) {
	skills, err := discoverSkillsInDir("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Errorf("expected nil error for nonexistent dir, got: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills for nonexistent dir, got: %v", skills)
	}
}

func TestIntegration_Skills_LoadManifestValid(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-manifest-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manifest := SkillManifest{
		ID:       "test-skill",
		Name:     "Test Skill",
		Type:     SkillTypeModelInvoked,
		Triggers: []string{"test"},
		Tags:     []string{"testing"},
	}
	writeSkillManifest(t, tmpDir, manifest)

	loaded, err := loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err != nil {
		t.Fatalf("loadSkillManifest failed: %v", err)
	}
	if loaded.ID != "test-skill" {
		t.Errorf("expected ID 'test-skill', got %q", loaded.ID)
	}
	if loaded.Name != "Test Skill" {
		t.Errorf("expected Name 'Test Skill', got %q", loaded.Name)
	}
	if loaded.Type != SkillTypeModelInvoked {
		t.Errorf("expected type model_invoked, got %q", loaded.Type)
	}
}

func TestIntegration_Skills_LoadManifestMissingID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-no-id-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write manifest without id
	data, _ := json.Marshal(map[string]string{"name": "No ID Skill"})
	if err := os.WriteFile(filepath.Join(tmpDir, "skill.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err = loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err == nil {
		t.Error("expected error for missing id")
	}
	if !strings.Contains(err.Error(), "missing required field: id") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIntegration_Skills_LoadManifestMissingName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-no-name-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write manifest without name
	data, _ := json.Marshal(map[string]string{"id": "no-name-skill"})
	if err := os.WriteFile(filepath.Join(tmpDir, "skill.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err = loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err == nil {
		t.Error("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "missing required field: name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIntegration_Skills_LoadManifestDefaultsType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-default-type-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write manifest without type field
	manifest := map[string]any{
		"id":   "default-type-skill",
		"name": "Default Type",
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(tmpDir, "skill.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err != nil {
		t.Fatalf("loadSkillManifest failed: %v", err)
	}
	if loaded.Type != SkillTypeModelInvoked {
		t.Errorf("expected default type 'model_invoked', got %q", loaded.Type)
	}
}

func TestIntegration_Skills_LoadManifestDefaultsInstructionsFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-default-instr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manifest := SkillManifest{
		ID:   "default-instr-skill",
		Name: "Default Instructions",
	}
	writeSkillManifest(t, tmpDir, manifest)

	loaded, err := loadSkillManifest(filepath.Join(tmpDir, "skill.json"))
	if err != nil {
		t.Fatalf("loadSkillManifest failed: %v", err)
	}
	if loaded.InstructionsFile != "SKILL.md" {
		t.Errorf("expected default InstructionsFile 'SKILL.md', got %q", loaded.InstructionsFile)
	}
}

func TestIntegration_Skills_LoadInstructions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-instr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create SKILL.md
	content := "# Test Skill\n\nThis is a test skill instruction."
	if err := os.WriteFile(filepath.Join(tmpDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	skill := &SkillManifest{
		ID:               "test-skill",
		Name:             "Test",
		InstructionsFile: "SKILL.md",
		BaseDir:          tmpDir,
	}

	instructions, err := LoadInstructions(skill)
	if err != nil {
		t.Fatalf("LoadInstructions failed: %v", err)
	}
	if instructions != content {
		t.Errorf("expected %q, got %q", content, instructions)
	}
}

func TestIntegration_Skills_LoadInstructionsPathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "iroha-skill-traversal-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a skill with instructions_file that escapes BaseDir
	skill := &SkillManifest{
		ID:               "traversal-skill",
		Name:             "Traversal",
		InstructionsFile: "../../etc/passwd",
		BaseDir:          filepath.Join(tmpDir, "skills", "traversal-skill"),
	}
	if err := os.MkdirAll(skill.BaseDir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err = LoadInstructions(skill)
	if err == nil {
		t.Error("expected error for path traversal attempt")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIntegration_Skills_LoadInstructionsNoBaseDir(t *testing.T) {
	skill := &SkillManifest{
		ID:               "no-basedir-skill",
		Name:             "No BaseDir",
		InstructionsFile: "SKILL.md",
		BaseDir:          "",
	}

	_, err := LoadInstructions(skill)
	if err == nil {
		t.Error("expected error when BaseDir is empty")
	}
	if !strings.Contains(err.Error(), "no base directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

// writeSkillManifest is a helper to create a valid skill.json in a directory.
func writeSkillManifest(t *testing.T, dir string, manifest SkillManifest) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), data, 0644); err != nil {
		t.Fatalf("failed to write skill.json: %v", err)
	}
}

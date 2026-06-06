package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name     string
		manifest PluginManifest
		wantErr  bool
	}{
		{"Valid", PluginManifest{ID: "my-plugin", Name: "My Plugin", Version: "1.0.0"}, false},
		{"Empty ID", PluginManifest{ID: "", Name: "My Plugin"}, true},
		{"Invalid ID with slash", PluginManifest{ID: "my/plugin", Name: "My Plugin", Version: "1.0.0"}, true},
		{"Invalid ID with double underscore", PluginManifest{ID: "my__plugin", Name: "My Plugin", Version: "1.0.0"}, true},
		{"Empty Name", PluginManifest{ID: "my-plugin", Name: "", Version: "1.0.0"}, true},
		{"Invalid version", PluginManifest{ID: "my-plugin", Name: "My Plugin", Version: "not-semver"}, true},
		{"Missing version", PluginManifest{ID: "my-plugin", Name: "My Plugin"}, true},
		{"Valid with v prefix", PluginManifest{ID: "my-plugin", Name: "My Plugin", Version: "v1.2.3"}, false},
		{"Valid prerelease", PluginManifest{ID: "my-plugin", Name: "My Plugin", Version: "1.0.0-alpha"}, false},
		{"ID with underscores", PluginManifest{ID: "my_plugin", Name: "My Plugin", Version: "1.0.0"}, false},
		{"ID starting with digit", PluginManifest{ID: "0plugin", Name: "My Plugin", Version: "1.0.0"}, false},
		{"ID starting with hyphen", PluginManifest{ID: "-plugin", Name: "My Plugin", Version: "1.0.0"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateManifest(&tt.manifest)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPluginManagerGetPluginsAndGetByID(t *testing.T) {
	pm := &PluginManager{}

	// Manually set plugins on the manager for testing GetPlugins/GetPluginByID
	manifest := &PluginManifest{
		ID:      "test-plugin",
		Name:    "Test Plugin",
		Version: "1.0.0",
	}
	pm.mu.Lock()
	pm.plugins = append(pm.plugins, manifest)
	pm.mu.Unlock()

	plugins := pm.GetPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].ID != "test-plugin" {
		t.Errorf("expected plugin ID 'test-plugin', got %q", plugins[0].ID)
	}

	// Test GetPluginByID
	p := pm.GetPluginByID("test-plugin")
	if p == nil {
		t.Fatal("expected to find test-plugin")
	}
	if p.Name != "Test Plugin" {
		t.Errorf("expected name 'Test Plugin', got %q", p.Name)
	}

	// Test not found
	p = pm.GetPluginByID("nonexistent")
	if p != nil {
		t.Error("expected nil for nonexistent plugin")
	}
}

func TestDiscoverPluginsFromDir(t *testing.T) {
	// Create a temp plugin directory with manifest
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifest := PluginManifest{
		ID:      "test-plugin",
		Name:    "Test Plugin",
		Version: "1.0.0",
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Discover from temp dir
	plugins, err := DiscoverPlugins(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverPlugins() error = %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].ID != "test-plugin" {
		t.Errorf("expected plugin ID 'test-plugin', got %q", plugins[0].ID)
	}
}

func TestDiscoverPluginsEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	plugins, err := DiscoverPlugins(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverPlugins() error = %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins in empty dir, got %d", len(plugins))
	}
}

func TestDiscoverPluginsNonexistentDir(t *testing.T) {
	plugins, err := DiscoverPlugins("/nonexistent/path/12345")
	if err != nil {
		t.Fatalf("DiscoverPlugins() error = %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected nil for nonexistent dir, got %d plugins", len(plugins))
	}
}

func TestMigratePluginsConfig(t *testing.T) {
	result := MigratePluginsConfig(PluginsConfig{
		MCPServers: map[string]MCPServerConfig{
			"legacy-mcp": {Command: "node", Args: []string{"server.js"}},
		},
	})
	if result == nil {
		t.Fatal("expected non-nil migrated manifest")
	}
	if result.ID != "migrated-legacy" {
		t.Errorf("expected ID 'migrated-legacy', got %q", result.ID)
	}
	if result.Name != "Migrated Legacy Plugins" {
		t.Errorf("expected name 'Migrated Legacy Plugins', got %q", result.Name)
	}
	if len(result.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(result.MCPServers))
	}
}

func TestMigratePluginsConfigEmpty(t *testing.T) {
	result := MigratePluginsConfig(PluginsConfig{})
	if result != nil {
		t.Error("expected nil for empty config")
	}
}

func TestPluginManagerMergeMCPServers(t *testing.T) {
	pm := &PluginManager{}
	pm.mu.Lock()
	pm.plugins = []*PluginManifest{
		{
			ID:      "plug-a",
			Name:    "Plugin A",
			Version: "1.0.0",
			MCPServers: map[string]MCPServerConfig{
				"server1": {Command: "cmd1"},
			},
		},
	}
	pm.mu.Unlock()

	merged := pm.MergeMCPServers()
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged server, got %d", len(merged))
	}
	key := "plug-a__server1"
	if _, ok := merged[key]; !ok {
		t.Errorf("expected key %q in merged servers", key)
	}
}

func TestPluginManagerMergeHooks(t *testing.T) {
	pm := &PluginManager{}
	pm.mu.Lock()
	pm.plugins = []*PluginManifest{
		{
			ID:      "hook-plug",
			Name:    "Hook Plugin",
			Version: "1.0.0",
			Hooks: map[string][]HookDef{
				"pre_commit": {{Command: "lint.sh"}},
			},
		},
	}
	pm.mu.Unlock()

	merged := pm.MergeHooks()
	if len(merged) != 1 {
		t.Fatalf("expected 1 hook event, got %d", len(merged))
	}
	hooks, ok := merged["pre_commit"]
	if !ok {
		t.Fatal("expected 'pre_commit' in merged hooks")
	}
	if len(hooks) != 1 || hooks[0].Command != "lint.sh" {
		t.Errorf("unexpected hooks content: %+v", hooks)
	}
}

// ---------------------------------------------------------------------------
// Additional ValidateManifest cases
// ---------------------------------------------------------------------------

func TestValidateManifest_WhitespaceID(t *testing.T) {
	m := &PluginManifest{ID: "   ", Name: "My Plugin", Version: "1.0.0"}
	if err := ValidateManifest(m); err == nil {
		t.Error("expected error for whitespace-only ID")
	}
}

func TestValidateManifest_WhitespaceName(t *testing.T) {
	m := &PluginManifest{ID: "my-plugin", Name: "   ", Version: "1.0.0"}
	if err := ValidateManifest(m); err == nil {
		t.Error("expected error for whitespace-only Name")
	}
}

func TestValidateManifest_SemverWithBuild(t *testing.T) {
	m := &PluginManifest{ID: "my-plugin", Name: "My Plugin", Version: "1.0.0+build.123"}
	if err := ValidateManifest(m); err != nil {
		t.Errorf("expected valid for semver with build metadata, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// LoadPluginManifest additional cases
// ---------------------------------------------------------------------------

func TestLoadPluginManifest_NonexistentFile(t *testing.T) {
	_, err := LoadPluginManifest("/nonexistent/plugin.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadPluginManifest_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "plugin.json")
	os.WriteFile(manifestFile, []byte("{bad json}"), 0644)

	_, err := LoadPluginManifest(manifestFile)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadPluginManifest_InvalidManifest(t *testing.T) {
	tmpDir := t.TempDir()
	data, _ := json.Marshal(map[string]string{"name": "No ID Plugin"})
	manifestFile := filepath.Join(tmpDir, "plugin.json")
	os.WriteFile(manifestFile, data, 0644)

	_, err := LoadPluginManifest(manifestFile)
	if err == nil {
		t.Error("expected error for manifest missing required fields")
	}
}

func TestLoadPluginManifest_WithMCPServers(t *testing.T) {
	tmpDir := t.TempDir()
	manifest := PluginManifest{
		ID:      "mcp-plugin",
		Name:    "MCP Plugin",
		Version: "1.0.0",
		MCPServers: map[string]MCPServerConfig{
			"my-server": {Command: "node", Args: []string{"server.js"}},
		},
	}
	data, _ := json.Marshal(manifest)
	manifestFile := filepath.Join(tmpDir, "plugin.json")
	os.WriteFile(manifestFile, data, 0644)

	loaded, err := LoadPluginManifest(manifestFile)
	if err != nil {
		t.Fatalf("LoadPluginManifest failed: %v", err)
	}
	if len(loaded.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(loaded.MCPServers))
	}
	if loaded.MCPServers["my-server"].Command != "node" {
		t.Errorf("command = %q, want 'node'", loaded.MCPServers["my-server"].Command)
	}
}

func TestLoadPluginManifest_WithHooks(t *testing.T) {
	tmpDir := t.TempDir()
	manifest := PluginManifest{
		ID:      "hook-plugin",
		Name:    "Hook Plugin",
		Version: "1.0.0",
		Hooks: map[string][]HookDef{
			"PreToolUse": {{Command: "echo pre-hook"}},
		},
	}
	data, _ := json.Marshal(manifest)
	manifestFile := filepath.Join(tmpDir, "plugin.json")
	os.WriteFile(manifestFile, data, 0644)

	loaded, err := LoadPluginManifest(manifestFile)
	if err != nil {
		t.Fatalf("LoadPluginManifest failed: %v", err)
	}
	if len(loaded.Hooks["PreToolUse"]) != 1 {
		t.Errorf("expected 1 hook, got %d", len(loaded.Hooks["PreToolUse"]))
	}
}

func TestLoadPluginManifest_WithSkills(t *testing.T) {
	tmpDir := t.TempDir()
	manifest := PluginManifest{
		ID:      "skill-plugin",
		Name:    "Skill Plugin",
		Version: "1.0.0",
		Skills:  []string{"skill-a", "skill-b"},
	}
	data, _ := json.Marshal(manifest)
	manifestFile := filepath.Join(tmpDir, "plugin.json")
	os.WriteFile(manifestFile, data, 0644)

	loaded, err := LoadPluginManifest(manifestFile)
	if err != nil {
		t.Fatalf("LoadPluginManifest failed: %v", err)
	}
	if len(loaded.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(loaded.Skills))
	}
}

func TestLoadPluginManifest_WithPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	manifest := PluginManifest{
		ID:          "perm-plugin",
		Name:        "Perm Plugin",
		Version:     "1.0.0",
		Permissions: []string{"read:files", "write:files"},
	}
	data, _ := json.Marshal(manifest)
	manifestFile := filepath.Join(tmpDir, "plugin.json")
	os.WriteFile(manifestFile, data, 0644)

	loaded, err := LoadPluginManifest(manifestFile)
	if err != nil {
		t.Fatalf("LoadPluginManifest failed: %v", err)
	}
	if len(loaded.Permissions) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(loaded.Permissions))
	}
}

// ---------------------------------------------------------------------------
// MigratePluginsConfig additional cases
// ---------------------------------------------------------------------------

func TestMigratePluginsConfig_NilMCPServers(t *testing.T) {
	result := MigratePluginsConfig(PluginsConfig{})
	if result != nil {
		t.Error("expected nil for nil MCPServers")
	}
}

// ---------------------------------------------------------------------------
// DiscoverPlugins additional cases
// ---------------------------------------------------------------------------

func TestDiscoverPlugins_InvalidManifest(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "bad-plugin")
	os.MkdirAll(pluginDir, 0755)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte("{invalid}"), 0644)

	plugins, err := DiscoverPlugins(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverPlugins failed: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins (invalid skipped), got %d", len(plugins))
	}
}

func TestDiscoverPlugins_SkipsNonDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file (not a directory) - should be skipped
	os.WriteFile(filepath.Join(tmpDir, "not-a-plugin.txt"), []byte("text"), 0644)

	plugins, err := DiscoverPlugins(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverPlugins failed: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(plugins))
	}
}

func TestDiscoverPlugins_MultiplePlugins(t *testing.T) {
	tmpDir := t.TempDir()
	for _, name := range []string{"plugin-a", "plugin-b", "plugin-c"} {
		pluginDir := filepath.Join(tmpDir, name)
		os.MkdirAll(pluginDir, 0755)
		manifest := PluginManifest{ID: name, Name: name, Version: "1.0.0"}
		data, _ := json.Marshal(manifest)
		os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0644)
	}

	plugins, err := DiscoverPlugins(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverPlugins failed: %v", err)
	}
	if len(plugins) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(plugins))
	}
}

// ---------------------------------------------------------------------------
// PluginManager additional method tests
// ---------------------------------------------------------------------------

func TestPluginManager_GetPlugins_ReturnsCopy(t *testing.T) {
	pm := &PluginManager{}
	pm.mu.Lock()
	pm.plugins = []*PluginManifest{
		{ID: "p1", Name: "P1", Version: "1.0.0"},
	}
	pm.mu.Unlock()

	plugins := pm.GetPlugins()
	plugins[0] = nil

	inner := pm.GetPlugins()
	if inner[0] == nil {
		t.Error("GetPlugins should return a copy, not internal slice")
	}
}

func TestPluginManager_GetPlugins_Empty(t *testing.T) {
	pm := &PluginManager{}
	plugins := pm.GetPlugins()
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(plugins))
	}
}

func TestPluginManager_MergeMCPServers_Empty(t *testing.T) {
	pm := &PluginManager{}
	merged := pm.MergeMCPServers()
	if len(merged) != 0 {
		t.Errorf("expected 0 merged servers, got %d", len(merged))
	}
}

func TestPluginManager_MergeMCPServers_MultiplePlugins(t *testing.T) {
	pm := &PluginManager{}
	pm.mu.Lock()
	pm.plugins = []*PluginManifest{
		{
			ID: "plug-a", Name: "A", Version: "1.0.0",
			MCPServers: map[string]MCPServerConfig{"s1": {Command: "cmd1"}},
		},
		{
			ID: "plug-b", Name: "B", Version: "1.0.0",
			MCPServers: map[string]MCPServerConfig{"s2": {Command: "cmd2"}},
		},
	}
	pm.mu.Unlock()

	merged := pm.MergeMCPServers()
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged servers, got %d", len(merged))
	}
	if _, ok := merged["plug-a__s1"]; !ok {
		t.Error("expected key 'plug-a__s1'")
	}
	if _, ok := merged["plug-b__s2"]; !ok {
		t.Error("expected key 'plug-b__s2'")
	}
}

func TestPluginManager_MergeHooks_Empty(t *testing.T) {
	pm := &PluginManager{}
	merged := pm.MergeHooks()
	if len(merged) != 0 {
		t.Errorf("expected 0 merged hooks, got %d", len(merged))
	}
}

func TestPluginManager_MergeHooks_NoHookPlugins(t *testing.T) {
	pm := &PluginManager{}
	pm.mu.Lock()
	pm.plugins = []*PluginManifest{
		{ID: "no-hooks", Name: "No Hooks", Version: "1.0.0"},
	}
	pm.mu.Unlock()

	merged := pm.MergeHooks()
	if len(merged) != 0 {
		t.Errorf("expected 0 merged hooks for plugin without hooks, got %d", len(merged))
	}
}

func TestPluginManager_MergeHooks_MultiplePluginsMerge(t *testing.T) {
	pm := &PluginManager{}
	pm.mu.Lock()
	pm.plugins = []*PluginManifest{
		{
			ID: "plug-a", Name: "A", Version: "1.0.0",
			Hooks: map[string][]HookDef{
				"PreToolUse":  {{Command: "hook-a-pre"}},
				"PostToolUse": {{Command: "hook-a-post"}},
			},
		},
		{
			ID: "plug-b", Name: "B", Version: "1.0.0",
			Hooks: map[string][]HookDef{
				"PreToolUse": {{Command: "hook-b-pre"}},
			},
		},
	}
	pm.mu.Unlock()

	merged := pm.MergeHooks()
	if len(merged["PreToolUse"]) != 2 {
		t.Errorf("expected 2 PreToolUse hooks, got %d", len(merged["PreToolUse"]))
	}
	if len(merged["PostToolUse"]) != 1 {
		t.Errorf("expected 1 PostToolUse hook, got %d", len(merged["PostToolUse"]))
	}
}

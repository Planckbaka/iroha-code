package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRebuildToolPool(t *testing.T) {
	origRouter := GlobalMCPRouter
	defer func() {
		GlobalMCPRouter = origRouter
		toolPoolVersion = 0
	}()

	GlobalMCPRouter = nil
	count, err := RebuildToolPool()
	if err != nil {
		t.Errorf("expected nil error with nil router, got: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tools with nil router, got %d", count)
	}

	GlobalMCPRouter = &MCPToolRouter{
		clients: make(map[string]*MCPClient),
	}
	count, err = RebuildToolPool()
	if err != nil {
		t.Errorf("expected nil error with empty router, got: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tools with empty router, got %d", count)
	}
}

func TestRebuildToolPoolWithMock(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "iroha-toolpool-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	goClaudeDir := filepath.Join(tempDir, ".iroha")
	_ = os.MkdirAll(goClaudeDir, 0755)

	pluginsJson := fmt.Sprintf(`{
		"mcpServers": {
			"mock": {
				"command": "%s",
				"args": ["-test.run=TestHelperProcess"],
				"env": ["GO_WANT_HELPER_PROCESS=1"]
			}
		}
	}`, strings.ReplaceAll(os.Args[0], `\`, `\\`))

	err = os.WriteFile(filepath.Join(goClaudeDir, "plugins.json"), []byte(pluginsJson), 0644)
	if err != nil {
		t.Fatalf("failed to write plugins.json: %v", err)
	}

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer func() { _ = os.Chdir(oldWd) }()

	origRouter := GlobalMCPRouter
	toolPoolVersion = 0
	defer func() {
		GlobalMCPRouter = origRouter
		toolPoolVersion = 0
	}()

	router := &MCPToolRouter{
		clients: make(map[string]*MCPClient),
	}
	defer router.CloseAll()
	GlobalMCPRouter = router

	_ = router.LoadAndStartPlugins()
	time.Sleep(100 * time.Millisecond)

	count, err := RebuildToolPool()
	if err != nil {
		t.Errorf("RebuildToolPool failed: %v", err)
	}
	if count == 0 {
		t.Error("expected >0 tools after rebuild with mock server")
	}
	if ToolPoolVersion() != 1 {
		t.Errorf("expected toolPoolVersion=1, got %d", ToolPoolVersion())
	}
}

func TestCheckPluginsFileChanged(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "iroha-plugins-changed-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	goClaudeDir := filepath.Join(tempDir, ".iroha")
	_ = os.MkdirAll(goClaudeDir, 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer func() { _ = os.Chdir(oldWd) }()

	pluginsMtimeInit = false
	pluginsMtime = time.Time{}

	if CheckPluginsFileChanged() {
		t.Error("expected false when plugins.json does not exist")
	}

	cfgPath := filepath.Join(goClaudeDir, "plugins.json")
	err = os.WriteFile(cfgPath, []byte(`{"mcpServers":{}}`), 0644)
	if err != nil {
		t.Fatalf("failed to write plugins.json: %v", err)
	}

	// File appeared after baseline was set to "no file" — this is a change
	if !CheckPluginsFileChanged() {
		t.Error("expected true when file appears after no-file baseline")
	}

	if CheckPluginsFileChanged() {
		t.Error("expected false when file not modified")
	}

	time.Sleep(10 * time.Millisecond)
	err = os.WriteFile(cfgPath, []byte(`{"mcpServers":{"x":{"command":"echo"}}}`), 0644)
	if err != nil {
		t.Fatalf("failed to rewrite plugins.json: %v", err)
	}

	if !CheckPluginsFileChanged() {
		t.Error("expected true after file modification")
	}

	pluginsMtimeInit = false
	pluginsMtime = time.Time{}
}

func TestPluginsFileNoChange(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "iroha-plugins-nochange-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	goClaudeDir := filepath.Join(tempDir, ".iroha")
	_ = os.MkdirAll(goClaudeDir, 0755)

	cfgPath := filepath.Join(goClaudeDir, "plugins.json")
	err = os.WriteFile(cfgPath, []byte(`{"mcpServers":{}}`), 0644)
	if err != nil {
		t.Fatalf("failed to write plugins.json: %v", err)
	}

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer func() { _ = os.Chdir(oldWd) }()

	pluginsMtimeInit = false
	pluginsMtime = time.Time{}

	if CheckPluginsFileChanged() {
		t.Error("first call should return false (seeding baseline)")
	}

	for i := 0; i < 5; i++ {
		if CheckPluginsFileChanged() {
			t.Errorf("call %d: expected false, got true", i+1)
		}
	}

	pluginsMtimeInit = false
	pluginsMtime = time.Time{}
}

func TestToolPoolVersionIncrements(t *testing.T) {
	origRouter := GlobalMCPRouter
	toolPoolVersion = 0
	defer func() {
		GlobalMCPRouter = origRouter
		toolPoolVersion = 0
	}()

	GlobalMCPRouter = &MCPToolRouter{
		clients: make(map[string]*MCPClient),
	}

	if ToolPoolVersion() != 0 {
		t.Errorf("expected initial version 0, got %d", ToolPoolVersion())
	}

	_, _ = RebuildToolPool()
	if ToolPoolVersion() != 1 {
		t.Errorf("expected version 1 after first rebuild, got %d", ToolPoolVersion())
	}

	_, _ = RebuildToolPool()
	if ToolPoolVersion() != 2 {
		t.Errorf("expected version 2 after second rebuild, got %d", ToolPoolVersion())
	}
}

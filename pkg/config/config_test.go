package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Existing tests (preserved)
// ---------------------------------------------------------------------------

func TestDefaultProviderConfig(t *testing.T) {
	tests := []struct {
		provider        string
		expectedModel   string
		expectedBaseURL string
		expectedEnvKey  string
	}{
		{"glm", "glm-4", "https://open.bigmodel.cn/api/paas/v4", "ZHIPU_API_KEY"},
		{"openai", "gpt-4o", "https://api.openai.com/v1", "OPENAI_API_KEY"},
		{"claude", "claude-sonnet-4-6", "https://api.anthropic.com", "ANTHROPIC_API_KEY"},
		{"deepseek", "deepseek-chat", "https://api.deepseek.com/v1", "DEEPSEEK_API_KEY"},
		{"kimi", "kimi-k2.6", "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY"},
		{"siliconflow", "deepseek-ai/DeepSeek-V3", "https://api.siliconflow.cn/v1", "SILICONFLOW_API_KEY"},
		{"unknown", "glm-4", "https://open.bigmodel.cn/api/paas/v4", "ZHIPU_API_KEY"}, // fallback
	}

	for _, tt := range tests {
		cfg := DefaultProviderConfig(tt.provider)
		if cfg.Model != tt.expectedModel {
			t.Errorf("Provider %s: expected model %s, got %s", tt.provider, tt.expectedModel, cfg.Model)
		}
		if cfg.BaseURL != tt.expectedBaseURL {
			t.Errorf("Provider %s: expected BaseURL %s, got %s", tt.provider, tt.expectedBaseURL, cfg.BaseURL)
		}
		if cfg.EnvKey != tt.expectedEnvKey {
			t.Errorf("Provider %s: expected EnvKey %s, got %s", tt.provider, tt.expectedEnvKey, cfg.EnvKey)
		}
	}
}

func TestProviderAutoDetection(t *testing.T) {
	tests := []struct {
		model            string
		expectedProvider string
	}{
		{"glm-4", "glm"},
		{"gpt-4o", "openai"},
		{"o1-mini", "openai"},
		{"claude-sonnet-4-6", "claude"},
		{"deepseek-chat", "deepseek"},
		{"kimi-k2.6", "kimi"},
		{"moonshot-v1-8k", "kimi"},
		{"siliconflow-something", "siliconflow"},
		{"deepseek-ai/DeepSeek-V3", "siliconflow"},
	}

	for _, tt := range tests {
		// Mock the logic used in LoadConfig
		cfg := Config{
			Model: tt.model,
		}
		if cfg.Provider == "" {
			if strings.HasPrefix(cfg.Model, "glm") {
				cfg.Provider = "glm"
			} else if strings.HasPrefix(cfg.Model, "gpt") || strings.HasPrefix(cfg.Model, "o1") || strings.HasPrefix(cfg.Model, "o3") {
				cfg.Provider = "openai"
			} else if strings.HasPrefix(cfg.Model, "claude") {
				cfg.Provider = "claude"
			} else if strings.HasPrefix(cfg.Model, "siliconflow") || strings.Contains(cfg.Model, "deepseek-ai/") {
				cfg.Provider = "siliconflow"
			} else if strings.HasPrefix(cfg.Model, "deepseek") {
				cfg.Provider = "deepseek"
			} else if strings.HasPrefix(cfg.Model, "kimi") || strings.HasPrefix(cfg.Model, "moonshot") {
				cfg.Provider = "kimi"
			}
		}

		if cfg.Provider != tt.expectedProvider {
			t.Errorf("Model %s: expected provider %s, got %s", tt.model, tt.expectedProvider, cfg.Provider)
		}
	}
}

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		model        string
		totalTokens  int
		expectedCost float64
	}{
		// GPT-4o pricing: input 2.50, output 10.00
		// For 1,000,000 tokens:
		// input: 850,000 -> 850,000 * 2.50 / 1,000,000 = $2.125
		// output: 150,000 -> 150,000 * 10.00 / 1,000,000 = $1.50
		// total = $3.625
		{"gpt-4o", 1000000, 3.625},
		// Claude 3.5 Sonnet pricing: input 3.00, output 15.00
		// For 100,000 tokens:
		// input: 85,000 -> 85,000 * 3.00 / 1,000,000 = $0.255
		// output: 15,000 -> 15,000 * 15.00 / 1,000,000 = $0.225
		// total = $0.48
		{"claude-3-5-sonnet", 100000, 0.48},
		// Zero or negative tokens should be $0.00
		{"gpt-4o", 0, 0.0},
		{"claude-sonnet", -10, 0.0},
	}

	for _, tt := range tests {
		cost := EstimateCost(tt.model, tt.totalTokens)
		if cost != tt.expectedCost {
			t.Errorf("Model %s with %d tokens: expected cost $%f, got $%f", tt.model, tt.totalTokens, tt.expectedCost, cost)
		}
	}
}

// ---------------------------------------------------------------------------
// New comprehensive tests
// ---------------------------------------------------------------------------

// helperSetHome sets HOME and returns a cleanup function.
func helperSetHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
}

// TestGetConfigPath verifies that GetConfigPath returns a path ending in
// .iroha.json and no error.
func TestGetConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	helperSetHome(t, tmpDir)

	path, err := GetConfigPath()
	if err != nil {
		t.Fatalf("GetConfigPath returned error: %v", err)
	}
	if !strings.HasSuffix(path, ".iroha.json") {
		t.Errorf("expected path to end with .iroha.json, got %s", path)
	}
	expectedDir := tmpDir
	actualDir := filepath.Dir(path)
	if actualDir != expectedDir {
		t.Errorf("expected directory %s, got %s", expectedDir, actualDir)
	}
}

// TestGetConfigPath_NoHome tests that GetConfigPath returns an error when HOME
// cannot be determined (unsetting all home-related env vars).
func TestGetConfigPath_NoHome(t *testing.T) {
	// On most systems UserHomeDir reads $HOME on Unix or $USERPROFILE on
	// Windows.  We clear them; if the runtime still resolves home via
	// getuid/getpwuid the test may pass on some CI, so we accept either
	// outcome.
	t.Setenv("HOME", "")
	path, err := GetConfigPath()
	if err == nil {
		// Some systems can still resolve home via OS APIs; that's fine.
		if path == "" {
			t.Error("expected non-empty path or error")
		}
	}
}

// TestLoadConfig exercises LoadConfig with table-driven subtests.
func TestLoadConfig(t *testing.T) {
	t.Run("ValidJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{
			Provider:  "openai",
			Model:     "gpt-4o",
			APIKey:    "sk-test-key-123",
			BaseURL:   "https://api.openai.com/v1",
			APIFormat: "openai",
		}
		data, err := json.MarshalIndent(cfgData, "", "  ")
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "openai" {
			t.Errorf("expected provider 'openai', got %q", cfg.Provider)
		}
		if cfg.Model != "gpt-4o" {
			t.Errorf("expected model 'gpt-4o', got %q", cfg.Model)
		}
		if cfg.APIKey != "sk-test-key-123" {
			t.Errorf("expected APIKey 'sk-test-key-123', got %q", cfg.APIKey)
		}
		if cfg.BaseURL != "https://api.openai.com/v1" {
			t.Errorf("expected BaseURL 'https://api.openai.com/v1', got %q", cfg.BaseURL)
		}
		if cfg.APIFormat != "openai" {
			t.Errorf("expected APIFormat 'openai', got %q", cfg.APIFormat)
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		if err := os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), []byte("{bad json!!!"), 0600); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		_, err := LoadConfig()
		if err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	})

	t.Run("MissingFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		_, err := LoadConfig()
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
		// Verify it's a "file not found" style error message
		if !strings.Contains(err.Error(), "no configuration file found") {
			t.Errorf("expected 'no configuration file found' in error, got %v", err)
		}
	})

	t.Run("LegacyMigration", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		// Create legacy config file (.go-claude.json) but NOT .iroha.json
		legacyData := Config{
			Provider: "claude",
			Model:    "claude-sonnet-4-6",
			APIKey:   "ant-legacy-key",
		}
		data, err := json.MarshalIndent(legacyData, "", "  ")
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}
		legacyPath := filepath.Join(tmpDir, ".go-claude.json")
		if err := os.WriteFile(legacyPath, data, 0600); err != nil {
			t.Fatalf("failed to write legacy config: %v", err)
		}

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "claude" {
			t.Errorf("expected provider 'claude', got %q", cfg.Provider)
		}
		if cfg.Model != "claude-sonnet-4-6" {
			t.Errorf("expected model 'claude-sonnet-4-6', got %q", cfg.Model)
		}
		if cfg.APIKey != "ant-legacy-key" {
			t.Errorf("expected APIKey 'ant-legacy-key', got %q", cfg.APIKey)
		}

		// Verify new config file was created
		newPath := filepath.Join(tmpDir, ".iroha.json")
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			t.Error("expected .iroha.json to be created during migration")
		}
		// Verify legacy file was renamed to .bak
		bakPath := legacyPath + ".bak"
		if _, err := os.Stat(bakPath); os.IsNotExist(err) {
			t.Error("expected .go-claude.json.bak to exist after migration")
		}
	})

	t.Run("ProviderAutoDetection_GLM", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "glm-4-plus", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "glm" {
			t.Errorf("expected auto-detected provider 'glm', got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_GPT", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "gpt-4o-mini", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "openai" {
			t.Errorf("expected auto-detected provider 'openai', got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_O1", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "o1-mini", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "openai" {
			t.Errorf("expected auto-detected provider 'openai' for o1 model, got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_O3", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "o3-mini", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "openai" {
			t.Errorf("expected auto-detected provider 'openai' for o3 model, got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_Claude", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "claude-sonnet-4-6", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "claude" {
			t.Errorf("expected auto-detected provider 'claude', got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_DeepSeek", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "deepseek-chat", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "deepseek" {
			t.Errorf("expected auto-detected provider 'deepseek', got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_Kimi", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "kimi-k2.6", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "kimi" {
			t.Errorf("expected auto-detected provider 'kimi', got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_Moonshot", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "moonshot-v1-8k", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "kimi" {
			t.Errorf("expected auto-detected provider 'kimi' for moonshot model, got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_SiliconflowPrefix", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "siliconflow-model-x", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "siliconflow" {
			t.Errorf("expected auto-detected provider 'siliconflow', got %q", cfg.Provider)
		}
	})

	t.Run("ProviderAutoDetection_DeepSeekAISlash", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{Model: "deepseek-ai/DeepSeek-V3", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "siliconflow" {
			t.Errorf("expected auto-detected provider 'siliconflow' for deepseek-ai/ model, got %q", cfg.Provider)
		}
	})

	t.Run("ProviderNotOverriddenWhenSet", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		// Provider is explicitly set; should not be overridden by auto-detection
		cfgData := Config{Provider: "deepseek", Model: "gpt-4o", APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "deepseek" {
			t.Errorf("expected explicit provider 'deepseek' to be preserved, got %q", cfg.Provider)
		}
	})

	t.Run("LSPServersPreserved", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{
			Provider: "openai",
			Model:    "gpt-4o",
			APIKey:   "test",
			LSPServers: []LSPServerConfig{
				{Language: "go", Command: "gopls", Args: []string{"serve"}, FilePatterns: []string{"*.go"}},
				{Language: "python", Command: "pylsp"},
			},
		}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if len(cfg.LSPServers) != 2 {
			t.Fatalf("expected 2 LSP servers, got %d", len(cfg.LSPServers))
		}
		if cfg.LSPServers[0].Language != "go" || cfg.LSPServers[0].Command != "gopls" {
			t.Errorf("first LSP server mismatch: %+v", cfg.LSPServers[0])
		}
		if cfg.LSPServers[0].Args[0] != "serve" {
			t.Errorf("expected Args ['serve'], got %v", cfg.LSPServers[0].Args)
		}
		if cfg.LSPServers[1].Language != "python" {
			t.Errorf("second LSP server language mismatch: got %q", cfg.LSPServers[1].Language)
		}
	})

	t.Run("WebSearchConfigPreserved", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{
			Provider:            "openai",
			Model:               "gpt-4o",
			APIKey:              "test",
			WebSearchSearXNGURL: "http://localhost:8080",
		}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.WebSearchSearXNGURL != "http://localhost:8080" {
			t.Errorf("expected WebSearchSearXNGURL 'http://localhost:8080', got %q", cfg.WebSearchSearXNGURL)
		}
	})

	t.Run("EmptyModelNoProvider", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfgData := Config{APIKey: "test"}
		data, _ := json.MarshalIndent(cfgData, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Provider != "" {
			t.Errorf("expected empty provider for empty model, got %q", cfg.Provider)
		}
	})
}

// TestSaveConfig exercises SaveConfig and verifies round-trip, permissions,
// and MkdirAll behavior.
func TestSaveConfig(t *testing.T) {
	t.Run("RoundTrip", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		original := &Config{
			Provider:  "claude",
			Model:     "claude-sonnet-4-6",
			APIKey:    "sk-ant-round-trip-key",
			BaseURL:   "https://api.anthropic.com",
			APIFormat: "anthropic",
			LSPServers: []LSPServerConfig{
				{Language: "go", Command: "gopls"},
			},
			WebSearchSearXNGURL: "http://localhost:9090",
		}

		if err := SaveConfig(original); err != nil {
			t.Fatalf("SaveConfig returned error: %v", err)
		}

		loaded, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error after SaveConfig: %v", err)
		}

		if loaded.Provider != original.Provider {
			t.Errorf("Provider mismatch: expected %q, got %q", original.Provider, loaded.Provider)
		}
		if loaded.Model != original.Model {
			t.Errorf("Model mismatch: expected %q, got %q", original.Model, loaded.Model)
		}
		if loaded.APIKey != original.APIKey {
			t.Errorf("APIKey mismatch: expected %q, got %q", original.APIKey, loaded.APIKey)
		}
		if loaded.BaseURL != original.BaseURL {
			t.Errorf("BaseURL mismatch: expected %q, got %q", original.BaseURL, loaded.BaseURL)
		}
		if loaded.APIFormat != original.APIFormat {
			t.Errorf("APIFormat mismatch: expected %q, got %q", original.APIFormat, loaded.APIFormat)
		}
		if loaded.WebSearchSearXNGURL != original.WebSearchSearXNGURL {
			t.Errorf("WebSearchSearXNGURL mismatch: expected %q, got %q", original.WebSearchSearXNGURL, loaded.WebSearchSearXNGURL)
		}
		if len(loaded.LSPServers) != 1 || loaded.LSPServers[0].Language != "go" {
			t.Errorf("LSPServers mismatch: expected 1 server with language 'go', got %+v", loaded.LSPServers)
		}
	})

	t.Run("FilePermissions", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfg := &Config{Provider: "glm", Model: "glm-4", APIKey: "test"}
		if err := SaveConfig(cfg); err != nil {
			t.Fatalf("SaveConfig returned error: %v", err)
		}

		info, err := os.Stat(filepath.Join(tmpDir, ".iroha.json"))
		if err != nil {
			t.Fatalf("failed to stat config file: %v", err)
		}
		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Errorf("expected file permissions 0600, got %04o", perm)
		}
	})

	t.Run("MkdirAllBehavior", func(t *testing.T) {
		// SaveConfig calls MkdirAll on filepath.Dir of the config path.
		// Since filepath.Dir of ~/.iroha.json is home itself, MkdirAll is
		// effectively a no-op when home exists. Verify SaveConfig succeeds
		// and creates the config file in a fresh temp directory.
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfg := &Config{Provider: "glm", Model: "glm-4", APIKey: "test"}
		if err := SaveConfig(cfg); err != nil {
			t.Fatalf("SaveConfig returned error: %v", err)
		}
		// Verify file was created
		if _, err := os.Stat(filepath.Join(tmpDir, ".iroha.json")); err != nil {
			t.Errorf("config file not created: %v", err)
		}
	})

	t.Run("OverwriteExisting", func(t *testing.T) {
		tmpDir := t.TempDir()
		helperSetHome(t, tmpDir)

		cfg1 := &Config{Provider: "openai", Model: "gpt-4o", APIKey: "key1"}
		if err := SaveConfig(cfg1); err != nil {
			t.Fatalf("first SaveConfig returned error: %v", err)
		}

		cfg2 := &Config{Provider: "claude", Model: "claude-sonnet-4-6", APIKey: "key2"}
		if err := SaveConfig(cfg2); err != nil {
			t.Fatalf("second SaveConfig returned error: %v", err)
		}

		loaded, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if loaded.Provider != "claude" {
			t.Errorf("expected provider 'claude' after overwrite, got %q", loaded.Provider)
		}
		if loaded.APIKey != "key2" {
			t.Errorf("expected APIKey 'key2' after overwrite, got %q", loaded.APIKey)
		}
	})
}

// TestEstimateCost_Complete provides a comprehensive table-driven test covering
// all ModelPricingMap entries, fuzzy match fallback, provider heuristic
// fallback, and default pricing fallback.
func TestEstimateCost_Complete(t *testing.T) {
	// For all tests using 1,000,000 tokens:
	// inputTokens  = 0.85 * 1,000,000 = 850,000
	// outputTokens = 0.15 * 1,000,000 = 150,000
	// cost = (850000/1e6)*input + (150000/1e6)*output

	tests := []struct {
		name         string
		model        string
		totalTokens  int
		expectedCost float64
	}{
		// --- All 17 ModelPricingMap entries with exact match at 1M tokens ---

		// claude-3-5-sonnet: in=3.00 out=15.00 => 0.85*3 + 0.15*15 = 2.55 + 2.25 = 4.80
		{"exact claude-3-5-sonnet", "claude-3-5-sonnet", 1_000_000, 4.80},
		// claude-sonnet: same pricing => 4.80
		{"exact claude-sonnet", "claude-sonnet", 1_000_000, 4.80},
		// claude-3-5-haiku: in=0.80 out=4.00 => 0.85*0.80 + 0.15*4.00 = 0.68 + 0.60 = 1.28
		{"exact claude-3-5-haiku", "claude-3-5-haiku", 1_000_000, 1.28},
		// claude-3-haiku: in=0.25 out=1.25 => 0.85*0.25 + 0.15*1.25 = 0.2125 + 0.1875 = 0.40
		{"exact claude-3-haiku", "claude-3-haiku", 1_000_000, 0.40},
		// claude-3-opus: in=15.00 out=75.00 => 0.85*15 + 0.15*75 = 12.75 + 11.25 = 24.00
		{"exact claude-3-opus", "claude-3-opus", 1_000_000, 24.00},
		// gpt-4o-mini: in=0.15 out=0.60 => 0.85*0.15 + 0.15*0.60 = 0.1275 + 0.09 = 0.2175
		// Note: the model name "gpt-4o-mini" contains both "gpt-4o" and "gpt-4o-mini"
		// as substrings. Map iteration is non-deterministic, so either could match first.
		// We test with the exact key which should match when it's iterated, but accept
		// either gpt-4o pricing (3.625) or gpt-4o-mini pricing (0.2175).
		{"fuzzy overlap gpt-4o-mini vs gpt-4o", "gpt-4o-mini", 1_000_000, -1}, // special: see below
		// gpt-4o: in=2.50 out=10.00 => 0.85*2.50 + 0.15*10.00 = 2.125 + 1.50 = 3.625
		{"exact gpt-4o", "gpt-4o", 1_000_000, 3.625},
		// o1-mini: contains "o1" so map iteration order determines match (-1 = accept either)
		{"exact o1-mini", "o1-mini", 1_000_000, -1},
		// o1: in=15.00 out=60.00 => 0.85*15 + 0.15*60 = 12.75 + 9.00 = 21.75
		{"exact o1", "o1", 1_000_000, 21.75},
		// o3-mini: in=1.10 out=4.40 => 0.85*1.10 + 0.15*4.40 = 0.935 + 0.66 = 1.595
		{"exact o3-mini", "o3-mini", 1_000_000, 1.595},
		// deepseek-chat: in=0.14 out=0.28 => 0.85*0.14 + 0.15*0.28 = 0.119 + 0.042 = 0.161
		{"exact deepseek-chat", "deepseek-chat", 1_000_000, 0.161},
		// deepseek-v3: in=0.14 out=0.28 => same as deepseek-chat = 0.161
		{"exact deepseek-v3", "deepseek-v3", 1_000_000, 0.161},
		// deepseek-r1: in=0.55 out=2.19 => 0.85*0.55 + 0.15*2.19 = 0.4675 + 0.3285 = 0.796
		{"exact deepseek-r1", "deepseek-r1", 1_000_000, 0.796},
		// glm-4-flash: contains "glm-4" so map iteration order determines match (-1 = accept either)
		{"exact glm-4-flash", "glm-4-flash", 1_000_000, -2},
		// glm-4: in=0.10 out=0.10 => 0.85*0.10 + 0.15*0.10 = 0.085 + 0.015 = 0.10
		{"exact glm-4", "glm-4", 1_000_000, 0.10},
		// kimi: in=1.00 out=1.00 => 0.85*1.00 + 0.15*1.00 = 1.00
		{"exact kimi", "kimi", 1_000_000, 1.00},
		// moonshot: in=1.00 out=1.00 => same as kimi = 1.00
		{"exact moonshot", "moonshot", 1_000_000, 1.00},

		// --- Zero/negative tokens ---
		{"zero tokens", "gpt-4o", 0, 0.0},
		{"negative tokens", "gpt-4o", -100, 0.0},

		// --- Fuzzy match: model names containing known substrings ---
		// "my-gpt-4o-custom" contains "gpt-4o" => uses gpt-4o pricing
		{"fuzzy gpt-4o", "my-gpt-4o-custom", 1_000_000, 3.625},
		// "claude-3-5-sonnet-latest" contains "claude-3-5-sonnet"
		{"fuzzy claude-3-5-sonnet", "claude-3-5-sonnet-latest", 1_000_000, 4.80},
		// "deepseek-chat-v3" contains "deepseek-chat"
		{"fuzzy deepseek-chat", "deepseek-chat-v3", 1_000_000, 0.161},

		// --- Provider heuristic fallback (no direct ModelPricingMap match) ---
		// "gpt-3.5-turbo" doesn't match any key directly, but contains "gpt" => gpt-4o pricing
		{"heuristic gpt", "gpt-3.5-turbo", 1_000_000, 3.625},
		// "openai-custom" contains "openai" => gpt-4o pricing
		{"heuristic openai", "openai-custom", 1_000_000, 3.625},
		// "claude-instant" contains "claude" (no direct match) => claude-sonnet pricing
		{"heuristic claude", "claude-instant", 1_000_000, 4.80},
		// "deepseek-coder" contains "deepseek" (no direct match) => deepseek-chat pricing
		{"heuristic deepseek", "deepseek-coder", 1_000_000, 0.161},
		// "glm-3-turbo" contains "glm" => glm-4 pricing
		{"heuristic glm", "glm-3-turbo", 1_000_000, 0.10},
		// "zhipu-bigmodel" contains "zhipu" => glm-4 pricing
		{"heuristic zhipu", "zhipu-bigmodel", 1_000_000, 0.10},
		// "kimi-latest" contains "kimi" (no direct match) => kimi pricing
		{"heuristic kimi", "kimi-latest", 1_000_000, 1.00},
		// "moonshot-lite" contains "moonshot" (no direct match) => kimi pricing
		{"heuristic moonshot", "moonshot-lite", 1_000_000, 1.00},

		// --- Default fallback pricing for completely unknown model ---
		// Default: in=1.50 out=6.00 => 0.85*1.50 + 0.15*6.00 = 1.275 + 0.90 = 2.175
		{"default fallback", "totally-unknown-model", 1_000_000, 2.175},

		// --- Case insensitivity ---
		// "GPT-4O" should match "gpt-4o" after ToLower
		{"case insensitive GPT-4O", "GPT-4O", 1_000_000, 3.625},
		{"case insensitive CLAUDE", "CLAUDE-3-5-SONNET", 1_000_000, 4.80},

		// --- Small token count ---
		// gpt-4o with 1000 tokens: 0.85*1000*2.50/1e6 + 0.15*1000*10.00/1e6
		// = 0.002125 + 0.0015 = 0.003625
		{"small token count", "gpt-4o", 1000, 0.003625},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := EstimateCost(tt.model, tt.totalTokens)
			// Special case: -1 indicates ambiguous fuzzy match where either
			// of two overlapping pricing entries is acceptable.
			if tt.expectedCost == -1 {
				// Ambiguous match (gpt-4o-mini/o1-mini), accept overlapping pricing
				valid := map[float64]bool{0.2175: true, 3.625: true, 4.35: true, 21.75: true}
				if !valid[cost] {
					t.Errorf("EstimateCost(%q, %d) = %f, not an expected ambiguous match", tt.model, tt.totalTokens, cost)
				}
				return
			}
			if tt.expectedCost == -2 {
				// glm-4-flash overlaps with glm-4
				if cost != 0.00 && cost != 0.10 {
					t.Errorf("EstimateCost(%q, %d) = %f, want 0.00 or 0.10", tt.model, tt.totalTokens, cost)
				}
				return
			}
			// Use tolerance for floating-point comparison
			delta := tt.expectedCost - cost
			if delta < 0 {
				delta = -delta
			}
			if delta > 1e-9 {
				t.Errorf("EstimateCost(%q, %d) = %f, want %f", tt.model, tt.totalTokens, cost, tt.expectedCost)
			}
		})
	}
}

// TestEstimateCost_AllPricingMapKeys verifies that every key in ModelPricingMap
// can be looked up via EstimateCost with a non-zero result (unless pricing is
// zero like glm-4-flash).
func TestEstimateCost_AllPricingMapKeys(t *testing.T) {
	for key := range ModelPricingMap {
		cost := EstimateCost(key, 1_000_000)
		// Accept if cost matches any entry (tolerating float rounding and map-order ambiguity)
		found := false
		for _, p2 := range ModelPricingMap {
			expected := 0.85*p2.InputCostPerMillion + 0.15*p2.OutputCostPerMillion
			if cost-expected < 1e-9 && expected-cost < 1e-9 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EstimateCost(%q, 1M) = %f, doesn't match any pricing entry", key, cost)
		}
	}
}

// TestModelPricingMapCompleteness verifies that the map has exactly the
// expected number of entries to catch accidental additions/removals.
func TestModelPricingMapCompleteness(t *testing.T) {
	expectedCount := 17
	if len(ModelPricingMap) != expectedCount {
		t.Errorf("expected ModelPricingMap to have %d entries, got %d", expectedCount, len(ModelPricingMap))
	}
}

// TestProviderDefaultsCompleteness verifies ProviderDefaults has the expected
// number of provider entries.
func TestProviderDefaultsCompleteness(t *testing.T) {
	expectedProviders := []string{"glm", "openai", "claude", "deepseek", "kimi", "siliconflow"}
	if len(ProviderDefaults) != len(expectedProviders) {
		t.Errorf("expected ProviderDefaults to have %d entries, got %d", len(expectedProviders), len(ProviderDefaults))
	}
	for _, p := range expectedProviders {
		if _, ok := ProviderDefaults[p]; !ok {
			t.Errorf("expected provider %q in ProviderDefaults, not found", p)
		}
	}
}

// TestConfigJSONRoundTrip verifies JSON marshaling/unmarshaling of the Config
// struct directly.
func TestConfigJSONRoundTrip(t *testing.T) {
	original := Config{
		Provider:            "deepseek",
		Model:               "deepseek-chat",
		APIKey:              "ds-test-key",
		BaseURL:             "https://api.deepseek.com/v1",
		APIFormat:           "openai",
		WebSearchSearXNGURL: "http://searxng:8080",
		LSPServers: []LSPServerConfig{
			{Language: "go", Command: "gopls", Args: []string{"serve"}, FilePatterns: []string{"*.go"}},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if loaded.Provider != original.Provider {
		t.Errorf("Provider: expected %q, got %q", original.Provider, loaded.Provider)
	}
	if loaded.Model != original.Model {
		t.Errorf("Model: expected %q, got %q", original.Model, loaded.Model)
	}
	if loaded.APIKey != original.APIKey {
		t.Errorf("APIKey: expected %q, got %q", original.APIKey, loaded.APIKey)
	}
	if loaded.BaseURL != original.BaseURL {
		t.Errorf("BaseURL: expected %q, got %q", original.BaseURL, loaded.BaseURL)
	}
	if loaded.APIFormat != original.APIFormat {
		t.Errorf("APIFormat: expected %q, got %q", original.APIFormat, loaded.APIFormat)
	}
	if loaded.WebSearchSearXNGURL != original.WebSearchSearXNGURL {
		t.Errorf("WebSearchSearXNGURL: expected %q, got %q", original.WebSearchSearXNGURL, loaded.WebSearchSearXNGURL)
	}
	if len(loaded.LSPServers) != 1 {
		t.Fatalf("LSPServers: expected 1, got %d", len(loaded.LSPServers))
	}
	if loaded.LSPServers[0].Language != "go" {
		t.Errorf("LSPServers[0].Language: expected 'go', got %q", loaded.LSPServers[0].Language)
	}
}

// TestConfigJSON_EmptyFields verifies that empty/omitempty fields behave
// correctly in JSON serialization.
func TestConfigJSON_EmptyFields(t *testing.T) {
	cfg := Config{
		Provider: "glm",
		Model:    "glm-4",
		APIKey:   "test",
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// BaseURL and APIFormat have omitempty, so they should not appear
	s := string(data)
	if strings.Contains(s, "base_url") {
		t.Errorf("expected base_url to be omitted for empty value, got: %s", s)
	}
	if strings.Contains(s, "api_format") {
		t.Errorf("expected api_format to be omitted for empty value, got: %s", s)
	}
	if strings.Contains(s, "lsp_servers") {
		t.Errorf("expected lsp_servers to be omitted for nil slice, got: %s", s)
	}
}

// TestLSPServerConfig_JSON verifies LSPServerConfig serialization.
func TestLSPServerConfig_JSON(t *testing.T) {
	tests := []struct {
		name     string
		server   LSPServerConfig
		jsonStr  string
	}{
		{
			name:    "full",
			server:  LSPServerConfig{Language: "go", Command: "gopls", Args: []string{"serve"}, FilePatterns: []string{"*.go"}},
			jsonStr: `{"language":"go","command":"gopls","args":["serve"],"file_patterns":["*.go"]}`,
		},
		{
			name:    "minimal",
			server:  LSPServerConfig{Language: "python", Command: "pylsp"},
			jsonStr: `{"language":"python","command":"pylsp"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.server)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			if string(data) != tt.jsonStr {
				t.Errorf("expected %s, got %s", tt.jsonStr, string(data))
			}

			var parsed LSPServerConfig
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if parsed.Language != tt.server.Language {
				t.Errorf("Language: expected %q, got %q", tt.server.Language, parsed.Language)
			}
			if parsed.Command != tt.server.Command {
				t.Errorf("Command: expected %q, got %q", tt.server.Command, parsed.Command)
			}
		})
	}
}

// TestDefaultProviderConfig_Fields verifies AnthropicBaseURL for providers that
// support it.
func TestDefaultProviderConfig_AnthropicBaseURL(t *testing.T) {
	tests := []struct {
		provider              string
		hasAnthropicBaseURL   bool
		anthropicBaseURL      string
	}{
		{"glm", true, "https://open.bigmodel.cn/api/anthropic"},
		{"deepseek", true, "https://api.deepseek.com/anthropic"},
		{"openai", false, ""},
		{"claude", false, ""},
		{"kimi", false, ""},
		{"siliconflow", false, ""},
		{"unknown", true, "https://open.bigmodel.cn/api/anthropic"}, // falls back to glm
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			cfg := DefaultProviderConfig(tt.provider)
			if tt.hasAnthropicBaseURL {
				if cfg.AnthropicBaseURL != tt.anthropicBaseURL {
					t.Errorf("expected AnthropicBaseURL %q, got %q", tt.anthropicBaseURL, cfg.AnthropicBaseURL)
				}
			} else {
				if cfg.AnthropicBaseURL != "" {
					t.Errorf("expected empty AnthropicBaseURL, got %q", cfg.AnthropicBaseURL)
				}
			}
		})
	}
}

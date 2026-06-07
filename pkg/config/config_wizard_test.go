package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunConfigWizard exercises the interactive configuration wizard by piping
// pre-defined input through stdin. Each subtest sets up a temp HOME with an
// optional existing config, writes the wizard answers to a pipe, and verifies
// the resulting saved configuration.
func TestRunConfigWizard(t *testing.T) {
	t.Run("AllDefaults_NoExistingConfig", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// All prompts answered with Enter (empty line) to accept defaults
		input := "\n\n\n\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg == nil {
			t.Fatal("RunConfigWizard returned nil config")
		}
		// Default provider is glm (from no existing config)
		if cfg.Provider != "glm" {
			t.Errorf("expected default provider 'glm', got %q", cfg.Provider)
		}
		if cfg.Model != "glm-4" {
			t.Errorf("expected default model 'glm-4', got %q", cfg.Model)
		}
		// Verify saved file exists
		data, err := os.ReadFile(filepath.Join(tmpDir, ".iroha.json"))
		if err != nil {
			t.Fatalf("failed to read saved config: %v", err)
		}
		var saved Config
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatalf("failed to parse saved config: %v", err)
		}
		if saved.Provider != "glm" {
			t.Errorf("saved provider: expected 'glm', got %q", saved.Provider)
		}
	})

	t.Run("SelectOpenAI_WithCustomModelAndKey", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Provider: o (openai), Model: gpt-4o-mini, API Key: sk-test-123, Base URL: Enter (default), Format: Enter (no anthropic support for openai)
		input := "o\ngpt-4o-mini\nsk-test-123\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "openai" {
			t.Errorf("expected provider 'openai', got %q", cfg.Provider)
		}
		if cfg.Model != "gpt-4o-mini" {
			t.Errorf("expected model 'gpt-4o-mini', got %q", cfg.Model)
		}
		if cfg.APIKey != "sk-test-123" {
			t.Errorf("expected apiKey 'sk-test-123', got %q", cfg.APIKey)
		}
	})

	t.Run("SelectClaude_ProviderFullName", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Provider: claude (full name), Model: Enter (default), API Key: ant-key, Base URL: Enter
		input := "claude\n\nant-key\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "claude" {
			t.Errorf("expected provider 'claude', got %q", cfg.Provider)
		}
		if cfg.APIKey != "ant-key" {
			t.Errorf("expected apiKey 'ant-key', got %q", cfg.APIKey)
		}
	})

	t.Run("SelectDeepSeek_WithCustomBaseURL", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Provider: d, Model: Enter, Key: ds-key, Base URL: https://custom.deepseek.com, Format: o
		input := "d\n\nds-key\nhttps://custom.deepseek.com\no\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "deepseek" {
			t.Errorf("expected provider 'deepseek', got %q", cfg.Provider)
		}
		if cfg.BaseURL != "https://custom.deepseek.com" {
			t.Errorf("expected custom baseURL, got %q", cfg.BaseURL)
		}
	})

	t.Run("SelectKimi", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		input := "k\n\nkimi-key\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "kimi" {
			t.Errorf("expected provider 'kimi', got %q", cfg.Provider)
		}
		if cfg.APIKey != "kimi-key" {
			t.Errorf("expected apiKey 'kimi-key', got %q", cfg.APIKey)
		}
	})

	t.Run("SelectSiliconflow", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		input := "f\n\nsf-key\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "siliconflow" {
			t.Errorf("expected provider 'siliconflow', got %q", cfg.Provider)
		}
		if cfg.APIKey != "sf-key" {
			t.Errorf("expected apiKey 'sf-key', got %q", cfg.APIKey)
		}
	})

	t.Run("SelectGLM_Explicit", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		input := "g\n\nzhipu-key\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "glm" {
			t.Errorf("expected provider 'glm', got %q", cfg.Provider)
		}
	})

	t.Run("ExistingConfig_PreservesOnEnter", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Write existing config
		existing := &Config{
			Provider:  "openai",
			Model:     "gpt-4o",
			APIKey:    "existing-key-1234",
			BaseURL:   "https://api.openai.com/v1",
			APIFormat: "openai",
		}
		data, _ := json.MarshalIndent(existing, "", "  ")
		if err := os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600); err != nil {
			t.Fatalf("failed to write existing config: %v", err)
		}

		// All Enter to keep existing values
		input := "\n\n\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "openai" {
			t.Errorf("expected preserved provider 'openai', got %q", cfg.Provider)
		}
		if cfg.Model != "gpt-4o" {
			t.Errorf("expected preserved model 'gpt-4o', got %q", cfg.Model)
		}
		if cfg.APIKey != "existing-key-1234" {
			t.Errorf("expected preserved apiKey, got %q", cfg.APIKey)
		}
	})

	t.Run("ExistingConfig_ChangeProviderKeepsExistingModel", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Write existing config with openai
		existing := &Config{
			Provider: "openai",
			Model:    "gpt-4o",
			APIKey:   "existing-key",
			BaseURL:  "https://api.openai.com/v1",
		}
		data, _ := json.MarshalIndent(existing, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		// Switch to deepseek (d), press Enter on model (keeps existing "gpt-4o")
		input := "d\n\n\n\no\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "deepseek" {
			t.Errorf("expected provider 'deepseek', got %q", cfg.Provider)
		}
		// When provider changes but model input is empty, existing model is kept
		if cfg.Model != "gpt-4o" {
			t.Errorf("expected existing model 'gpt-4o' kept on Enter, got %q", cfg.Model)
		}
	})

	t.Run("ExistingConfig_ChangeProviderWithNewModel", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Write existing config with openai
		existing := &Config{
			Provider: "openai",
			Model:    "gpt-4o",
			APIKey:   "existing-key",
			BaseURL:  "https://api.openai.com/v1",
		}
		data, _ := json.MarshalIndent(existing, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		// Switch to deepseek (d), explicitly set model to deepseek-chat
		input := "d\ndeepseek-chat\n\n\no\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "deepseek" {
			t.Errorf("expected provider 'deepseek', got %q", cfg.Provider)
		}
		if cfg.Model != "deepseek-chat" {
			t.Errorf("expected model 'deepseek-chat', got %q", cfg.Model)
		}
	})

	t.Run("BaseURL_ResetWithDefault", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Write existing config with custom base URL
		existing := &Config{
			Provider: "glm",
			Model:    "glm-4",
			APIKey:   "test-key",
			BaseURL:  "https://custom-url.example.com",
		}
		data, _ := json.MarshalIndent(existing, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		// Use "default" keyword to reset base URL.
		// Since provider matches existing, defaultBaseURL = existing.BaseURL = custom one.
		// So "default" resets to the existing provider's default, which is the custom URL
		// because provider==existing.Provider. To truly reset, we switch provider then switch back.
		// Actually, "default" sets baseURL = defaultBaseURL. When provider==existing.Provider,
		// defaultBaseURL = existing.BaseURL. So typing "default" is a no-op in this case.
		input := "\n\ndefault\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		// With same provider, defaultBaseURL == existing.BaseURL, so "default" is a no-op
		if cfg.BaseURL != "https://custom-url.example.com" {
			t.Errorf("expected baseURL unchanged when provider matches, got %q", cfg.BaseURL)
		}
	})

	t.Run("BaseURL_ResetToProviderDefault", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Write existing config with openai provider and custom base URL
		existing := &Config{
			Provider: "openai",
			Model:    "gpt-4o",
			APIKey:   "test-key",
			BaseURL:  "https://custom-url.example.com",
		}
		data, _ := json.MarshalIndent(existing, "", "  ")
		os.WriteFile(filepath.Join(tmpDir, ".iroha.json"), data, 0600)

		// Switch to glm (g), then on base URL type "default" which sets to glm default
		input := "g\n\ntest-key\ndefault\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "glm" {
			t.Errorf("expected provider 'glm', got %q", cfg.Provider)
		}
		if cfg.BaseURL != "https://open.bigmodel.cn/api/paas/v4" {
			t.Errorf("expected default glm baseURL after switching provider, got %q", cfg.BaseURL)
		}
	})

	t.Run("GLM_AnthropicFormat_SwitchEndpoint", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Provider: glm, then select anthropic format, accept endpoint switch
		input := "g\n\nglm-key\n\na\ny\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Provider != "glm" {
			t.Errorf("expected provider 'glm', got %q", cfg.Provider)
		}
		if cfg.APIFormat != "anthropic" {
			t.Errorf("expected apiFormat 'anthropic', got %q", cfg.APIFormat)
		}
		// Should have auto-switched to anthropic base URL
		if cfg.BaseURL != "https://open.bigmodel.cn/api/anthropic" {
			t.Errorf("expected anthropic base URL, got %q", cfg.BaseURL)
		}
	})

	t.Run("DeepSeek_AnthropicFormat_DeclineSwitch", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Provider: deepseek, anthropic format, decline endpoint switch
		input := "d\n\nds-key\n\na\nn\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.APIFormat != "anthropic" {
			t.Errorf("expected apiFormat 'anthropic', got %q", cfg.APIFormat)
		}
		// Base URL should be deepseek default (not switched to anthropic)
		if cfg.BaseURL != "https://api.deepseek.com/v1" {
			t.Errorf("expected deepseek default baseURL, got %q", cfg.BaseURL)
		}
	})

	t.Run("OpenAI_NoAnthropicFormat", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// OpenAI does not support anthropic format, so format step is skipped
		input := "o\n\ntest-key\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.APIFormat != "" {
			t.Errorf("expected empty apiFormat for openai, got %q", cfg.APIFormat)
		}
	})

	t.Run("ShortAPIKey_NotMasked", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Short API key (< 8 chars) should not be masked
		input := "\n\nshort\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.APIKey != "short" {
			t.Errorf("expected apiKey 'short', got %q", cfg.APIKey)
		}
	})

	t.Run("EmptyModelFallsBackToDefault", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// No existing config, provider glm, model empty => should get glm default
		input := "g\n\ntest-key\n\n\n"
		cfg, err := runWizardWithInput(t, input)
		if err != nil {
			t.Fatalf("RunConfigWizard returned error: %v", err)
		}
		if cfg.Model != "glm-4" {
			t.Errorf("expected model 'glm-4', got %q", cfg.Model)
		}
	})
}

// runWizardWithInput is a helper that redirects stdin to the provided input
// string, runs RunConfigWizard, and restores stdin afterward.
func runWizardWithInput(t *testing.T, input string) (*Config, error) {
	t.Helper()

	// Create a pipe: write input to writeEnd, readEnd becomes new stdin
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	// Write input to the pipe
	go func() {
		writeEnd.WriteString(input)
		writeEnd.Close()
	}()

	// Replace stdin
	oldStdin := os.Stdin
	os.Stdin = readEnd
	defer func() { os.Stdin = oldStdin }()

	// Also capture stdout to suppress wizard output during tests
	oldStdout := os.Stdout
	devNull, _ := os.Open(os.DevNull)
	os.Stdout = devNull
	defer func() {
		os.Stdout = oldStdout
		devNull.Close()
	}()

	cfg, err := RunConfigWizard()

	// Close read end
	readEnd.Close()

	return cfg, err
}

// TestRunConfigWizard_SiliconflowProvider verifies siliconflow with provider key 'f'.
func TestRunConfigWizard_SiliconflowProvider(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	input := "f\nsiliconflow-custom\nsf-key-12345678\nhttps://custom.sf.com\n\n"
	cfg, err := runWizardWithInput(t, input)
	if err != nil {
		t.Fatalf("RunConfigWizard returned error: %v", err)
	}
	if cfg.Provider != "siliconflow" {
		t.Errorf("expected provider 'siliconflow', got %q", cfg.Provider)
	}
	if cfg.Model != "siliconflow-custom" {
		t.Errorf("expected custom model, got %q", cfg.Model)
	}
	if !strings.Contains(cfg.BaseURL, "custom.sf.com") {
		t.Errorf("expected custom base URL containing 'custom.sf.com', got %q", cfg.BaseURL)
	}
}

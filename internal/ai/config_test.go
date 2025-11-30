package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", cfg.Provider)
	}
	if cfg.Model != "gpt-4.1-mini" {
		t.Errorf("expected model 'gpt-4.1-mini', got '%s'", cfg.Model)
	}
	if cfg.Endpoint != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("expected OpenAI endpoint, got '%s'", cfg.Endpoint)
	}
	if cfg.APIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("expected api_key_env 'OPENAI_API_KEY', got '%s'", cfg.APIKeyEnv)
	}
	if cfg.MaxTokens != 1024 {
		t.Errorf("expected max_tokens 1024, got %d", cfg.MaxTokens)
	}
	if cfg.DefaultActions.Summarize.MaxLines != 300 {
		t.Errorf("expected summarize max_lines 300, got %d", cfg.DefaultActions.Summarize.MaxLines)
	}
	if cfg.DefaultActions.Explain.MaxLines != 100 {
		t.Errorf("expected explain max_lines 100, got %d", cfg.DefaultActions.Explain.MaxLines)
	}
}

func TestConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		expected bool
	}{
		{"openai enabled", "openai", true},
		{"anthropic enabled", "anthropic", true},
		{"none disabled", "none", false},
		{"empty disabled", "", false},
		{"cli enabled", "cli", true},
		{"claude-code enabled", "claude-code", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Provider: tt.provider}
			if got := cfg.IsEnabled(); got != tt.expected {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfig_IsCLIProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		expected bool
	}{
		{"openai is not CLI", "openai", false},
		{"anthropic is not CLI", "anthropic", false},
		{"claude-code is CLI", "claude-code", true},
		{"codex is CLI", "codex", true},
		{"gemini is CLI", "gemini", true},
		{"aider is CLI", "aider", true},
		{"cli is CLI", "cli", true},
		{"custom-http is not CLI", "custom-http", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Provider: tt.provider}
			if got := cfg.IsCLIProvider(); got != tt.expected {
				t.Errorf("IsCLIProvider() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		setEnvKey string
		setEnvVal string
		wantErr   bool
		errField  string
	}{
		{
			name:    "disabled is valid",
			config:  Config{Provider: "none"},
			wantErr: false,
		},
		{
			name:      "openai with API key is valid",
			config:    Config{Provider: "openai", APIKeyEnv: "TEST_API_KEY"},
			setEnvKey: "TEST_API_KEY",
			setEnvVal: "sk-test",
			wantErr:   false,
		},
		{
			name:     "openai without API key is invalid",
			config:   Config{Provider: "openai", APIKeyEnv: "NONEXISTENT_KEY"},
			wantErr:  true,
			errField: "api_key",
		},
		{
			name:    "claude-code is valid without API key",
			config:  Config{Provider: "claude-code"},
			wantErr: false,
		},
		{
			name:     "cli without command is invalid",
			config:   Config{Provider: "cli"},
			wantErr:  true,
			errField: "cli_command",
		},
		{
			name:    "cli with command is valid",
			config:  Config{Provider: "cli", CLICommand: "my-ai-tool"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnvKey != "" {
				os.Setenv(tt.setEnvKey, tt.setEnvVal)
				defer os.Unsetenv(tt.setEnvKey)
			}

			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				if cfgErr, ok := err.(*ConfigError); ok {
					if cfgErr.Field != tt.errField {
						t.Errorf("Validate() error field = %s, want %s", cfgErr.Field, tt.errField)
					}
				}
			}
		})
	}
}

func TestConfig_applyProviderDefaults(t *testing.T) {
	t.Run("openai defaults", func(t *testing.T) {
		cfg := Config{Provider: "openai"}
		cfg.applyProviderDefaults()

		if cfg.Model != "gpt-4.1-mini" {
			t.Errorf("expected model 'gpt-4.1-mini', got '%s'", cfg.Model)
		}
		if cfg.Endpoint != "https://api.openai.com/v1/chat/completions" {
			t.Errorf("expected OpenAI endpoint, got '%s'", cfg.Endpoint)
		}
		if cfg.APIKeyEnv != "OPENAI_API_KEY" {
			t.Errorf("expected OPENAI_API_KEY, got '%s'", cfg.APIKeyEnv)
		}
		if cfg.MaxTokens != 1024 {
			t.Errorf("expected MaxTokens 1024, got %d", cfg.MaxTokens)
		}
	})

	t.Run("anthropic defaults", func(t *testing.T) {
		cfg := Config{Provider: "anthropic"}
		cfg.applyProviderDefaults()

		if cfg.Model != "claude-3-haiku-20240307" {
			t.Errorf("expected claude-3-haiku model, got '%s'", cfg.Model)
		}
		if cfg.Endpoint != "https://api.anthropic.com/v1/messages" {
			t.Errorf("expected Anthropic endpoint, got '%s'", cfg.Endpoint)
		}
		if cfg.APIKeyEnv != "ANTHROPIC_API_KEY" {
			t.Errorf("expected ANTHROPIC_API_KEY, got '%s'", cfg.APIKeyEnv)
		}
	})

	t.Run("claude-code CLI defaults", func(t *testing.T) {
		cfg := Config{Provider: "claude-code"}
		cfg.applyProviderDefaults()

		if cfg.CLICommand != "claude" {
			t.Errorf("expected CLICommand 'claude', got '%s'", cfg.CLICommand)
		}
	})

	t.Run("empty provider defaults to openai", func(t *testing.T) {
		cfg := Config{}
		cfg.applyProviderDefaults()

		if cfg.Provider != "openai" {
			t.Errorf("expected provider 'openai', got '%s'", cfg.Provider)
		}
	})
}

func TestLoadConfig_NoFile(t *testing.T) {
	// Change to a temp directory without config files
	tmpDir, err := os.MkdirTemp("", "muxctl-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldDir)

	// Temporarily modify HOME to avoid loading user config
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Should return defaults
	if cfg.Provider != "openai" {
		t.Errorf("expected default provider 'openai', got '%s'", cfg.Provider)
	}
}

func TestLoadConfig_WithFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muxctl-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create config file
	configContent := `
provider: anthropic
model: claude-3-opus
max_tokens: 2048
`
	configPath := filepath.Join(tmpDir, "ai.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	oldDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldDir)

	// Temporarily modify HOME
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got '%s'", cfg.Provider)
	}
	if cfg.Model != "claude-3-opus" {
		t.Errorf("expected model 'claude-3-opus', got '%s'", cfg.Model)
	}
	if cfg.MaxTokens != 2048 {
		t.Errorf("expected max_tokens 2048, got %d", cfg.MaxTokens)
	}
}

func TestConfig_GetAPIKey(t *testing.T) {
	os.Setenv("TEST_MUXCTL_KEY", "test-secret-key")
	defer os.Unsetenv("TEST_MUXCTL_KEY")

	cfg := Config{APIKeyEnv: "TEST_MUXCTL_KEY"}
	if got := cfg.GetAPIKey(); got != "test-secret-key" {
		t.Errorf("GetAPIKey() = %s, want 'test-secret-key'", got)
	}

	cfg2 := Config{APIKeyEnv: "NONEXISTENT_KEY"}
	if got := cfg2.GetAPIKey(); got != "" {
		t.Errorf("GetAPIKey() for nonexistent key = %s, want ''", got)
	}
}

func TestConfigError(t *testing.T) {
	err := &ConfigError{Field: "api_key", Message: "not found"}
	expected := "AI config error (api_key): not found"
	if err.Error() != expected {
		t.Errorf("Error() = %s, want %s", err.Error(), expected)
	}
}

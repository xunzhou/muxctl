package ai

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds AI provider configuration.
type Config struct {
	Provider  string `yaml:"provider"`    // "openai", "anthropic", "custom-http", "claude-code", "codex", "gemini", "aider", "cli", "none"
	Model     string `yaml:"model"`       // e.g., "gpt-4.1-mini", "claude-3-haiku"
	Endpoint  string `yaml:"endpoint"`    // API endpoint URL
	APIKeyEnv string `yaml:"api_key_env"` // Environment variable name for API key
	MaxTokens int    `yaml:"max_tokens"`  // Max tokens for response

	// CLI-based provider settings
	CLICommand string   `yaml:"cli_command"` // Command for generic "cli" provider
	CLIArgs    []string `yaml:"cli_args"`    // Base arguments for CLI command

	// Action-specific settings
	DefaultActions ActionDefaults           `yaml:"default_actions"`
	CustomActions  map[string]*CustomAction `yaml:"custom_actions,omitempty"` // User-defined actions
}

// ActionDefaults holds default settings for built-in action types.
type ActionDefaults struct {
	Summarize ActionSettings `yaml:"summarize"`
	Explain   ActionSettings `yaml:"explain"`
}

// ActionSettings holds settings for a specific action.
type ActionSettings struct {
	MaxLines     int    `yaml:"max_lines"`
	SystemPrompt string `yaml:"system_prompt,omitempty"` // Override default system prompt
	UserPrompt   string `yaml:"user_prompt,omitempty"`   // Override default user prompt (supports {{context}}, {{content}})
}

// CustomAction defines a user-defined AI action.
type CustomAction struct {
	MaxLines     int    `yaml:"max_lines"`
	SystemPrompt string `yaml:"system_prompt"`
	UserPrompt   string `yaml:"user_prompt"` // Supports {{context}}, {{content}}, {{truncated}}
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Provider:  "openai",
		Model:     "gpt-4.1-mini",
		Endpoint:  "https://api.openai.com/v1/chat/completions",
		APIKeyEnv: "OPENAI_API_KEY",
		MaxTokens: 1024,
		DefaultActions: ActionDefaults{
			Summarize: ActionSettings{MaxLines: 300},
			Explain:   ActionSettings{MaxLines: 100},
		},
	}
}

// LoadConfig loads AI configuration from the config file.
// Falls back to defaults if the file doesn't exist.
func LoadConfig() (Config, error) {
	var cfg Config

	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file, use defaults
			return DefaultConfig(), nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	// Apply provider-specific defaults
	cfg.applyProviderDefaults()

	// Action defaults apply to all providers
	if cfg.DefaultActions.Summarize.MaxLines == 0 {
		cfg.DefaultActions.Summarize.MaxLines = 300
	}
	if cfg.DefaultActions.Explain.MaxLines == 0 {
		cfg.DefaultActions.Explain.MaxLines = 100
	}

	return cfg, nil
}

// applyProviderDefaults applies provider-specific default values.
func (c *Config) applyProviderDefaults() {
	if c.Provider == "" {
		c.Provider = "openai"
	}

	// Apply CLI provider defaults
	if c.IsCLIProvider() {
		if c.CLICommand == "" {
			switch c.Provider {
			case "claude-code":
				c.CLICommand = "claude"
			case "codex":
				c.CLICommand = "codex"
			case "gemini":
				c.CLICommand = "gemini"
			case "aider":
				c.CLICommand = "aider"
			}
		}
		return
	}

	// Apply API provider-specific defaults
	switch c.Provider {
	case "openai":
		if c.Model == "" {
			c.Model = "gpt-4.1-mini"
		}
		if c.Endpoint == "" {
			c.Endpoint = "https://api.openai.com/v1/chat/completions"
		}
		if c.APIKeyEnv == "" {
			c.APIKeyEnv = "OPENAI_API_KEY"
		}
	case "anthropic":
		if c.Model == "" {
			c.Model = "claude-3-haiku-20240307"
		}
		if c.Endpoint == "" {
			c.Endpoint = "https://api.anthropic.com/v1/messages"
		}
		if c.APIKeyEnv == "" {
			c.APIKeyEnv = "ANTHROPIC_API_KEY"
		}
	case "custom-http":
		// custom-http requires explicit config, no defaults
	}

	// MaxTokens default for all API providers
	if c.MaxTokens == 0 {
		c.MaxTokens = 1024
	}
}

// SaveConfig saves the configuration to the config file.
func SaveConfig(cfg Config) error {
	configPath := getConfigPath()

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// getConfigPath returns the path to the AI config file.
// Checks local directory first, then ~/.config/muxctl/
func getConfigPath() string {
	// Check local directory first
	localPaths := []string{"ai.yaml", "muxctl.yaml", ".muxctl/ai.yaml"}
	for _, p := range localPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Fall back to home config directory
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "muxctl", "ai.yaml")
}

// GetAPIKey returns the API key from the configured environment variable.
func (c Config) GetAPIKey() string {
	return os.Getenv(c.APIKeyEnv)
}

// IsEnabled returns true if AI features are enabled.
func (c Config) IsEnabled() bool {
	return c.Provider != "none" && c.Provider != ""
}

// IsCLIProvider returns true if the provider uses a CLI tool instead of an API.
func (c Config) IsCLIProvider() bool {
	switch c.Provider {
	case "claude-code", "codex", "gemini", "aider", "cli":
		return true
	default:
		return false
	}
}

// Validate checks if the configuration is valid for use.
func (c Config) Validate() error {
	if !c.IsEnabled() {
		return nil // Disabled is valid
	}

	// CLI-based providers don't require API keys
	if c.IsCLIProvider() {
		// Generic "cli" provider requires cli_command
		if c.Provider == "cli" && c.CLICommand == "" {
			return &ConfigError{
				Field:   "cli_command",
				Message: "cli provider requires cli_command to be set",
			}
		}
		return nil
	}

	// API-based providers require an API key
	if c.GetAPIKey() == "" {
		return &ConfigError{
			Field:   "api_key",
			Message: "API key not found in environment variable " + c.APIKeyEnv,
		}
	}

	return nil
}

// ConfigError represents a configuration error.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return "AI config error (" + e.Field + "): " + e.Message
}

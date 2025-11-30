package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/xunzhou/muxctl/internal/debug"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"`
}

// Client is the interface for AI providers.
type Client interface {
	Chat(ctx context.Context, messages []Message) (string, error)
}

// splitArgs splits args that contain spaces into separate args.
// e.g., "--model gpt-4" becomes ["--model", "gpt-4"]
func splitArgs(args []string) []string {
	var result []string
	for _, arg := range args {
		parts := strings.Fields(arg)
		if len(parts) > 1 {
			result = append(result, parts...)
		} else {
			result = append(result, arg)
		}
	}
	return result
}

// mergeArgs combines default args with config args.
// Config args are split if they contain spaces, then appended after default args.
func mergeArgs(defaults, config []string) []string {
	if len(config) == 0 {
		return defaults
	}
	splitConfig := splitArgs(config)
	result := make([]string, 0, len(defaults)+len(splitConfig))
	result = append(result, defaults...)
	result = append(result, splitConfig...)
	return result
}

// NewClient creates a new AI client based on the configuration.
func NewClient(cfg Config) (Client, error) {
	if !cfg.IsEnabled() {
		return &DisabledClient{}, nil
	}

	switch cfg.Provider {
	// API-based providers
	case "openai":
		return NewOpenAIClient(cfg), nil
	case "anthropic":
		return NewAnthropicClient(cfg), nil
	case "custom-http":
		return NewOpenAIClient(cfg), nil // Use OpenAI-compatible format

	// CLI-based providers (CLICommand is set by applyProviderDefaults)
	case "claude-code":
		return NewCLIClient(cfg.CLICommand, mergeArgs([]string{"-p"}, cfg.CLIArgs), cfg), nil
	case "codex":
		return NewCLIClient(cfg.CLICommand, splitArgs(cfg.CLIArgs), cfg), nil
	case "gemini":
		return NewCLIClient(cfg.CLICommand, splitArgs(cfg.CLIArgs), cfg), nil
	case "aider":
		return NewCLIClient(cfg.CLICommand, mergeArgs([]string{"--message"}, cfg.CLIArgs), cfg), nil
	case "cli":
		if cfg.CLICommand == "" {
			return nil, fmt.Errorf("cli provider requires cli_command in config")
		}
		return NewCLIClient(cfg.CLICommand, splitArgs(cfg.CLIArgs), cfg), nil

	default:
		return nil, fmt.Errorf("unknown AI provider: %s", cfg.Provider)
	}
}

// DisabledClient is a no-op client when AI is disabled.
type DisabledClient struct{}

func (c *DisabledClient) Chat(ctx context.Context, messages []Message) (string, error) {
	return "", fmt.Errorf("AI features are disabled")
}

// OpenAIClient implements the Client interface for OpenAI API.
type OpenAIClient struct {
	cfg        Config
	httpClient *http.Client
}

// NewOpenAIClient creates a new OpenAI client.
func NewOpenAIClient(cfg Config) *OpenAIClient {
	return &OpenAIClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// OpenAI API request/response types
type openAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Chat sends a chat completion request to OpenAI.
func (c *OpenAIClient) Chat(ctx context.Context, messages []Message) (string, error) {
	reqBody := openAIRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: 0.3, // Lower temperature for more focused responses
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Debug: log request
	debug.LogRequest("OpenAI", "POST", c.cfg.Endpoint, jsonBody)

	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.Endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.GetAPIKey())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Debug: log response
	debug.LogResponse("OpenAI", resp.StatusCode, body)

	var result openAIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from AI")
	}

	return result.Choices[0].Message.Content, nil
}

// AnthropicClient implements the Client interface for Anthropic API.
type AnthropicClient struct {
	cfg        Config
	httpClient *http.Client
}

// NewAnthropicClient creates a new Anthropic client.
func NewAnthropicClient(cfg Config) *AnthropicClient {
	// Default to Anthropic endpoint if not specified
	if cfg.Endpoint == "" || cfg.Endpoint == "https://api.openai.com/v1/chat/completions" {
		cfg.Endpoint = "https://api.anthropic.com/v1/messages"
	}
	return &AnthropicClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Anthropic API request/response types
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat sends a chat completion request to Anthropic.
func (c *AnthropicClient) Chat(ctx context.Context, messages []Message) (string, error) {
	// Extract system message and convert to Anthropic format
	var system string
	var anthropicMsgs []anthropicMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			system = msg.Content
		} else {
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	reqBody := anthropicRequest{
		Model:     c.cfg.Model,
		MaxTokens: c.cfg.MaxTokens,
		System:    system,
		Messages:  anthropicMsgs,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Debug: log request
	debug.LogRequest("Anthropic", "POST", c.cfg.Endpoint, jsonBody)

	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.Endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.GetAPIKey())
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Debug: log response
	debug.LogResponse("Anthropic", resp.StatusCode, body)

	var result anthropicResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("no response from AI")
	}

	// Combine all text content
	var text string
	for _, c := range result.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}

	return text, nil
}

// CLIClient implements the Client interface using external CLI tools.
type CLIClient struct {
	command string   // CLI command (e.g., "claude", "codex", "gemini")
	args    []string // Base arguments before the prompt
	cfg     Config
}

// NewCLIClient creates a new CLI-based client.
func NewCLIClient(command string, args []string, cfg Config) *CLIClient {
	return &CLIClient{
		command: command,
		args:    args,
		cfg:     cfg,
	}
}

// Chat sends a prompt to the CLI tool and returns the response.
func (c *CLIClient) Chat(ctx context.Context, messages []Message) (string, error) {
	// Check if the CLI tool is available
	if _, err := exec.LookPath(c.command); err != nil {
		return "", fmt.Errorf("CLI tool '%s' not found in PATH: %w", c.command, err)
	}

	// Build the prompt from messages
	prompt := c.buildPrompt(messages)

	// Build command arguments
	args := make([]string, len(c.args))
	copy(args, c.args)
	args = append(args, prompt)

	// Debug: log CLI command
	debug.LogCLICommand(c.command, args)

	// Create and run the command
	cmd := exec.CommandContext(ctx, c.command, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Debug: log CLI response
	debug.LogCLIResponse(stdout.String(), stderr.String(), err)

	if err != nil {
		// Include stderr in error message for debugging
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("CLI command failed: %s", strings.TrimSpace(errMsg))
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return "", fmt.Errorf("empty response from CLI tool")
	}

	return output, nil
}

// buildPrompt combines messages into a single prompt string for CLI tools.
func (c *CLIClient) buildPrompt(messages []Message) string {
	var parts []string

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			parts = append(parts, fmt.Sprintf("[System]\n%s", msg.Content))
		case "user":
			parts = append(parts, msg.Content)
		case "assistant":
			parts = append(parts, fmt.Sprintf("[Previous response]\n%s", msg.Content))
		}
	}

	return strings.Join(parts, "\n\n")
}

package ai

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	muxctx "github.com/xunzhou/muxctl/internal/context"
)

// ActionType represents the type of AI action.
type ActionType string

const (
	ActionSummarize ActionType = "summarize"
	ActionExplain   ActionType = "explain"
)

// IsCustomAction returns true if the action type is a custom action name.
func (a ActionType) IsCustomAction() bool {
	return a != ActionSummarize && a != ActionExplain
}

// ActionInput contains the input data for an AI action.
type ActionInput struct {
	PaneContent string         // Captured pane content
	Context     muxctx.Context // muxctl context (cluster, namespace, etc.)
	MaxLines    int            // Override for max lines

	// Last command mode (alternative to PaneContent)
	LastCommandMode bool   // If true, use command capture fields below
	Command         string // The last executed command
	CommandOutput   string // Output from the command
	ExitCode        string // Exit code of the command
	ShellType       string // Detected shell type
}

// ActionResult contains the result of an AI action.
type ActionResult struct {
	Content   string
	Truncated bool
	Error     error
}

// Engine provides AI-powered actions.
type Engine struct {
	cfg    Config
	client Client
}

// NewEngine creates a new AI engine.
func NewEngine(cfg Config) (*Engine, error) {
	client, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Engine{
		cfg:    cfg,
		client: client,
	}, nil
}

// IsEnabled returns true if the AI engine is enabled.
func (e *Engine) IsEnabled() bool {
	return e.cfg.IsEnabled()
}

// GetProvider returns the configured AI provider name.
func (e *Engine) GetProvider() string {
	return e.cfg.Provider
}

// CompactConversation triggers conversation summarization/compaction.
// Different providers use different commands:
// - Claude Code: /compact
// - Gemini: /compress
// - Codex: /compact
// - Aider: Not supported (would lose context)
func (e *Engine) CompactConversation(ctx context.Context) error {
	if !e.IsEnabled() {
		return fmt.Errorf("AI features are disabled")
	}

	var compactCmd string
	switch e.cfg.Provider {
	case "claude-code":
		compactCmd = "/compact"
	case "gemini":
		compactCmd = "/compress"
	case "codex":
		compactCmd = "/compact"
	case "aider":
		// Aider's /clear wipes all context, not suitable for compaction
		return fmt.Errorf("compaction not supported for aider (use manual context management)")
	default:
		return fmt.Errorf("compaction not supported for provider: %s", e.cfg.Provider)
	}

	// Send the compact command as a message
	messages := []Message{
		{Role: "user", Content: compactCmd},
	}

	_, err := e.client.Chat(ctx, messages)
	return err
}

// Run executes an AI action.
func (e *Engine) Run(ctx context.Context, action ActionType, input ActionInput) (*ActionResult, error) {
	if !e.IsEnabled() {
		return nil, fmt.Errorf("AI features are disabled")
	}

	var messages []Message
	truncated := false

	if input.LastCommandMode {
		// Build prompt for last command mode
		messages = e.buildCommandPrompt(action, input)
	} else {
		// Standard pane capture mode
		content := sanitizeContent(input.PaneContent)

		// Get max lines for this action
		maxLines := input.MaxLines
		if maxLines == 0 {
			switch action {
			case ActionSummarize:
				maxLines = e.cfg.DefaultActions.Summarize.MaxLines
			case ActionExplain:
				maxLines = e.cfg.DefaultActions.Explain.MaxLines
			default:
				// Check if it's a custom action with max_lines configured
				if customAction, ok := e.cfg.CustomActions[string(action)]; ok && customAction.MaxLines > 0 {
					maxLines = customAction.MaxLines
				} else {
					maxLines = 200
				}
			}
		}

		// Truncate content if needed
		lines := strings.Split(content, "\n")
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
			truncated = true
		}
		content = strings.Join(lines, "\n")

		// Build messages based on action type
		switch action {
		case ActionSummarize:
			messages = e.buildSummarizePrompt(input.Context, content, truncated, maxLines)
		case ActionExplain:
			messages = e.buildExplainPrompt(input.Context, content, truncated, maxLines)
		default:
			// Check if it's a custom action
			customAction, ok := e.cfg.CustomActions[string(action)]
			if !ok {
				return nil, fmt.Errorf("unknown action type: %s", action)
			}
			messages = e.buildCustomPrompt(customAction, input.Context, content, truncated, maxLines)
		}
	}

	// Call AI
	response, err := e.client.Chat(ctx, messages)
	if err != nil {
		return &ActionResult{Error: err}, err
	}

	return &ActionResult{
		Content:   response,
		Truncated: truncated,
	}, nil
}

// buildSummarizePrompt builds the prompt for log summarization.
func (e *Engine) buildSummarizePrompt(ctx muxctx.Context, content string, truncated bool, maxLines int) []Message {
	// Use custom prompts if configured
	settings := e.cfg.DefaultActions.Summarize

	systemPrompt := settings.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = `You are analyzing terminal output. Be concise and actionable.`
	}

	contextInfo := buildContextInfo(ctx)
	truncateNote := ""
	if truncated {
		truncateNote = fmt.Sprintf("\n(Note: Showing last %d lines, earlier content truncated)", maxLines)
	}

	userPrompt := settings.UserPrompt
	if userPrompt == "" {
		userPrompt = fmt.Sprintf(`Context:
%s

Here is the terminal output:%s

%s

Tasks:
1. Briefly summarize what's happening (2-3 sentences max).
2. Highlight any errors, warnings, or anomalies.
3. Suggest 2-3 concrete next steps.`, contextInfo, truncateNote, content)
	} else {
		// Replace template variables
		userPrompt = strings.ReplaceAll(userPrompt, "{{context}}", contextInfo)
		userPrompt = strings.ReplaceAll(userPrompt, "{{content}}", content)
		userPrompt = strings.ReplaceAll(userPrompt, "{{truncated}}", truncateNote)
	}

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// buildExplainPrompt builds the prompt for error explanation.
func (e *Engine) buildExplainPrompt(ctx muxctx.Context, content string, truncated bool, maxLines int) []Message {
	// Use custom prompts if configured
	settings := e.cfg.DefaultActions.Explain

	systemPrompt := settings.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = `You are helping interpret CLI output and error messages. Be concise and provide actionable guidance.`
	}

	contextInfo := buildContextInfo(ctx)
	truncateNote := ""
	if truncated {
		truncateNote = fmt.Sprintf("\n(Note: Showing last %d lines)", maxLines)
	}

	userPrompt := settings.UserPrompt
	if userPrompt == "" {
		userPrompt = fmt.Sprintf(`Context:
%s

CLI output:%s

%s

Tasks:
1. Identify the most likely root cause (1-2 sentences).
2. Explain the error in simple terms.
3. Suggest 2-3 concrete commands or checks to run next.`, contextInfo, truncateNote, content)
	} else {
		// Replace template variables
		userPrompt = strings.ReplaceAll(userPrompt, "{{context}}", contextInfo)
		userPrompt = strings.ReplaceAll(userPrompt, "{{content}}", content)
		userPrompt = strings.ReplaceAll(userPrompt, "{{truncated}}", truncateNote)
	}

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// buildCustomPrompt builds the prompt for a custom action.
func (e *Engine) buildCustomPrompt(action *CustomAction, ctx muxctx.Context, content string, truncated bool, maxLines int) []Message {
	contextInfo := buildContextInfo(ctx)
	truncateNote := ""
	if truncated {
		truncateNote = fmt.Sprintf("\n(Note: Showing last %d lines, earlier content truncated)", maxLines)
	}

	systemPrompt := action.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are analyzing terminal output. Be concise and actionable."
	}

	userPrompt := action.UserPrompt
	// Replace template variables
	userPrompt = strings.ReplaceAll(userPrompt, "{{context}}", contextInfo)
	userPrompt = strings.ReplaceAll(userPrompt, "{{content}}", content)
	userPrompt = strings.ReplaceAll(userPrompt, "{{truncated}}", truncateNote)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// buildCommandPrompt builds the prompt for last command mode.
func (e *Engine) buildCommandPrompt(action ActionType, input ActionInput) []Message {
	contextInfo := buildContextInfo(input.Context)

	// Build exit code info
	exitInfo := ""
	if input.ExitCode != "" {
		if input.ExitCode == "0" {
			exitInfo = "Exit code: 0 (success)"
		} else {
			exitInfo = fmt.Sprintf("Exit code: %s (failure)", input.ExitCode)
		}
	}

	// Sanitize the command output
	output := sanitizeContent(input.CommandOutput)

	var systemPrompt, userPrompt string

	switch action {
	case ActionSummarize:
		systemPrompt = `You are analyzing a single command execution. Be concise and actionable.`
		userPrompt = fmt.Sprintf(`Context:
%s

Command executed:
%s

%s

Output:
%s

Tasks:
1. Briefly summarize what this command did and its result (1-2 sentences).
2. If the command failed, explain why.
3. Suggest 1-2 concrete next steps if applicable.`, contextInfo, input.Command, exitInfo, output)

	case ActionExplain:
		systemPrompt = `You are helping interpret a command and its output. Be concise and provide actionable guidance.`
		userPrompt = fmt.Sprintf(`Context:
%s

Command executed:
%s

%s

Output:
%s

Tasks:
1. Explain what this command does.
2. Interpret the output - what does it mean?
3. If there are errors, explain the root cause and how to fix them.`, contextInfo, input.Command, exitInfo, output)

	default:
		systemPrompt = `You are analyzing terminal output.`
		userPrompt = fmt.Sprintf(`Command: %s
%s

Output:
%s`, input.Command, exitInfo, output)
	}

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// buildContextInfo formats the muxctl context for prompts.
func buildContextInfo(ctx muxctx.Context) string {
	var parts []string

	if ctx.Cluster != "" {
		parts = append(parts, fmt.Sprintf("- Cluster: %s", ctx.Cluster))
	}
	if ctx.Namespace != "" {
		parts = append(parts, fmt.Sprintf("- Namespace: %s", ctx.Namespace))
	}
	if ctx.Environment != "" {
		parts = append(parts, fmt.Sprintf("- Environment: %s", ctx.Environment))
	}
	if ctx.Region != "" {
		parts = append(parts, fmt.Sprintf("- Region: %s", ctx.Region))
	}
	// Include custom metadata
	for k, v := range ctx.Metadata {
		parts = append(parts, fmt.Sprintf("- %s: %s", k, v))
	}

	if len(parts) == 0 {
		return "- No specific context available"
	}

	return strings.Join(parts, "\n")
}

// sanitizeContent removes sensitive information and cleans up the content.
func sanitizeContent(content string) string {
	// Strip ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	content = ansiRegex.ReplaceAllString(content, "")

	// Remove common secret patterns
	secretPatterns := []struct {
		pattern *regexp.Regexp
		replace string
	}{
		{regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[=:]\s*\S+`), "$1=[REDACTED]"},
		{regexp.MustCompile(`(?i)(token|api_key|apikey|secret|auth)\s*[=:]\s*\S+`), "$1=[REDACTED]"},
		{regexp.MustCompile(`(?i)(bearer)\s+\S+`), "$1 [REDACTED]"},
		{regexp.MustCompile(`(?i)(authorization)\s*[=:]\s*\S+`), "$1=[REDACTED]"},
	}

	for _, sp := range secretPatterns {
		content = sp.pattern.ReplaceAllString(content, sp.replace)
	}

	// Compress contiguous empty lines
	emptyLines := regexp.MustCompile(`\n{3,}`)
	content = emptyLines.ReplaceAllString(content, "\n\n")

	return strings.TrimSpace(content)
}

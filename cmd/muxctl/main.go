package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/xunzhou/muxctl/internal/ai"
	muxctx "github.com/xunzhou/muxctl/internal/context"
	"github.com/xunzhou/muxctl/internal/debug"
	"github.com/xunzhou/muxctl/internal/tmux"
	"github.com/xunzhou/muxctl/internal/ui"
	pkgai "github.com/xunzhou/muxctl/pkg/ai"
)

const (
	defaultSessionName = "muxctl"
	version            = "0.2.0"
)

var (
	tmuxCtrl    *tmux.TmuxController
	ctxManager  *muxctx.ContextManager
	debugMode   bool
	sessionName string
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		formatError(err)
		os.Exit(1)
	}
}

func formatError(err error) {
	errStr := err.Error()
	fmt.Fprintln(os.Stderr, "\n\033[31m✗ Error\033[0m")
	fmt.Fprintln(os.Stderr, strings.Repeat("─", 50))

	words := strings.Fields(errStr)
	var line string
	maxWidth := 48

	for _, word := range words {
		if len(line)+len(word)+1 > maxWidth {
			fmt.Fprintln(os.Stderr, "  "+line)
			line = word
		} else if line == "" {
			line = word
		} else {
			line += " " + word
		}
	}
	if line != "" {
		fmt.Fprintln(os.Stderr, "  "+line)
	}

	fmt.Fprintln(os.Stderr, strings.Repeat("─", 50))
	fmt.Fprintln(os.Stderr)
}

var rootCmd = &cobra.Command{
	Use:           "muxctl",
	Short:         "Terminal multiplexer orchestration layer",
	Long:          "muxctl provides stable pane management for tmux, enabling applications to route commands to specific panes without managing tmux directly.",
	Version:       version,
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if debugMode {
			if err := debug.Enable(); err != nil {
				return fmt.Errorf("failed to enable debug logging: %w", err)
			}
			debug.Log("Command: %s %v", cmd.Name(), args)
		}
		return nil
	},
}

// === Session Commands ===

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize muxctl session with 3-pane layout",
	Long: `Creates or ensures a tmux session with the standard 3-pane layout:
  +------------------+
  |       top        |
  +------------------+
  |   left  |  right |
  +------------------+

Pane IDs are stored in tmux session variables (@muxctl_top, @muxctl_left, @muxctl_right)
for stable references across restarts.

This command is idempotent - safe to call multiple times.`,
	RunE: runInit,
}

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to an existing muxctl session",
	Long:  "Attaches to an existing muxctl session, or runs init first if none exists.",
	RunE:  runAttach,
}

// === Pane Commands ===

var runCmd = &cobra.Command{
	Use:   "run [flags] -- <command> [args...]",
	Short: "Run a command in a specific pane",
	Long: `Runs a command in the specified pane. The pane must be initialized first.

Examples:
  muxctl run --pane top -- my-tui-app
  muxctl run --pane left -- kubectl logs -f pod-name
  muxctl run --pane right -- kubectl describe pod pod-name`,
	RunE:               runRun,
	DisableFlagParsing: false,
}

var focusCmd = &cobra.Command{
	Use:   "focus <pane>",
	Short: "Focus on a specific pane",
	Long: `Switches focus to the specified pane.

Pane roles: top, left, right

Examples:
  muxctl focus left
  muxctl focus right
  muxctl focus top`,
	Args: cobra.ExactArgs(1),
	RunE: runFocus,
}

var clearCmd = &cobra.Command{
	Use:   "clear <pane>",
	Short: "Clear a pane (Ctrl-C + clear)",
	Long: `Clears the specified pane by sending Ctrl-C to stop any running command,
then running 'clear' to clear the screen.

Pane roles: top, left, right

Examples:
  muxctl clear left
  muxctl clear right`,
	Args: cobra.ExactArgs(1),
	RunE: runClear,
}

var sendCmd = &cobra.Command{
	Use:   "send [flags] <keys>",
	Short: "Send raw keystrokes to a pane",
	Long: `Sends raw keystrokes to the specified pane. Useful for interactive commands.

Special keys: C-c (Ctrl-C), Enter, Escape, etc.

Examples:
  muxctl send --pane left "C-c"
  muxctl send --pane top "q"
  muxctl send --pane right "Enter"`,
	Args: cobra.ExactArgs(1),
	RunE: runSend,
}

// === Convenience Commands ===

var logsCmd = &cobra.Command{
	Use:   "logs [pod-selector]",
	Short: "Run kubectl logs in the left pane",
	Long: `Convenience command to run kubectl logs in the left pane.
Equivalent to: muxctl run --pane left -- kubectl logs ...`,
	RunE: runLogs,
}

// === AI Commands ===

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "AI-powered context analysis",
	Long:  "Use AI to analyze pane content, summarize output, and explain errors.",
}

var aiSummarizeCmd = &cobra.Command{
	Use:   "summarize",
	Short: "Summarize content from a pane",
	RunE:  runAISummarize,
}

var aiExplainCmd = &cobra.Command{
	Use:   "explain",
	Short: "Explain errors from a pane",
	RunE:  runAIExplain,
}

var aiConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Show AI configuration",
	RunE:  runAIConfig,
}

var aiServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start AI socket server for external requests",
	Long: `Starts a Unix socket server that accepts AI requests from external processes.

The server listens on /tmp/muxctl-{session}.sock and accepts JSON requests.
This allows other tools (like sctl) to request AI analysis without calling muxctl directly.

The server runs until interrupted (Ctrl-C).`,
	RunE: runAIServe,
}

var aiRequestCmd = &cobra.Command{
	Use:   "request",
	Short: "Send an AI request via socket or stdin",
	Long: `Sends an AI request to the socket server or reads from stdin.

This command can be used to test the socket protocol or send requests programmatically.

Examples:
  # Send request to running server
  echo '{"action":"summarize","source_pane":"left","target_pane":"right","context":{}}' | muxctl ai request

  # With context file
  muxctl ai request --context-file /tmp/context.json --action summarize --source left --target right`,
	RunE: runAIRequest,
}

// === Status Command ===

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show muxctl session status",
	Long:  "Displays information about the current muxctl session and pane assignments.",
	RunE:  runStatus,
}

// === Start Command (TUI) ===

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the muxctl TUI dashboard",
	Long: `Starts the interactive TUI dashboard in the top pane.

The dashboard provides:
  - Current context display (cluster, namespace, etc.)
  - Quick actions: logs, shell, AI summarize/explain
  - Keyboard navigation

If the session doesn't exist, it will be created first.`,
	RunE: runStart,
}

// === Kill Command ===

var killCmd = &cobra.Command{
	Use:   "kill",
	Short: "Kill the muxctl session",
	Long: `Terminates the muxctl tmux session.

This will close all panes and stop any running commands in the session.`,
	RunE: runKill,
}

// === Completion Command ===

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for muxctl.

To load completions:

Bash:
  $ source <(muxctl completion bash)
  # To load completions for each session, add to ~/.bashrc:
  # source <(muxctl completion bash)

Zsh:
  $ source <(muxctl completion zsh)
  # To load completions for each session, add to ~/.zshrc:
  # source <(muxctl completion zsh)

Fish:
  $ muxctl completion fish | source
  # To load completions for each session:
  $ muxctl completion fish > ~/.config/fish/completions/muxctl.fish

PowerShell:
  PS> muxctl completion powershell | Out-String | Invoke-Expression
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE:                  runCompletion,
}

// === Flags ===

var (
	// Init flags
	initTopPercent  int
	initSidePercent int
	initNoAttach    bool

	// Run/send flags
	paneRole string

	// Logs flags
	logsFollow    bool
	logsTail      int
	logsContainer string

	// AI flags
	aiPaneRole    string
	aiMaxLines    int
	aiLastCommand bool
	aiContextFile string
	aiTargetPane  string
)

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "Enable debug logging to /tmp/muxctl-debug.log")
	rootCmd.PersistentFlags().StringVarP(&sessionName, "session", "s", defaultSessionName, "tmux session name")

	// Commands
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(focusCmd)
	rootCmd.AddCommand(clearCmd)
	rootCmd.AddCommand(sendCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(aiCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(completionCmd)

	// AI subcommands
	aiCmd.AddCommand(aiSummarizeCmd)
	aiCmd.AddCommand(aiExplainCmd)
	aiCmd.AddCommand(aiConfigCmd)
	aiCmd.AddCommand(aiServeCmd)
	aiCmd.AddCommand(aiRequestCmd)

	// Register custom AI actions from config
	registerCustomAICommands()

	// Init flags
	initCmd.Flags().IntVar(&initTopPercent, "top-percent", 30, "Percentage of screen for top pane")
	initCmd.Flags().IntVar(&initSidePercent, "side-percent", 40, "Percentage of bottom for side pane")
	initCmd.Flags().BoolVar(&initNoAttach, "no-attach", false, "Don't attach after init (for scripting)")

	// Run flags
	runCmd.Flags().StringVarP(&paneRole, "pane", "p", "", "Target pane (required: top, left, right)")
	runCmd.MarkFlagRequired("pane")

	// Send flags
	sendCmd.Flags().StringVarP(&paneRole, "pane", "p", "", "Target pane (required: top, left, right)")
	sendCmd.MarkFlagRequired("pane")

	// Logs flags
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
	logsCmd.Flags().IntVarP(&logsTail, "tail", "t", 100, "Number of lines to show from the end")
	logsCmd.Flags().StringVarP(&logsContainer, "container", "c", "", "Container name")

	// AI flags
	aiSummarizeCmd.Flags().StringVarP(&aiPaneRole, "pane", "p", "left", "Pane to capture (top, left, right)")
	aiSummarizeCmd.Flags().IntVarP(&aiMaxLines, "lines", "n", 0, "Max lines to capture")
	aiSummarizeCmd.Flags().BoolVarP(&aiLastCommand, "last-command", "l", false, "Capture only last command, output, and exit code")
	aiSummarizeCmd.Flags().StringVar(&aiContextFile, "context-file", "", "JSON file with context bundle")
	aiSummarizeCmd.Flags().StringVar(&aiTargetPane, "target", "", "Target pane for output (default: stdout)")

	aiExplainCmd.Flags().StringVarP(&aiPaneRole, "pane", "p", "left", "Pane to capture")
	aiExplainCmd.Flags().IntVarP(&aiMaxLines, "lines", "n", 0, "Max lines to capture")
	aiExplainCmd.Flags().BoolVarP(&aiLastCommand, "last-command", "l", false, "Capture only last command, output, and exit code")
	aiExplainCmd.Flags().StringVar(&aiContextFile, "context-file", "", "JSON file with context bundle")
	aiExplainCmd.Flags().StringVar(&aiTargetPane, "target", "", "Target pane for output (default: stdout)")

	// AI request flags
	aiRequestCmd.Flags().StringVar(&aiContextFile, "context-file", "", "JSON file with context")
	aiRequestCmd.Flags().StringVar(&aiPaneRole, "source", "left", "Source pane to capture")
	aiRequestCmd.Flags().StringVar(&aiTargetPane, "target", "right", "Target pane for output")

	// Initialize controllers
	tmuxCtrl = tmux.NewController()
	ctxManager = muxctx.NewManager()
}

// === Command Implementations ===

func runInit(cmd *cobra.Command, args []string) error {
	if !tmuxCtrl.Available() {
		return fmt.Errorf("tmux is not installed or not in PATH")
	}

	layout := tmux.LayoutDef{
		TopPercent:  initTopPercent,
		SidePercent: initSidePercent,
	}

	if err := tmuxCtrl.Init(sessionName, layout); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	fmt.Printf("Initialized muxctl session '%s' with 3-pane layout\n", sessionName)
	fmt.Printf("  @muxctl_top   → top pane\n")
	fmt.Printf("  @muxctl_left  → left pane (bottom-left)\n")
	fmt.Printf("  @muxctl_right → right pane (bottom-right)\n")

	if initNoAttach {
		return nil
	}

	// Attach to session
	return tmuxCtrl.Attach(sessionName)
}

func runAttach(cmd *cobra.Command, args []string) error {
	if !tmuxCtrl.Available() {
		return fmt.Errorf("tmux is not installed or not in PATH")
	}

	if !tmuxCtrl.SessionExists(sessionName) {
		fmt.Printf("No existing session. Running init...\n")
		return runInit(cmd, args)
	}

	return tmuxCtrl.Attach(sessionName)
}

func runRun(cmd *cobra.Command, args []string) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	role, err := tmux.ParseRole(paneRole)
	if err != nil {
		return err
	}

	// Find command args after "--"
	cmdArgs := args
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			cmdArgs = os.Args[i+1:]
			break
		}
	}

	if len(cmdArgs) == 0 {
		return fmt.Errorf("no command specified. Usage: muxctl run --pane <role> -- <command>")
	}

	// Load context for env vars
	ctxManager.Refresh()
	ctx := ctxManager.Current()

	if err := tmuxCtrl.RunInPane(role, cmdArgs, ctx.Env()); err != nil {
		return fmt.Errorf("failed to run in pane '%s': %w", role, err)
	}

	return nil
}

func runFocus(cmd *cobra.Command, args []string) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	role, err := tmux.ParseRole(args[0])
	if err != nil {
		return err
	}

	if err := tmuxCtrl.FocusPane(role); err != nil {
		return fmt.Errorf("failed to focus pane '%s': %w", role, err)
	}

	return nil
}

func runClear(cmd *cobra.Command, args []string) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	role, err := tmux.ParseRole(args[0])
	if err != nil {
		return err
	}

	if err := tmuxCtrl.ClearPane(role); err != nil {
		return fmt.Errorf("failed to clear pane '%s': %w", role, err)
	}

	return nil
}

func runSend(cmd *cobra.Command, args []string) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	role, err := tmux.ParseRole(paneRole)
	if err != nil {
		return err
	}

	if err := tmuxCtrl.SendKeys(role, args[0]); err != nil {
		return fmt.Errorf("failed to send keys to pane '%s': %w", role, err)
	}

	return nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	// Refresh context
	ctxManager.Refresh()
	ctx := ctxManager.Current()

	// Build kubectl logs command
	kubectlArgs := []string{"kubectl", "logs"}

	if ctx.Namespace != "" {
		kubectlArgs = append(kubectlArgs, "-n", ctx.Namespace)
	}

	if len(args) > 0 {
		kubectlArgs = append(kubectlArgs, args[0])
		if logsFollow {
			kubectlArgs = append(kubectlArgs, "-f")
		}
		if logsTail > 0 {
			kubectlArgs = append(kubectlArgs, fmt.Sprintf("--tail=%d", logsTail))
		}
		if logsContainer != "" {
			kubectlArgs = append(kubectlArgs, "-c", logsContainer)
		}
	} else {
		// No pod specified - show pods
		kubectlArgs = []string{"kubectl", "get", "pods"}
		if ctx.Namespace != "" {
			kubectlArgs = append(kubectlArgs, "-n", ctx.Namespace)
		}
	}

	if err := tmuxCtrl.RunInPane(tmux.RoleLeft, kubectlArgs, ctx.Env()); err != nil {
		return fmt.Errorf("failed to run logs: %w", err)
	}

	// Focus on left pane
	tmuxCtrl.FocusPane(tmux.RoleLeft)

	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	if !tmuxCtrl.Available() {
		return fmt.Errorf("tmux is not installed")
	}

	fmt.Printf("muxctl status\n")
	fmt.Printf("─────────────\n")

	if !tmuxCtrl.SessionExists(sessionName) {
		fmt.Printf("Session: not initialized\n")
		fmt.Printf("\nRun 'muxctl init' to create the session.\n")
		return nil
	}

	fmt.Printf("Session: %s (exists)\n", sessionName)

	// Initialize controller to read session vars
	tmuxCtrl.EnsureSession(sessionName)

	// Check each pane
	for _, role := range tmux.ValidRoles() {
		paneID, ok := tmuxCtrl.GetPaneID(role)
		if ok {
			fmt.Printf("  @muxctl_%-4s → %s ✓\n", role, paneID)
		} else {
			fmt.Printf("  @muxctl_%-4s → (not found) ✗\n", role)
		}
	}

	// Show if inside tmux
	if tmux.InsideTmux() {
		currentSession := tmux.GetCurrentSession()
		if currentSession == sessionName {
			fmt.Printf("\nCurrently inside muxctl session.\n")
		} else {
			fmt.Printf("\nInside tmux session '%s' (not muxctl).\n", currentSession)
		}
	} else {
		fmt.Printf("\nNot inside tmux. Run 'muxctl attach' to connect.\n")
	}

	return nil
}

// === AI Commands ===

// registerCustomAICommands dynamically registers custom AI actions from config as subcommands.
func registerCustomAICommands() {
	cfg, err := ai.LoadConfig()
	if err != nil {
		// Silently skip if config can't be loaded - will error at runtime
		return
	}

	if len(cfg.CustomActions) == 0 {
		return
	}

	for name, action := range cfg.CustomActions {
		// Capture variables for closure
		actionName := name
		actionDef := action

		description := actionDef.Description
		if description == "" {
			description = fmt.Sprintf("Run custom action '%s'", actionName)
		}

		cmd := &cobra.Command{
			Use:   actionName,
			Short: description,
			Annotations: map[string]string{
				"custom": "true",
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				return runAIAction(ai.ActionType(actionName))
			},
		}

		// Add standard AI flags
		cmd.Flags().StringVarP(&aiPaneRole, "pane", "p", "left", "Pane to capture (top, left, right)")
		cmd.Flags().IntVarP(&aiMaxLines, "lines", "n", 0, "Max lines to capture")
		cmd.Flags().BoolVarP(&aiLastCommand, "last-command", "l", false, "Capture only last command, output, and exit code")

		aiCmd.AddCommand(cmd)
	}

	// Set custom help template for ai command
	aiCmd.SetHelpTemplate(aiHelpTemplate)
}

const aiHelpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (and (not (index .Annotations "custom")) .IsAvailableCommand)}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

Custom Actions:{{range .Commands}}{{if (and (index .Annotations "custom") .IsAvailableCommand)}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

func runAISummarize(cmd *cobra.Command, args []string) error {
	return runAIAction(ai.ActionSummarize)
}

func runAIExplain(cmd *cobra.Command, args []string) error {
	return runAIAction(ai.ActionExplain)
}

func runAIConfig(cmd *cobra.Command, args []string) error {
	cfg, err := ai.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Printf("AI Configuration:\n")
	fmt.Printf("  Provider:    %s\n", cfg.Provider)
	if cfg.Model != "" {
		fmt.Printf("  Model:       %s\n", cfg.Model)
	}
	if cfg.Endpoint != "" {
		fmt.Printf("  Endpoint:    %s\n", cfg.Endpoint)
	}
	if cfg.APIKeyEnv != "" {
		fmt.Printf("  API Key Env: %s\n", cfg.APIKeyEnv)
	}
	if cfg.MaxTokens != 0 {
		fmt.Printf("  Max Tokens:  %d\n", cfg.MaxTokens)
	}
	if cfg.CLICommand != "" {
		fmt.Printf("  CLI Command: %s\n", cfg.CLICommand)
	}
	if len(cfg.CLIArgs) > 0 {
		fmt.Printf("  CLI Args:    %v\n", cfg.CLIArgs)
	}

	// Show custom actions
	if len(cfg.CustomActions) > 0 {
		fmt.Printf("\nCustom Actions:\n")
		for name, action := range cfg.CustomActions {
			desc := action.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Printf("  %s: %s\n", name, desc)
		}
	}

	if err := cfg.Validate(); err != nil {
		fmt.Printf("\nValidation Error: %v\n", err)
	} else {
		fmt.Printf("\nConfig is valid.\n")
	}

	return nil
}

func runAIAction(action ai.ActionType) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	// Load AI config
	aiCfg, err := ai.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load AI config: %w", err)
	}

	if !aiCfg.IsEnabled() {
		return fmt.Errorf("AI features are disabled (provider: none)")
	}

	if err := aiCfg.Validate(); err != nil {
		return fmt.Errorf("AI config error: %w", err)
	}

	// Create AI engine
	engine, err := ai.NewEngine(aiCfg)
	if err != nil {
		return fmt.Errorf("failed to create AI engine: %w", err)
	}

	// Resolve pane role
	role, err := tmux.ParseRole(aiPaneRole)
	if err != nil {
		return err
	}

	// Get context - from file if provided, otherwise from kubectl
	var ctx muxctx.Context
	if aiContextFile != "" {
		ctx, err = loadContextFromFile(aiContextFile)
		if err != nil {
			return fmt.Errorf("failed to load context file: %w", err)
		}
	} else {
		ctxManager.Refresh()
		ctx = ctxManager.Current()
	}

	var input ai.ActionInput

	if aiLastCommand {
		// Last command mode: capture command, output, and exit code
		fmt.Printf("Capturing last command from pane '%s'...\n", aiPaneRole)

		cmdCapture, err := tmuxCtrl.CaptureLastCommand(role)
		if err != nil {
			return fmt.Errorf("failed to capture last command: %w", err)
		}

		if cmdCapture.Command == "" {
			return fmt.Errorf("could not detect last command (empty history?)")
		}

		fmt.Printf("Command: %s\n", cmdCapture.Command)
		if cmdCapture.ExitCode != "" {
			fmt.Printf("Exit code: %s\n", cmdCapture.ExitCode)
		}
		fmt.Printf("Shell: %s\n\n", cmdCapture.Shell)

		input = ai.ActionInput{
			Context:         ctx,
			LastCommandMode: true,
			Command:         cmdCapture.Command,
			CommandOutput:   cmdCapture.Output,
			ExitCode:        cmdCapture.ExitCode,
			ShellType:       string(cmdCapture.Shell),
		}
	} else {
		// Standard mode: capture pane content
		maxLines := aiMaxLines
		if maxLines == 0 {
			switch action {
			case ai.ActionSummarize:
				maxLines = aiCfg.DefaultActions.Summarize.MaxLines
			case ai.ActionExplain:
				maxLines = aiCfg.DefaultActions.Explain.MaxLines
			default:
				maxLines = 200
			}
		}

		content, err := tmuxCtrl.CapturePane(role, maxLines)
		if err != nil {
			return fmt.Errorf("failed to capture pane: %w", err)
		}

		input = ai.ActionInput{
			PaneContent: content,
			Context:     ctx,
			MaxLines:    maxLines,
		}
	}

	// Run AI action
	fmt.Printf("Running AI %s...\n\n", action)

	result, err := engine.Run(context.Background(), action, input)
	if err != nil {
		return fmt.Errorf("AI action failed: %w", err)
	}

	if result.Truncated {
		fmt.Printf("(Note: Input was truncated to last %d lines)\n\n", input.MaxLines)
	}

	// If target pane is specified, display result there with a pager
	if aiTargetPane != "" {
		targetRole, err := tmux.ParseRole(aiTargetPane)
		if err != nil {
			return fmt.Errorf("invalid target pane: %w", err)
		}

		// Write result to temp file (JSON format)
		resultFile := "/tmp/muxctl-ai-result.json"
		if err := os.WriteFile(resultFile, []byte(result.Content), 0644); err != nil {
			return fmt.Errorf("failed to write result file: %w", err)
		}

		// Clear and display in target pane using jq + glow pipeline
		tmuxCtrl.ClearPane(targetRole)
		cmd := fmt.Sprintf("'jq -r .result %s | glow -p'", resultFile)
		if err := tmuxCtrl.RunInPane(targetRole, []string{"$SHELL", "-c", cmd}, nil); err != nil {
			return fmt.Errorf("failed to display in pane: %w", err)
		}

		fmt.Printf("Result displayed in %s pane\n", aiTargetPane)
	} else {
		fmt.Println(result.Content)
		fmt.Println()
	}

	return nil
}

// === Helpers ===

func requireMuxctlSession() error {
	if !tmuxCtrl.Available() {
		return fmt.Errorf("tmux is not installed")
	}

	// Check if muxctl session exists (works from inside or outside tmux)
	if !tmuxCtrl.SessionExists(sessionName) {
		return fmt.Errorf("muxctl session '%s' not running. Run 'muxctl init' first", sessionName)
	}

	// If inside tmux, verify we're in the muxctl session (optional warning)
	if tmux.InsideTmux() {
		currentSession := tmux.GetCurrentSession()
		if currentSession != sessionName {
			// Allow operation but log a note - user might be controlling muxctl from another session
			debug.Log("Warning: inside tmux session '%s', targeting muxctl session '%s'", currentSession, sessionName)
		}
	}

	// Initialize controller with muxctl session
	tmuxCtrl.EnsureSession(sessionName)

	return nil
}

// === Start Command Implementation ===

func runStart(cmd *cobra.Command, args []string) error {
	if !tmuxCtrl.Available() {
		return fmt.Errorf("tmux is not installed or not in PATH")
	}

	// Initialize session if it doesn't exist
	if !tmuxCtrl.SessionExists(sessionName) {
		layout := tmux.LayoutDef{
			TopPercent:  initTopPercent,
			SidePercent: initSidePercent,
		}
		if err := tmuxCtrl.Init(sessionName, layout); err != nil {
			return fmt.Errorf("failed to initialize session: %w", err)
		}
		fmt.Printf("Initialized muxctl session '%s'\n", sessionName)
	} else {
		tmuxCtrl.EnsureSession(sessionName)
	}

	// Get initial context
	ctxManager.Refresh()
	ctx := ctxManager.Current()

	// Create context update channel and subscribe
	ctxChan := make(chan muxctx.Context, 1)
	ctxManager.Subscribe(ctxChan)

	// Define refresh function
	refreshFunc := func() (muxctx.Context, error) {
		ctxManager.Refresh()
		return ctxManager.Current(), nil
	}

	// Define action function that routes TUI actions to pane commands
	actionFunc := func(action string) error {
		switch action {
		case "logs":
			// Run logs in left pane
			kubectlArgs := []string{"kubectl", "get", "pods"}
			if ctx.Namespace != "" {
				kubectlArgs = append(kubectlArgs, "-n", ctx.Namespace)
			}
			return tmuxCtrl.RunInPane(tmux.RoleLeft, kubectlArgs, ctx.Env())

		case "shell":
			// Open shell in right pane
			return tmuxCtrl.RunInPane(tmux.RoleRight, []string{"$SHELL"}, ctx.Env())

		case "ai-summarize":
			// Run AI summarize on left pane
			content, err := tmuxCtrl.CapturePane(tmux.RoleLeft, 300)
			if err != nil {
				return err
			}
			return runAIOnContent(ai.ActionSummarize, content, ctx)

		case "ai-explain":
			// Run AI explain on left pane
			content, err := tmuxCtrl.CapturePane(tmux.RoleLeft, 100)
			if err != nil {
				return err
			}
			return runAIOnContent(ai.ActionExplain, content, ctx)

		default:
			return fmt.Errorf("unknown action: %s", action)
		}
	}

	// Run TUI
	fmt.Printf("Starting muxctl dashboard...\n")
	return ui.RunTUI(ctx, ctxChan, refreshFunc, actionFunc)
}

// runAIOnContent runs an AI action on the given content and prints results.
func runAIOnContent(action ai.ActionType, content string, ctx muxctx.Context) error {
	aiCfg, err := ai.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load AI config: %w", err)
	}

	if !aiCfg.IsEnabled() {
		return fmt.Errorf("AI features are disabled")
	}

	engine, err := ai.NewEngine(aiCfg)
	if err != nil {
		return fmt.Errorf("failed to create AI engine: %w", err)
	}

	input := ai.ActionInput{
		PaneContent: content,
		Context:     ctx,
	}

	result, err := engine.Run(context.Background(), action, input)
	if err != nil {
		return err
	}

	fmt.Println(result.Content)
	return nil
}

// === Kill Command Implementation ===

func runKill(cmd *cobra.Command, args []string) error {
	if !tmuxCtrl.Available() {
		return fmt.Errorf("tmux is not installed")
	}

	if !tmuxCtrl.SessionExists(sessionName) {
		fmt.Printf("Session '%s' does not exist.\n", sessionName)
		return nil
	}

	// Kill the tmux session
	killCmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	if err := killCmd.Run(); err != nil {
		return fmt.Errorf("failed to kill session '%s': %w", sessionName, err)
	}

	fmt.Printf("Session '%s' has been terminated.\n", sessionName)
	return nil
}

// === Completion Command Implementation ===

func runCompletion(cmd *cobra.Command, args []string) error {
	switch args[0] {
	case "bash":
		return rootCmd.GenBashCompletion(os.Stdout)
	case "zsh":
		return rootCmd.GenZshCompletion(os.Stdout)
	case "fish":
		return rootCmd.GenFishCompletion(os.Stdout, true)
	case "powershell":
		return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
	default:
		return fmt.Errorf("unsupported shell: %s", args[0])
	}
}

// === AI Socket Server Implementation ===

func runAIServe(cmd *cobra.Command, args []string) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	server, err := pkgai.NewServer(sessionName, tmuxCtrl)
	if err != nil {
		return fmt.Errorf("failed to create AI server: %w", err)
	}

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start AI server: %w", err)
	}

	fmt.Printf("AI server listening on %s\n", server.GetSocketPath())
	fmt.Printf("Press Ctrl-C to stop...\n")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Printf("\nShutting down...\n")
	server.Stop()

	return nil
}

func runAIRequest(cmd *cobra.Command, args []string) error {
	if err := requireMuxctlSession(); err != nil {
		return err
	}

	// Build request from flags or stdin
	var req pkgai.Request

	// Check if input is from stdin (piped)
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Reading from pipe/file
		decoder := json.NewDecoder(os.Stdin)
		if err := decoder.Decode(&req); err != nil {
			return fmt.Errorf("failed to parse JSON from stdin: %w", err)
		}
	} else {
		// Build request from flags
		req = pkgai.Request{
			Action:     "summarize", // Default action
			SourcePane: aiPaneRole,
			TargetPane: aiTargetPane,
			Context:    pkgai.RequestContext{},
			Options: pkgai.RequestOptions{
				MaxLines: aiMaxLines,
			},
		}

		// Load context from file if provided
		if aiContextFile != "" {
			data, err := os.ReadFile(aiContextFile)
			if err != nil {
				return fmt.Errorf("failed to read context file: %w", err)
			}
			if err := json.Unmarshal(data, &req.Context); err != nil {
				return fmt.Errorf("failed to parse context file: %w", err)
			}
		}
	}

	// Send to socket server
	client := pkgai.NewClient(sessionName)

	if !client.IsServerRunning() {
		return fmt.Errorf("AI server not running. Start it with: muxctl ai serve")
	}

	resp, err := client.Send(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("AI request failed: %s", resp.Error)
	}

	fmt.Println("Request sent successfully")
	return nil
}

// loadContextFromFile loads context from a JSON file.
func loadContextFromFile(path string) (muxctx.Context, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return muxctx.Context{}, err
	}

	var ctx muxctx.Context
	if err := json.Unmarshal(data, &ctx); err != nil {
		return muxctx.Context{}, err
	}

	return ctx, nil
}

package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/xunzhou/muxctl/internal/debug"
)

// PaneRole identifies the logical role of a pane.
type PaneRole string

const (
	RoleTop   PaneRole = "top"   // Top pane
	RoleLeft  PaneRole = "left"  // Bottom-left pane
	RoleRight PaneRole = "right" // Bottom-right pane
)

// Session variable names for stable pane IDs
const (
	VarPaneTop   = "@muxctl_top"
	VarPaneLeft  = "@muxctl_left"
	VarPaneRight = "@muxctl_right"
)

// PaneRef holds reference info for a tmux pane.
type PaneRef struct {
	ID   string   // tmux pane id, e.g. %1
	Role PaneRole
}

// PaneInfo contains information about a pane.
type PaneInfo struct {
	ID     string
	Index  int
	Title  string
	Active bool
}

// ShellType represents the shell running in a pane.
type ShellType string

const (
	ShellBash    ShellType = "bash"
	ShellZsh     ShellType = "zsh"
	ShellFish    ShellType = "fish"
	ShellUnknown ShellType = "unknown"
)

// CommandCapture contains the last command, its output, and exit code.
type CommandCapture struct {
	Command  string    // The command that was executed
	Output   string    // Output from the command
	ExitCode string    // Exit code (as string, may be empty if unknown)
	Shell    ShellType // Detected shell type
}

// LayoutDef defines a desired pane layout.
type LayoutDef struct {
	TopPercent  int // percentage for top pane (default 30)
	SidePercent int // percentage for side pane (default 40)
}

// DefaultLayout returns the default 3-pane layout.
func DefaultLayout() LayoutDef {
	return LayoutDef{
		TopPercent:  30,
		SidePercent: 40,
	}
}

// Controller provides an interface for tmux operations.
type Controller interface {
	Available() bool
	SessionExists(name string) bool
	EnsureSession(name string) error
	Attach(session string) error
	Init(session string, layout LayoutDef) error
	RunInPane(role PaneRole, cmd []string, env map[string]string) error
	SendKeys(role PaneRole, keys string) error
	CapturePane(role PaneRole, lines int) (string, error)
	CaptureLastCommand(role PaneRole) (*CommandCapture, error)
	FocusPane(role PaneRole) error
	ClearPane(role PaneRole) error
	ListPanes(session string) ([]PaneInfo, error)
	GetPaneID(role PaneRole) (string, bool)
	GetSessionName() string
	DetectShell(role PaneRole) ShellType
	ResizePane(role PaneRole, widthPercent int) error
	GetPaneSize(role PaneRole) (width, height int, err error)
}

// TmuxController implements Controller using tmux commands.
type TmuxController struct {
	sessionName string
}

// NewController creates a new TmuxController.
func NewController() *TmuxController {
	return &TmuxController{}
}

// Available checks if tmux is installed and accessible.
func (c *TmuxController) Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// GetSessionName returns the current session name.
func (c *TmuxController) GetSessionName() string {
	return c.sessionName
}

// SessionExists checks if a tmux session exists.
func (c *TmuxController) SessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// EnsureSession creates a session if it doesn't exist.
func (c *TmuxController) EnsureSession(name string) error {
	c.sessionName = name

	if c.SessionExists(name) {
		return nil
	}

	// Create detached session
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create session %s: %w", name, err)
	}

	return nil
}

// Attach attaches to an existing session.
func (c *TmuxController) Attach(session string) error {
	c.sessionName = session

	// Check if we're already inside tmux
	if os.Getenv("TMUX") != "" {
		// Switch client to the session
		cmd := exec.Command("tmux", "switch-client", "-t", session)
		return cmd.Run()
	}

	// Attach to session
	cmd := exec.Command("tmux", "attach-session", "-t", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getSessionVar gets a tmux session variable.
func (c *TmuxController) getSessionVar(varName string) (string, error) {
	cmd := exec.Command("tmux", "show-options", "-v", "-t", c.sessionName, varName)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// setSessionVar sets a tmux session variable.
func (c *TmuxController) setSessionVar(varName, value string) error {
	cmd := exec.Command("tmux", "set-option", "-t", c.sessionName, varName, value)
	return cmd.Run()
}

// roleToVar maps a pane role to its session variable name.
func roleToVar(role PaneRole) string {
	switch role {
	case RoleTop:
		return VarPaneTop
	case RoleLeft:
		return VarPaneLeft
	case RoleRight:
		return VarPaneRight
	default:
		return ""
	}
}

// GetPaneID returns the pane ID for a given role from session variables.
func (c *TmuxController) GetPaneID(role PaneRole) (string, bool) {
	varName := roleToVar(role)
	if varName == "" {
		return "", false
	}

	paneID, err := c.getSessionVar(varName)
	if err != nil || paneID == "" {
		return "", false
	}

	// Verify pane still exists
	if !c.paneExists(paneID) {
		return "", false
	}

	return paneID, true
}

// paneIDResult holds the result of a parallel pane ID lookup.
type paneIDResult struct {
	role PaneRole
	id   string
	ok   bool
}

// getAllPaneIDs fetches all three pane IDs in parallel.
func (c *TmuxController) getAllPaneIDs() (topID, leftID, rightID string, topOK, leftOK, rightOK bool) {
	results := make(chan paneIDResult, 3)
	roles := []PaneRole{RoleTop, RoleLeft, RoleRight}

	for _, role := range roles {
		go func(r PaneRole) {
			id, ok := c.GetPaneID(r)
			results <- paneIDResult{role: r, id: id, ok: ok}
		}(role)
	}

	for i := 0; i < 3; i++ {
		res := <-results
		switch res.role {
		case RoleTop:
			topID, topOK = res.id, res.ok
		case RoleLeft:
			leftID, leftOK = res.id, res.ok
		case RoleRight:
			rightID, rightOK = res.id, res.ok
		}
	}
	return
}

// paneExists checks if a pane with the given ID exists.
func (c *TmuxController) paneExists(paneID string) bool {
	cmd := exec.Command("tmux", "list-panes", "-t", c.sessionName, "-F", "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == paneID {
			return true
		}
	}
	return false
}

// Init initializes the 3-pane layout with stable pane IDs.
// Layout:
//
//	+------------------+
//	|       top        |
//	+------------------+
//	|   left  |  right |
//	+------------------+
func (c *TmuxController) Init(session string, layout LayoutDef) error {
	c.sessionName = session

	debug.Log("Init: starting session=%s", session)

	// Ensure session exists
	if err := c.EnsureSession(session); err != nil {
		return err
	}

	// Check if all panes are already valid (parallel fetch)
	topID, leftID, rightID, topOK, leftOK, rightOK := c.getAllPaneIDs()

	debug.Log("Init: existing panes top=%s(%v) left=%s(%v) right=%s(%v)",
		topID, topOK, leftID, leftOK, rightID, rightOK)

	if topOK && leftOK && rightOK {
		debug.Log("Init: all panes valid, skipping layout creation")
		return nil // All panes valid, nothing to do
	}

	// Get current panes
	panes, err := c.ListPanes(session)
	if err != nil {
		return fmt.Errorf("failed to list panes: %w", err)
	}

	debug.Log("Init: found %d existing panes", len(panes))

	// If we have a fresh session with 1 pane, set up the layout
	if len(panes) == 1 {
		return c.createLayout(panes[0].ID, layout)
	}

	// Try to recover existing panes by position/count
	if len(panes) >= 3 {
		// Assume existing layout is correct, just re-register
		return c.registerPanes(panes)
	}

	// Partial layout - kill all and recreate
	debug.Log("Init: recreating layout from scratch")
	for _, p := range panes[1:] { // Keep first pane
		exec.Command("tmux", "kill-pane", "-t", p.ID).Run()
	}

	// Refresh pane list
	panes, _ = c.ListPanes(session)
	if len(panes) >= 1 {
		return c.createLayout(panes[0].ID, layout)
	}

	return fmt.Errorf("failed to initialize layout")
}

// createLayout creates the 3-pane layout from a single pane.
func (c *TmuxController) createLayout(basePaneID string, layout LayoutDef) error {
	debug.Log("createLayout: starting from pane %s", basePaneID)

	topPercent := layout.TopPercent
	if topPercent <= 0 {
		topPercent = 30
	}
	sidePercent := layout.SidePercent
	if sidePercent <= 0 {
		sidePercent = 40
	}

	// Step 1: Split horizontally to create top/bottom
	bottomPercent := 100 - topPercent
	cmd := exec.Command("tmux", "split-window", "-t", basePaneID, "-v", "-p", fmt.Sprintf("%d", bottomPercent))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to split top/bottom: %w", err)
	}

	// Get updated pane list
	panes, err := c.ListPanes(c.sessionName)
	if err != nil {
		return fmt.Errorf("failed to list panes after first split: %w", err)
	}

	debug.Log("createLayout: after first split, have %d panes", len(panes))

	if len(panes) < 2 {
		return fmt.Errorf("expected 2 panes after split, got %d", len(panes))
	}

	// panes[0] is top, panes[1] is bottom (which we'll split again)
	bottomPaneID := panes[1].ID

	// Step 2: Split bottom pane vertically to create logs/side
	cmd = exec.Command("tmux", "split-window", "-t", bottomPaneID, "-h", "-p", fmt.Sprintf("%d", sidePercent))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to split logs/side: %w", err)
	}

	// Get final pane list
	panes, err = c.ListPanes(c.sessionName)
	if err != nil {
		return fmt.Errorf("failed to list panes after second split: %w", err)
	}

	debug.Log("createLayout: after second split, have %d panes", len(panes))

	if len(panes) < 3 {
		return fmt.Errorf("expected 3 panes after splits, got %d", len(panes))
	}

	// Register panes - order after splits: top, logs (left bottom), side (right bottom)
	// But we need to identify them correctly
	return c.registerPanes(panes)
}

// registerPanes assigns roles to panes based on their position.
// Expected layout: pane 0 = top, pane 1 = left (bottom-left), pane 2 = right (bottom-right)
func (c *TmuxController) registerPanes(panes []PaneInfo) error {
	if len(panes) < 3 {
		return fmt.Errorf("need at least 3 panes, got %d", len(panes))
	}

	// Store pane IDs in session variables
	// panes are ordered by index: 0=top, 1=bottom-left, 2=bottom-right
	topID := panes[0].ID
	leftID := panes[1].ID
	rightID := panes[2].ID

	debug.Log("registerPanes: top=%s left=%s right=%s", topID, leftID, rightID)

	// Set session variables in parallel
	var wg sync.WaitGroup
	errChan := make(chan error, 3)

	type varSet struct {
		varName string
		value   string
	}
	vars := []varSet{
		{VarPaneTop, topID},
		{VarPaneLeft, leftID},
		{VarPaneRight, rightID},
	}

	for _, v := range vars {
		wg.Add(1)
		go func(varName, value string) {
			defer wg.Done()
			if err := c.setSessionVar(varName, value); err != nil {
				errChan <- fmt.Errorf("failed to set %s: %w", varName, err)
			}
		}(v.varName, v.value)
	}

	wg.Wait()
	close(errChan)

	// Return first error if any
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	// Set MUXCTL session environment variable (for future panes)
	cmd := exec.Command("tmux", "set-environment", "-t", c.sessionName, "MUXCTL", c.sessionName)
	cmd.Run() // Ignore error, non-critical

	// Set pane titles and respawn shells with MUXCTL env vars (no visible commands)
	type paneSetup struct {
		paneID string
		title  string
		role   string
	}
	paneSetups := []paneSetup{
		{topID, "[top]", "top"},
		{leftID, "[left]", "left"},
		{rightID, "[right]", "right"},
	}

	var setupWg sync.WaitGroup
	for _, p := range paneSetups {
		setupWg.Add(1)
		go func(paneID, title, role string) {
			defer setupWg.Done()
			c.setPaneTitle(paneID, title)
			// Respawn pane with env vars pre-set (kills current shell, starts fresh with env)
			exec.Command("tmux", "respawn-pane", "-k", "-t", paneID,
				"-e", fmt.Sprintf("MUXCTL=%s", c.sessionName),
				"-e", fmt.Sprintf("MUXCTL_PANE=%s", role),
			).Run()
		}(p.paneID, p.title, p.role)
	}
	setupWg.Wait()

	// Set up keybindings for pane toggles
	c.setupKeybindings()

	return nil
}

// setupKeybindings configures tmux keybindings for muxctl.
func (c *TmuxController) setupKeybindings() {
	// Find muxctl binary path
	muxctlPath, err := exec.LookPath("muxctl")
	if err != nil {
		muxctlPath = "muxctl"
	}

	// Bind ctrl-j to toggle bottom panes (gives top pane 100% height)
	exec.Command("tmux", "bind-key", "-n", "C-j",
		"run-shell", fmt.Sprintf("%s toggle bottom", muxctlPath)).Run()

	// Bind ctrl-k to toggle top pane (gives bottom panes 100% height)
	exec.Command("tmux", "bind-key", "-n", "C-k",
		"run-shell", fmt.Sprintf("%s toggle top", muxctlPath)).Run()

	// Bind ctrl-s to toggle right pane only
	exec.Command("tmux", "bind-key", "-n", "C-s",
		"run-shell", fmt.Sprintf("%s toggle right", muxctlPath)).Run()

	debug.Log("setupKeybindings: bound ctrl-j=toggle bottom, ctrl-k=toggle top, ctrl-s=toggle right")
}

// setPaneTitle sets the title of a pane.
func (c *TmuxController) setPaneTitle(paneID, title string) error {
	cmd := exec.Command("tmux", "select-pane", "-t", paneID, "-T", title)
	return cmd.Run()
}

// RunInPane runs a command in the specified pane.
func (c *TmuxController) RunInPane(role PaneRole, cmdArgs []string, env map[string]string) error {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	if len(cmdArgs) == 0 {
		return fmt.Errorf("no command specified")
	}

	// Build environment with muxctl detection vars
	allEnv := make(map[string]string)
	// Add muxctl detection variables (like $TMUX for tmux)
	allEnv["MUXCTL"] = c.sessionName
	allEnv["MUXCTL_PANE"] = string(role)
	// Add user-provided env vars (can override if needed)
	for k, v := range env {
		allEnv[k] = v
	}

	var cmdStr string
	if debug.IsEnabled() {
		// Debug mode: show env vars inline for visibility
		var envPrefix string
		for k, v := range allEnv {
			envPrefix += fmt.Sprintf("%s=%q ", k, v)
		}
		cmdStr = envPrefix + strings.Join(cmdArgs, " ")
		debug.Log("RunInPane: role=%s pane=%s cmd=%s", role, paneID, cmdStr)
	} else {
		// Normal mode: write env to temp file and use pipe-pane to suppress source output
		envFile := fmt.Sprintf("/tmp/muxctl-env-%d", os.Getpid())

		// Write env exports to temp file (not visible in pane)
		var envContent string
		for k, v := range allEnv {
			envContent += fmt.Sprintf("export %s=%q\n", k, v)
		}
		if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
			return fmt.Errorf("failed to write env file: %w", err)
		}

		// Use pipe-pane to /dev/null to suppress any output from sourcing
		exec.Command("tmux", "pipe-pane", "-t", paneID, "cat > /dev/null").Run()

		// Source env file silently
		exec.Command("tmux", "send-keys", "-t", paneID, fmt.Sprintf(". %s", envFile), "Enter").Run()

		// Small delay for source to complete
		time.Sleep(10 * time.Millisecond)

		// Stop pipe-pane
		exec.Command("tmux", "pipe-pane", "-t", paneID).Run()

		// Now send the actual command (visible)
		cmdStr = strings.Join(cmdArgs, " ")
	}

	cmd := exec.Command("tmux", "send-keys", "-t", paneID, cmdStr, "Enter")
	return cmd.Run()
}

// SendKeys sends raw keystrokes to a pane.
func (c *TmuxController) SendKeys(role PaneRole, keys string) error {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	debug.Log("SendKeys: role=%s pane=%s keys=%q", role, paneID, keys)

	cmd := exec.Command("tmux", "send-keys", "-t", paneID, keys)
	return cmd.Run()
}

// CapturePane captures the content of a pane.
func (c *TmuxController) CapturePane(role PaneRole, lines int) (string, error) {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return "", fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	startLine := fmt.Sprintf("-%d", lines)
	cmd := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-S", startLine)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to capture pane: %w", err)
	}
	return string(output), nil
}

// DetectShell detects the shell type running in a pane.
func (c *TmuxController) DetectShell(role PaneRole) ShellType {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return ShellUnknown
	}

	// Get the pane's current command using tmux
	cmd := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{pane_current_command}")
	output, err := cmd.Output()
	if err != nil {
		return ShellUnknown
	}

	shellCmd := strings.ToLower(strings.TrimSpace(string(output)))
	debug.Log("DetectShell: pane=%s command=%s", paneID, shellCmd)

	switch {
	case strings.Contains(shellCmd, "fish"):
		return ShellFish
	case strings.Contains(shellCmd, "zsh"):
		return ShellZsh
	case strings.Contains(shellCmd, "bash"):
		return ShellBash
	default:
		// Try to detect from $SHELL or fall back
		return ShellUnknown
	}
}

// CaptureLastCommand captures the last executed command, its output, and exit code.
// It uses the up-arrow trick to recall the last command from shell history.
func (c *TmuxController) CaptureLastCommand(role PaneRole) (*CommandCapture, error) {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return nil, fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	debug.Log("CaptureLastCommand: starting for pane=%s", paneID)

	// Detect shell type first
	shell := c.DetectShell(role)
	debug.Log("CaptureLastCommand: detected shell=%s", shell)

	// Capture current pane state (for output extraction later)
	fullCapture, err := c.CapturePane(role, 500)
	if err != nil {
		return nil, fmt.Errorf("failed to capture pane: %w", err)
	}

	// Step 1: Send Up arrow to recall last command
	cmd := exec.Command("tmux", "send-keys", "-t", paneID, "Up")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to send up arrow: %w", err)
	}

	// Wait for shell to respond
	time.Sleep(50 * time.Millisecond)

	// Step 2: Capture the command line
	cmdLineCapture, err := c.CapturePane(role, 50)
	if err != nil {
		return nil, fmt.Errorf("failed to capture command line: %w", err)
	}

	// Step 3: Cancel without executing (Ctrl-C)
	cmd = exec.Command("tmux", "send-keys", "-t", paneID, "C-c")
	cmd.Run() // Ignore error

	// Wait a moment for the shell to reset
	time.Sleep(20 * time.Millisecond)

	// Extract the command from the last non-empty line (which should have the prompt + command)
	lastCommand := extractLastCommand(cmdLineCapture)
	debug.Log("CaptureLastCommand: extracted command=%q", lastCommand)

	// Step 4: Get exit code based on shell type
	exitCode := c.captureExitCode(paneID, shell)
	debug.Log("CaptureLastCommand: exit code=%s", exitCode)

	// Extract the output (content between last two prompts)
	output := extractCommandOutput(fullCapture, lastCommand)

	return &CommandCapture{
		Command:  lastCommand,
		Output:   output,
		ExitCode: exitCode,
		Shell:    shell,
	}, nil
}

// captureExitCode retrieves the exit code of the last command using pipe-pane.
func (c *TmuxController) captureExitCode(paneID string, shell ShellType) string {
	// Create temp file for pipe-pane output
	tmpFile := fmt.Sprintf("/tmp/muxctl-exit-%d", os.Getpid())
	defer os.Remove(tmpFile)

	// Start pipe-pane to capture output to file
	pipeCmd := fmt.Sprintf("cat >> %s", tmpFile)
	cmd := exec.Command("tmux", "pipe-pane", "-t", paneID, "-o", pipeCmd)
	if err := cmd.Run(); err != nil {
		debug.Log("captureExitCode: failed to start pipe-pane: %v", err)
		return ""
	}

	// Determine the variable to echo based on shell
	var echoCmd string
	switch shell {
	case ShellFish:
		echoCmd = "echo $status"
	default: // bash, zsh, and others use $?
		echoCmd = "echo $?"
	}

	// Send the echo command
	cmd = exec.Command("tmux", "send-keys", "-t", paneID, echoCmd, "Enter")
	if err := cmd.Run(); err != nil {
		debug.Log("captureExitCode: failed to send echo: %v", err)
		// Stop pipe-pane before returning
		exec.Command("tmux", "pipe-pane", "-t", paneID).Run()
		return ""
	}

	// Wait for output to be captured
	time.Sleep(150 * time.Millisecond)

	// Stop pipe-pane (call with no command argument)
	exec.Command("tmux", "pipe-pane", "-t", paneID).Run()

	// Read captured output from temp file
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		debug.Log("captureExitCode: failed to read temp file: %v", err)
		return ""
	}

	debug.Log("captureExitCode: pipe-pane captured: %q", string(data))

	// Parse the exit code from captured output
	// Look for lines that are just numbers (the exit code output)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and the echo command itself
		if line == "" || strings.HasPrefix(line, "echo") {
			continue
		}
		if isNumeric(line) {
			return line
		}
	}

	return ""
}

// extractLastCommand extracts the command from captured pane content.
// It looks for the last line that appears to have a command (after prompt).
func extractLastCommand(capture string) string {
	lines := strings.Split(strings.TrimSpace(capture), "\n")
	if len(lines) == 0 {
		return ""
	}

	// Get the last non-empty line (should be prompt + command)
	var lastLine string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			lastLine = line
			break
		}
	}

	if lastLine == "" {
		return ""
	}

	// Try to strip common prompt patterns
	// Look for common prompt endings: $, >, %, #, ❯, →
	promptEndings := []string{" $ ", " > ", " % ", " # ", "❯ ", "→ ", "$ ", "> ", "% ", "# "}
	for _, ending := range promptEndings {
		if idx := strings.LastIndex(lastLine, ending); idx != -1 {
			return strings.TrimSpace(lastLine[idx+len(ending):])
		}
	}

	// If no prompt pattern found, return the whole line
	return lastLine
}

// extractCommandOutput extracts the output portion from the full capture.
// This is a best-effort extraction based on finding the command in the output.
func extractCommandOutput(fullCapture, command string) string {
	if command == "" {
		return fullCapture
	}

	lines := strings.Split(fullCapture, "\n")

	// Find the line containing the command
	cmdLineIdx := -1
	for i, line := range lines {
		if strings.Contains(line, command) {
			cmdLineIdx = i
		}
	}

	if cmdLineIdx == -1 || cmdLineIdx >= len(lines)-1 {
		return ""
	}

	// Find the next prompt line (indicates end of output)
	// Look for lines that look like prompts
	endIdx := len(lines)
	promptEndings := []string{" $ ", " > ", " % ", " # ", "❯ ", "→ "}
	for i := cmdLineIdx + 1; i < len(lines); i++ {
		line := lines[i]
		for _, ending := range promptEndings {
			if strings.Contains(line, ending) {
				endIdx = i
				break
			}
		}
		if endIdx != len(lines) {
			break
		}
	}

	// Extract lines between command and next prompt
	if cmdLineIdx+1 < endIdx {
		outputLines := lines[cmdLineIdx+1 : endIdx]
		return strings.TrimSpace(strings.Join(outputLines, "\n"))
	}

	return ""
}

// isNumeric checks if a string contains only digits.
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// FocusPane focuses on a specific pane.
func (c *TmuxController) FocusPane(role PaneRole) error {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	debug.Log("FocusPane: role=%s pane=%s", role, paneID)

	cmd := exec.Command("tmux", "select-pane", "-t", paneID)
	return cmd.Run()
}

// ClearPane clears the content of a pane by sending Ctrl-C and clear.
func (c *TmuxController) ClearPane(role PaneRole) error {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	debug.Log("ClearPane: role=%s pane=%s", role, paneID)

	// Send Ctrl-C to stop any running command
	cmd := exec.Command("tmux", "send-keys", "-t", paneID, "C-c")
	cmd.Run() // Ignore error

	// Send clear command
	cmd = exec.Command("tmux", "send-keys", "-t", paneID, "clear", "Enter")
	return cmd.Run()
}

// SwapPanes swaps the positions of two panes in the current window.
func (c *TmuxController) SwapPanes(role1, role2 PaneRole) error {
	pane1ID, ok1 := c.GetPaneID(role1)
	if !ok1 {
		return fmt.Errorf("pane '%s' not found or not initialized", role1)
	}

	pane2ID, ok2 := c.GetPaneID(role2)
	if !ok2 {
		return fmt.Errorf("pane '%s' not found or not initialized", role2)
	}

	debug.Log("SwapPanes: role1=%s pane1=%s role2=%s pane2=%s", role1, pane1ID, role2, pane2ID)

	// Swap the panes
	cmd := exec.Command("tmux", "swap-pane", "-s", pane1ID, "-t", pane2ID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to swap panes: %w", err)
	}

	// Update the stored pane IDs (they've swapped positions)
	c.setSessionVar(roleToVar(role1), pane2ID)
	c.setSessionVar(roleToVar(role2), pane1ID)

	return nil
}

// SwapPanesByTarget swaps two panes using their target identifiers.
// Targets can be in format: "window:pane" (e.g. "0:1", "mywindow:0")
// or "window.pane" (tmux native format)
// or pane IDs (e.g. "%1", "%2")
// This allows swapping panes across different windows.
func (c *TmuxController) SwapPanesByTarget(source, target string) error {
	if !c.Available() {
		return fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return fmt.Errorf("no session name set")
	}

	debug.Log("SwapPanesByTarget: source=%s target=%s session=%s", source, target, c.sessionName)

	// Save originals for error messages
	origSource := source
	origTarget := target

	// Qualify targets with session name if they're not pane IDs
	// Pane IDs start with %
	qualifiedSource := source
	qualifiedTarget := target

	if !strings.HasPrefix(source, "%") {
		// Convert "window:pane" to "session:window.pane" format
		// or "window.pane" to "session:window.pane"
		if strings.Contains(source, ":") {
			// Replace : with . for tmux format
			source = strings.Replace(source, ":", ".", 1)
		}
		qualifiedSource = fmt.Sprintf("%s:%s", c.sessionName, source)
	}

	if !strings.HasPrefix(target, "%") {
		if strings.Contains(target, ":") {
			target = strings.Replace(target, ":", ".", 1)
		}
		qualifiedTarget = fmt.Sprintf("%s:%s", c.sessionName, target)
	}

	debug.Log("SwapPanesByTarget: qualified source=%s target=%s", qualifiedSource, qualifiedTarget)

	// Swap the panes
	cmd := exec.Command("tmux", "swap-pane", "-s", qualifiedSource, "-t", qualifiedTarget)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to swap panes %s and %s: %w (output: %s)", origSource, origTarget, err, string(output))
	}

	return nil
}

// TogglePane toggles visibility of a pane by resizing it.
// For "bottom" role, toggles both left and right panes, giving full height to top.
// For "top" role, toggles top pane, giving full height to bottom panes.
func (c *TmuxController) TogglePane(role PaneRole) error {
	// Special case: "bottom" toggles both left and right
	if role == "bottom" {
		return c.toggleBottomPanes()
	}

	// Special case: "top" gives bottom panes full height
	if role == RoleTop {
		return c.toggleTopPane()
	}

	paneID, ok := c.GetPaneID(role)
	if !ok {
		return fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	// Session variable to track hidden state
	hiddenVar := fmt.Sprintf("@muxctl_%s_hidden", role)

	// Check if pane is currently hidden
	hidden, _ := c.getSessionVar(hiddenVar)
	isHidden := hidden == "1"

	debug.Log("TogglePane: role=%s pane=%s hidden=%v", role, paneID, isHidden)

	if isHidden {
		// Restore pane - resize to 50% of the bottom area
		cmd := exec.Command("tmux", "resize-pane", "-t", paneID, "-x", "50%")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restore pane: %w", err)
		}
		c.setSessionVar(hiddenVar, "0")
	} else {
		// Hide pane - resize to minimum width (2 cells)
		cmd := exec.Command("tmux", "resize-pane", "-t", paneID, "-x", "2")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to hide pane: %w", err)
		}
		c.setSessionVar(hiddenVar, "1")
	}

	// Focus left pane after toggling right
	if role == RoleRight {
		c.FocusPane(RoleLeft)
	}

	return nil
}

// toggleBottomPanes toggles both left and right panes together.
// When hidden, top pane gets 100% height. When shown, restores original layout.
func (c *TmuxController) toggleBottomPanes() error {
	topID, ok := c.GetPaneID(RoleTop)
	if !ok {
		return fmt.Errorf("top pane not found")
	}

	leftID, leftOK := c.GetPaneID(RoleLeft)
	rightID, rightOK := c.GetPaneID(RoleRight)

	// Check if bottom is currently hidden
	hidden, _ := c.getSessionVar("@muxctl_bottom_hidden")
	isHidden := hidden == "1"

	debug.Log("toggleBottomPanes: hidden=%v top=%s left=%s right=%s", isHidden, topID, leftID, rightID)

	if isHidden {
		// Restore bottom panes - resize top to 30%, bottom panes will auto-expand
		cmd := exec.Command("tmux", "resize-pane", "-t", topID, "-y", "30%")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restore layout: %w", err)
		}
		// Equalize bottom panes
		if leftOK && rightOK {
			exec.Command("tmux", "resize-pane", "-t", leftID, "-x", "50%").Run()
		}
		c.setSessionVar("@muxctl_bottom_hidden", "0")
	} else {
		// Hide bottom panes - resize top to 100%
		cmd := exec.Command("tmux", "resize-pane", "-t", topID, "-y", "100%")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to maximize top: %w", err)
		}
		c.setSessionVar("@muxctl_bottom_hidden", "1")
		// Focus top pane
		c.FocusPane(RoleTop)
	}

	return nil
}

// toggleTopPane toggles the top pane.
// When hidden, bottom panes get 100% height. When shown, restores original layout.
func (c *TmuxController) toggleTopPane() error {
	topID, ok := c.GetPaneID(RoleTop)
	if !ok {
		return fmt.Errorf("top pane not found")
	}

	leftID, leftOK := c.GetPaneID(RoleLeft)

	// Check if top is currently hidden
	hidden, _ := c.getSessionVar("@muxctl_top_hidden")
	isHidden := hidden == "1"

	debug.Log("toggleTopPane: hidden=%v top=%s", isHidden, topID)

	if isHidden {
		// Restore top pane - resize to 30%
		cmd := exec.Command("tmux", "resize-pane", "-t", topID, "-y", "30%")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restore layout: %w", err)
		}
		c.setSessionVar("@muxctl_top_hidden", "0")
	} else {
		// Hide top pane - resize bottom to 100% (by shrinking top to minimum)
		// First resize left pane to take full height
		if leftOK {
			cmd := exec.Command("tmux", "resize-pane", "-t", leftID, "-y", "100%")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to maximize bottom: %w", err)
			}
		}
		c.setSessionVar("@muxctl_top_hidden", "1")
		// Focus left pane
		c.FocusPane(RoleLeft)
	}

	return nil
}

// ListPanes lists all panes in a session.
func (c *TmuxController) ListPanes(session string) ([]PaneInfo, error) {
	cmd := exec.Command("tmux", "list-panes", "-t", session, "-F", "#{pane_id}:#{pane_index}:#{pane_title}:#{pane_active}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list panes: %w", err)
	}

	var panes []PaneInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 4 {
			continue
		}

		var index int
		fmt.Sscanf(parts[1], "%d", &index)

		panes = append(panes, PaneInfo{
			ID:     parts[0],
			Index:  index,
			Title:  parts[2],
			Active: parts[3] == "1",
		})
	}

	return panes, nil
}

// InsideTmux returns true if we're currently inside a tmux session.
func InsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// GetCurrentSession returns the current tmux session name if inside tmux.
func GetCurrentSession() string {
	if !InsideTmux() {
		return ""
	}
	cmd := exec.Command("tmux", "display-message", "-p", "#S")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// ValidRoles returns all valid pane role names.
func ValidRoles() []PaneRole {
	return []PaneRole{RoleTop, RoleLeft, RoleRight}
}

// ParseRole parses a string into a PaneRole.
func ParseRole(s string) (PaneRole, error) {
	switch strings.ToLower(s) {
	case "top":
		return RoleTop, nil
	case "left":
		return RoleLeft, nil
	case "right":
		return RoleRight, nil
	case "bottom":
		return "bottom", nil // pseudo-role for toggling both left and right
	default:
		return "", fmt.Errorf("invalid pane role: %s (valid: top, left, right, bottom)", s)
	}
}

// ResizePane resizes a pane to the specified width percentage.
// Only applies to horizontal resizing of left/right panes.
// widthPercent should be between 1 and 99.
func (c *TmuxController) ResizePane(role PaneRole, widthPercent int) error {
	if widthPercent < 1 || widthPercent > 99 {
		return fmt.Errorf("widthPercent must be between 1 and 99, got %d", widthPercent)
	}

	paneID, ok := c.GetPaneID(role)
	if !ok {
		return fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	debug.Log("ResizePane: role=%s pane=%s width=%d%%", role, paneID, widthPercent)

	// For left/right panes, resize width (-x)
	// For top pane, resize height (-y)
	var cmd *exec.Cmd
	if role == RoleTop {
		cmd = exec.Command("tmux", "resize-pane", "-t", paneID, "-y", fmt.Sprintf("%d%%", widthPercent))
	} else {
		cmd = exec.Command("tmux", "resize-pane", "-t", paneID, "-x", fmt.Sprintf("%d%%", widthPercent))
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to resize pane: %w", err)
	}

	// Store original size in session variable for potential restore
	sizeVar := fmt.Sprintf("@muxctl_%s_size", role)
	c.setSessionVar(sizeVar, fmt.Sprintf("%d", widthPercent))

	return nil
}

// GetPaneSize returns the current width and height of a pane in cells.
func (c *TmuxController) GetPaneSize(role PaneRole) (width, height int, err error) {
	paneID, ok := c.GetPaneID(role)
	if !ok {
		return 0, 0, fmt.Errorf("pane '%s' not found or not initialized", role)
	}

	// Query pane dimensions using tmux display-message
	cmd := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{pane_width} #{pane_height}")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get pane size: %w", err)
	}

	// Parse output: "width height"
	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected output format: %s", string(output))
	}

	var w, h int
	if _, err := fmt.Sscanf(parts[0], "%d", &w); err != nil {
		return 0, 0, fmt.Errorf("failed to parse width: %w", err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &h); err != nil {
		return 0, 0, fmt.Errorf("failed to parse height: %w", err)
	}

	return w, h, nil
}

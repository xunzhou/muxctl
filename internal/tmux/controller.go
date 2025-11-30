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

	// Set pane titles in parallel (errors ignored for titles)
	type titleSet struct {
		paneID string
		title  string
	}
	titles := []titleSet{
		{topID, "[top]"},
		{leftID, "[left]"},
		{rightID, "[right]"},
	}

	var titleWg sync.WaitGroup
	for _, t := range titles {
		titleWg.Add(1)
		go func(paneID, title string) {
			defer titleWg.Done()
			c.setPaneTitle(paneID, title)
		}(t.paneID, t.title)
	}
	titleWg.Wait()

	return nil
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

	// Build environment prefix
	var envPrefix string
	for k, v := range env {
		envPrefix += fmt.Sprintf("%s=%q ", k, v)
	}

	cmdStr := envPrefix + strings.Join(cmdArgs, " ")

	debug.Log("RunInPane: role=%s pane=%s cmd=%s", role, paneID, cmdStr)

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
	default:
		return "", fmt.Errorf("invalid pane role: %s (valid: top, left, right)", s)
	}
}

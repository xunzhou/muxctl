package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/xunzhou/muxctl/internal/debug"
)

// WindowInfo contains information about a tmux window.
type WindowInfo struct {
	Index  int    // Window index
	Name   string // Window name
	Active bool   // Currently selected
	Panes  int    // Number of panes in window
}

// CreateWindow creates a new window in the session with the given name.
// Returns the window index.
func (c *TmuxController) CreateWindow(name string) (int, error) {
	if !c.Available() {
		return 0, fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return 0, fmt.Errorf("no session name set")
	}

	debug.Log("Creating window: %s", name)

	// Create window with -P to print window index
	cmd := exec.Command("tmux", "new-window",
		"-t", c.sessionName+":",
		"-n", name,
		"-P", "-F", "#{window_index}")

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to create window %s: %w", name, err)
	}

	indexStr := strings.TrimSpace(string(output))
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse window index: %w", err)
	}

	// Disable automatic renaming to preserve window name
	windowTarget := fmt.Sprintf("%s:%d", c.sessionName, index)
	exec.Command("tmux", "set-window-option", "-t", windowTarget, "automatic-rename", "off").Run()
	exec.Command("tmux", "set-window-option", "-t", windowTarget, "allow-rename", "off").Run()

	debug.Log("Created window %s with index %d", name, index)
	return index, nil
}

// WindowExists checks if a window with the given name exists in the session.
func (c *TmuxController) WindowExists(name string) bool {
	if !c.Available() || c.sessionName == "" {
		return false
	}

	cmd := exec.Command("tmux", "list-windows",
		"-t", c.sessionName,
		"-F", "#{window_name}")

	output, err := cmd.Output()
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}

	return false
}

// GetWindowIndex returns the index of a window by name.
func (c *TmuxController) GetWindowIndex(name string) (int, error) {
	if !c.Available() {
		return 0, fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return 0, fmt.Errorf("no session name set")
	}

	cmd := exec.Command("tmux", "list-windows",
		"-t", c.sessionName,
		"-F", "#{window_index}:#{window_name}")

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to list windows: %w", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 && parts[1] == name {
			index, err := strconv.Atoi(parts[0])
			if err != nil {
				return 0, fmt.Errorf("failed to parse window index: %w", err)
			}
			return index, nil
		}
	}

	return 0, fmt.Errorf("window not found: %s", name)
}

// SwitchToWindow switches to a window by name.
func (c *TmuxController) SwitchToWindow(name string) error {
	if !c.Available() {
		return fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return fmt.Errorf("no session name set")
	}

	debug.Log("Switching to window: %s", name)

	windowTarget := fmt.Sprintf("%s:%s", c.sessionName, name)
	cmd := exec.Command("tmux", "select-window", "-t", windowTarget)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch to window %s: %w", name, err)
	}

	return nil
}

// SwitchToWindowIndex switches to a window by index.
func (c *TmuxController) SwitchToWindowIndex(index int) error {
	if !c.Available() {
		return fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return fmt.Errorf("no session name set")
	}

	debug.Log("Switching to window index: %d", index)

	windowTarget := fmt.Sprintf("%s:%d", c.sessionName, index)
	cmd := exec.Command("tmux", "select-window", "-t", windowTarget)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch to window %d: %w", index, err)
	}

	return nil
}

// CloseWindow closes a window by name.
func (c *TmuxController) CloseWindow(name string) error {
	if !c.Available() {
		return fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return fmt.Errorf("no session name set")
	}

	debug.Log("Closing window: %s", name)

	// Get window index first (to use numeric target for reliability)
	index, err := c.GetWindowIndex(name)
	if err != nil {
		return err
	}

	windowTarget := fmt.Sprintf("%s:%d", c.sessionName, index)
	cmd := exec.Command("tmux", "kill-window", "-t", windowTarget)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close window %s: %w", name, err)
	}

	return nil
}

// ListWindows lists all windows in the session.
func (c *TmuxController) ListWindows() ([]WindowInfo, error) {
	if !c.Available() {
		return nil, fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return nil, fmt.Errorf("no session name set")
	}

	cmd := exec.Command("tmux", "list-windows",
		"-t", c.sessionName,
		"-F", "#{window_index}:#{window_name}:#{window_active}:#{window_panes}")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list windows: %w", err)
	}

	var windows []WindowInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 4 {
			continue
		}

		var info WindowInfo
		info.Index, _ = strconv.Atoi(parts[0])
		info.Name = parts[1]
		info.Active = parts[2] == "1"
		info.Panes, _ = strconv.Atoi(parts[3])

		windows = append(windows, info)
	}

	return windows, nil
}

// RunInWindow runs a command in a specific window.
// The command is executed in the first pane of the window.
func (c *TmuxController) RunInWindow(windowName string, cmd []string, env map[string]string) error {
	if !c.Available() {
		return fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return fmt.Errorf("no session name set")
	}

	debug.Log("Running command in window %s: %v", windowName, cmd)

	// Get window index
	index, err := c.GetWindowIndex(windowName)
	if err != nil {
		return err
	}

	// Target the first pane in the window
	paneTarget := fmt.Sprintf("%s:%d.0", c.sessionName, index)

	// Build command string with environment
	cmdStr := strings.Join(cmd, " ")
	if len(env) > 0 {
		var envPrefix string
		for k, v := range env {
			envPrefix += fmt.Sprintf("%s=%q ", k, v)
		}
		cmdStr = envPrefix + cmdStr
	}

	// Send command to pane
	sendCmd := exec.Command("tmux", "send-keys", "-t", paneTarget, cmdStr, "Enter")
	if err := sendCmd.Run(); err != nil {
		return fmt.Errorf("failed to run command in window %s: %w", windowName, err)
	}

	return nil
}

// SetWindowMetadata stores metadata for a window in a session variable.
// This is useful for tracking window state like last access time.
func (c *TmuxController) SetWindowMetadata(windowName, key, value string) error {
	if !c.Available() {
		return fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return fmt.Errorf("no session name set")
	}

	// Use session variables to store window metadata
	varName := fmt.Sprintf("@muxctl_window_%s_%s", windowName, key)

	cmd := exec.Command("tmux", "set-option", "-t", c.sessionName, varName, value)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set window metadata %s:%s: %w", windowName, key, err)
	}

	debug.Log("Set window metadata: %s:%s=%s", windowName, key, value)
	return nil
}

// GetWindowMetadata retrieves metadata for a window from a session variable.
func (c *TmuxController) GetWindowMetadata(windowName, key string) (string, error) {
	if !c.Available() {
		return "", fmt.Errorf("tmux not available")
	}
	if c.sessionName == "" {
		return "", fmt.Errorf("no session name set")
	}

	varName := fmt.Sprintf("@muxctl_window_%s_%s", windowName, key)

	cmd := exec.Command("tmux", "show-options", "-v", "-t", c.sessionName, varName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get window metadata %s:%s: %w", windowName, key, err)
	}

	return strings.TrimSpace(string(output)), nil
}

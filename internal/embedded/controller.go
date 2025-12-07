package embedded

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/xunzhou/muxctl/internal/debug"
)

// TmuxController provides programmatic control of an embedded tmux server.
// It wraps tmux CLI commands executed against a specific socket.
type TmuxController struct {
	socketPath  string
	sessionName string
}

// NewTmuxController creates a controller for the given socket path.
func NewTmuxController(socketPath string) *TmuxController {
	return &TmuxController{
		socketPath: socketPath,
	}
}

// SetSession sets the session name for subsequent commands.
func (c *TmuxController) SetSession(name string) {
	c.sessionName = name
}

// exec runs a tmux command against the socket.
func (c *TmuxController) exec(args ...string) error {
	cmdArgs := append([]string{"-S", c.socketPath}, args...)
	cmd := exec.Command("tmux", cmdArgs...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		debug.Log("TmuxController.exec: command failed: tmux %v -> %v (output: %s)", args, err, string(output))
		return fmt.Errorf("tmux command failed: %w (output: %s)", err, string(output))
	}

	return nil
}

// execOutput runs a tmux command and returns its output.
func (c *TmuxController) execOutput(args ...string) (string, error) {
	cmdArgs := append([]string{"-S", c.socketPath}, args...)
	cmd := exec.Command("tmux", cmdArgs...)

	output, err := cmd.Output()
	if err != nil {
		debug.Log("TmuxController.execOutput: command failed: tmux %v -> %v", args, err)
		return "", fmt.Errorf("tmux command failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// NewWindow creates a new window and returns its WindowID.
func (c *TmuxController) NewWindow(name string, cmd []string) (WindowID, error) {
	debug.Log("TmuxController.NewWindow: name=%s cmd=%v", name, cmd)

	args := []string{"new-window", "-t", c.sessionName, "-n", name, "-P", "-F", "#{window_id}"}
	if len(cmd) > 0 {
		args = append(args, strings.Join(cmd, " "))
	}

	output, err := c.execOutput(args...)
	if err != nil {
		return WindowID{}, fmt.Errorf("failed to create window: %w", err)
	}

	return NewWindowID(output), nil
}

// SelectWindow switches to the specified window.
func (c *TmuxController) SelectWindow(win WindowID) error {
	debug.Log("TmuxController.SelectWindow: window=%s", win.TmuxID)
	return c.exec("select-window", "-t", win.TmuxID)
}

// KillWindow closes the specified window.
func (c *TmuxController) KillWindow(win WindowID) error {
	debug.Log("TmuxController.KillWindow: window=%s", win.TmuxID)
	return c.exec("kill-window", "-t", win.TmuxID)
}

// SplitPane splits the target pane/window in the specified direction.
func (c *TmuxController) SplitPane(target WindowID, cmd []string, dir Direction) (PaneID, error) {
	debug.Log("TmuxController.SplitPane: target=%s dir=%s cmd=%v", target.TmuxID, dir, cmd)

	args := []string{"split-window", "-t", target.TmuxID, "-P", "-F", "#{pane_id}"}

	// Add direction flag
	switch dir {
	case DirectionHorizontal:
		args = append(args, "-h")
	case DirectionVertical:
		args = append(args, "-v")
	}

	// Add command if provided
	if len(cmd) > 0 {
		args = append(args, strings.Join(cmd, " "))
	}

	output, err := c.execOutput(args...)
	if err != nil {
		return PaneID{}, fmt.Errorf("failed to split pane: %w", err)
	}

	return NewPaneID(output), nil
}

// SelectPane switches focus to the specified pane.
func (c *TmuxController) SelectPane(pane PaneID) error {
	debug.Log("TmuxController.SelectPane: pane=%s", pane.TmuxID)
	return c.exec("select-pane", "-t", pane.TmuxID)
}

// CapturePane captures content from the specified pane.
func (c *TmuxController) CapturePane(pane PaneID, opts CaptureOptions) (string, error) {
	debug.Log("TmuxController.CapturePane: pane=%s lines=%d start=%d end=%d", pane.TmuxID, opts.Lines, opts.StartLine, opts.EndLine)

	args := []string{"capture-pane", "-t", pane.TmuxID, "-p"}

	// Strip ANSI escape codes if requested
	if opts.StripEscapes {
		args = append(args, "-e")
	}

	if opts.Lines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", opts.Lines))
	} else if opts.StartLine != 0 {
		args = append(args, "-S", fmt.Sprintf("%d", opts.StartLine))
	}

	if opts.EndLine != 0 {
		args = append(args, "-E", fmt.Sprintf("%d", opts.EndLine))
	}

	output, err := c.execOutput(args...)
	if err != nil {
		return "", fmt.Errorf("failed to capture pane: %w", err)
	}

	return output, nil
}

// ClearHistory clears the scrollback history for the target pane.
func (c *TmuxController) ClearHistory(target PaneID) error {
	debug.Log("TmuxController.ClearHistory: pane=%s", target.TmuxID)
	return c.exec("clear-history", "-t", target.TmuxID)
}

// SendKeys sends keystrokes to the target pane.
func (c *TmuxController) SendKeys(target PaneID, keys string) error {
	debug.Log("TmuxController.SendKeys: pane=%s keys=%q", target.TmuxID, keys)
	return c.exec("send-keys", "-t", target.TmuxID, keys)
}

// ListPanes lists all panes in the current session.
func (c *TmuxController) ListPanes() ([]PaneID, error) {
	output, err := c.execOutput("list-panes", "-t", c.sessionName, "-F", "#{pane_id}")
	if err != nil {
		return nil, fmt.Errorf("failed to list panes: %w", err)
	}

	lines := strings.Split(output, "\n")
	panes := make([]PaneID, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			panes = append(panes, NewPaneID(line))
		}
	}

	return panes, nil
}

// ListWindows lists all windows in the current session.
func (c *TmuxController) ListWindows() ([]WindowID, error) {
	output, err := c.execOutput("list-windows", "-t", c.sessionName, "-F", "#{window_id}")
	if err != nil {
		return nil, fmt.Errorf("failed to list windows: %w", err)
	}

	lines := strings.Split(output, "\n")
	windows := make([]WindowID, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			windows = append(windows, NewWindowID(line))
		}
	}

	return windows, nil
}

// GetActivePane returns the currently active pane ID.
func (c *TmuxController) GetActivePane() (PaneID, error) {
	output, err := c.execOutput("display-message", "-p", "#{pane_id}")
	if err != nil {
		return PaneID{}, fmt.Errorf("failed to get active pane: %w", err)
	}

	return NewPaneID(output), nil
}

// GetActiveWindow returns the currently active window ID.
func (c *TmuxController) GetActiveWindow() (WindowID, error) {
	output, err := c.execOutput("display-message", "-p", "#{window_id}")
	if err != nil {
		return WindowID{}, fmt.Errorf("failed to get active window: %w", err)
	}

	return NewWindowID(output), nil
}

// SetOption sets a tmux option for the session.
func (c *TmuxController) SetOption(option, value string) error {
	// Use -g for global server options like 'status'
	return c.exec("set-option", "-g", option, value)
}

// SetWindowOption sets a window-specific option.
func (c *TmuxController) SetWindowOption(option, value string) error {
	return c.exec("set-window-option", "-t", c.sessionName, option, value)
}

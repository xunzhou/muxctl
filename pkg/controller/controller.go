package controller

import (
	"github.com/xunzhou/muxctl/internal/tmux"
)

// Controller provides an interface for pane/session/window management.
type Controller interface {
	// Session management
	Available() bool
	SessionExists(name string) bool
	EnsureSession(name string) error
	Attach(session string) error
	Init(session string, layout LayoutDef) error
	GetSessionName() string

	// Pane management
	RunInPane(role PaneRole, cmd []string, env map[string]string) error
	SendKeys(role PaneRole, keys string) error
	CapturePane(role PaneRole, lines int) (string, error)
	CaptureLastCommand(role PaneRole) (*CommandCapture, error)
	FocusPane(role PaneRole) error
	ClearPane(role PaneRole) error
	SwapPanes(role1, role2 PaneRole) error
	SwapPanesByTarget(source, target string) error
	ListPanes(session string) ([]PaneInfo, error)
	GetPaneID(role PaneRole) (string, bool)
	DetectShell(role PaneRole) ShellType
	ResizePane(role PaneRole, widthPercent int) error
	GetPaneSize(role PaneRole) (width, height int, err error)

	// Window management
	CreateWindow(name string) (int, error)
	WindowExists(name string) bool
	GetWindowIndex(name string) (int, error)
	SwitchToWindow(name string) error
	SwitchToWindowIndex(index int) error
	CloseWindow(name string) error
	ListWindows() ([]WindowInfo, error)
	RunInWindow(windowName string, cmd []string, env map[string]string) error
	SetWindowMetadata(windowName, key, value string) error
	GetWindowMetadata(windowName, key string) (string, error)
}

// TmuxController wraps internal/tmux.TmuxController for public use.
type TmuxController struct {
	impl *tmux.TmuxController
}

// New creates a new TmuxController.
func New() *TmuxController {
	return &TmuxController{
		impl: tmux.NewController(),
	}
}

// Available checks if tmux is installed and accessible.
func (c *TmuxController) Available() bool {
	return c.impl.Available()
}

// SessionExists checks if a tmux session exists.
func (c *TmuxController) SessionExists(name string) bool {
	return c.impl.SessionExists(name)
}

// EnsureSession creates a session if it doesn't exist.
func (c *TmuxController) EnsureSession(name string) error {
	return c.impl.EnsureSession(name)
}

// Attach attaches to an existing session.
func (c *TmuxController) Attach(session string) error {
	return c.impl.Attach(session)
}

// Init initializes the 3-pane layout.
func (c *TmuxController) Init(session string, layout LayoutDef) error {
	return c.impl.Init(session, tmux.LayoutDef{
		TopPercent:  layout.TopPercent,
		SidePercent: layout.SidePercent,
	})
}

// RunInPane runs a command in the specified pane.
func (c *TmuxController) RunInPane(role PaneRole, cmd []string, env map[string]string) error {
	return c.impl.RunInPane(tmux.PaneRole(role), cmd, env)
}

// SendKeys sends raw keystrokes to a pane.
func (c *TmuxController) SendKeys(role PaneRole, keys string) error {
	return c.impl.SendKeys(tmux.PaneRole(role), keys)
}

// CapturePane captures the content of a pane.
func (c *TmuxController) CapturePane(role PaneRole, lines int) (string, error) {
	return c.impl.CapturePane(tmux.PaneRole(role), lines)
}

// CaptureLastCommand captures the last executed command, its output, and exit code.
func (c *TmuxController) CaptureLastCommand(role PaneRole) (*CommandCapture, error) {
	result, err := c.impl.CaptureLastCommand(tmux.PaneRole(role))
	if err != nil {
		return nil, err
	}
	return &CommandCapture{
		Command:  result.Command,
		Output:   result.Output,
		ExitCode: result.ExitCode,
		Shell:    ShellType(result.Shell),
	}, nil
}

// FocusPane focuses on a specific pane.
func (c *TmuxController) FocusPane(role PaneRole) error {
	return c.impl.FocusPane(tmux.PaneRole(role))
}

// ClearPane clears the content of a pane.
func (c *TmuxController) ClearPane(role PaneRole) error {
	return c.impl.ClearPane(tmux.PaneRole(role))
}

// SwapPanes swaps the positions of two panes in the current window.
func (c *TmuxController) SwapPanes(role1, role2 PaneRole) error {
	return c.impl.SwapPanes(tmux.PaneRole(role1), tmux.PaneRole(role2))
}

// SwapPanesByTarget swaps two panes using their target identifiers.
// This allows swapping panes across different windows.
func (c *TmuxController) SwapPanesByTarget(source, target string) error {
	return c.impl.SwapPanesByTarget(source, target)
}

// ListPanes lists all panes in a session.
func (c *TmuxController) ListPanes(session string) ([]PaneInfo, error) {
	panes, err := c.impl.ListPanes(session)
	if err != nil {
		return nil, err
	}

	result := make([]PaneInfo, len(panes))
	for i, p := range panes {
		result[i] = PaneInfo{
			ID:     p.ID,
			Index:  p.Index,
			Title:  p.Title,
			Active: p.Active,
		}
	}
	return result, nil
}

// GetPaneID returns the pane ID for a given role.
func (c *TmuxController) GetPaneID(role PaneRole) (string, bool) {
	return c.impl.GetPaneID(tmux.PaneRole(role))
}

// GetSessionName returns the current session name.
func (c *TmuxController) GetSessionName() string {
	return c.impl.GetSessionName()
}

// DetectShell detects the shell type running in a pane.
func (c *TmuxController) DetectShell(role PaneRole) ShellType {
	return ShellType(c.impl.DetectShell(tmux.PaneRole(role)))
}

// InsideTmux returns true if we're currently inside a tmux session.
func InsideTmux() bool {
	return tmux.InsideTmux()
}

// GetCurrentSession returns the current tmux session name if inside tmux.
func GetCurrentSession() string {
	return tmux.GetCurrentSession()
}

// CreateWindow creates a new window in the session with the given name.
func (c *TmuxController) CreateWindow(name string) (int, error) {
	return c.impl.CreateWindow(name)
}

// WindowExists checks if a window with the given name exists.
func (c *TmuxController) WindowExists(name string) bool {
	return c.impl.WindowExists(name)
}

// GetWindowIndex returns the index of a window by name.
func (c *TmuxController) GetWindowIndex(name string) (int, error) {
	return c.impl.GetWindowIndex(name)
}

// SwitchToWindow switches to a window by name.
func (c *TmuxController) SwitchToWindow(name string) error {
	return c.impl.SwitchToWindow(name)
}

// SwitchToWindowIndex switches to a window by index.
func (c *TmuxController) SwitchToWindowIndex(index int) error {
	return c.impl.SwitchToWindowIndex(index)
}

// CloseWindow closes a window by name.
func (c *TmuxController) CloseWindow(name string) error {
	return c.impl.CloseWindow(name)
}

// ListWindows lists all windows in the session.
func (c *TmuxController) ListWindows() ([]WindowInfo, error) {
	windows, err := c.impl.ListWindows()
	if err != nil {
		return nil, err
	}

	result := make([]WindowInfo, len(windows))
	for i, w := range windows {
		result[i] = WindowInfo{
			Index:  w.Index,
			Name:   w.Name,
			Active: w.Active,
			Panes:  w.Panes,
		}
	}
	return result, nil
}

// RunInWindow runs a command in a specific window.
func (c *TmuxController) RunInWindow(windowName string, cmd []string, env map[string]string) error {
	return c.impl.RunInWindow(windowName, cmd, env)
}

// SetWindowMetadata stores metadata for a window in a session variable.
func (c *TmuxController) SetWindowMetadata(windowName, key, value string) error {
	return c.impl.SetWindowMetadata(windowName, key, value)
}

// GetWindowMetadata retrieves metadata for a window from a session variable.
func (c *TmuxController) GetWindowMetadata(windowName, key string) (string, error) {
	return c.impl.GetWindowMetadata(windowName, key)
}

// ResizePane resizes a pane to the specified width percentage.
func (c *TmuxController) ResizePane(role PaneRole, widthPercent int) error {
	return c.impl.ResizePane(tmux.PaneRole(role), widthPercent)
}

// GetPaneSize returns the current width and height of a pane in cells.
func (c *TmuxController) GetPaneSize(role PaneRole) (width, height int, err error) {
	return c.impl.GetPaneSize(tmux.PaneRole(role))
}

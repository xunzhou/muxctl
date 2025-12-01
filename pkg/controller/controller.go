package controller

import (
	"github.com/xunzhou/muxctl/internal/tmux"
)

// Controller provides an interface for pane/session management.
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

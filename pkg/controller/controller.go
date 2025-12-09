package controller

import (
	"github.com/xunzhou/muxctl/pkg/tmux"
)

// Role represents a pane role
type Role string

const (
	RoleLeft   Role = "left"
	RoleCenter Role = "center"
	RoleRight  Role = "right"
)

// Controller provides an interface to tmux operations
type Controller interface {
	// Available checks if tmux is available
	Available() bool

	// SessionExists checks if a session exists
	SessionExists(name string) bool

	// EnsureSession creates a session if it doesn't exist
	EnsureSession(name string) error

	// Init initializes a session with the given layout
	Init(sessionName string, layout Layout) error

	// GetManager returns the underlying tmux.Manager
	GetManager() *tmux.Manager

	// CreateWindow creates a new tmux window and returns its ID
	CreateWindow(name string) (string, error)

	// RunInWindow runs a command in a window
	RunInWindow(window string, cmd []string, opts map[string]string) error

	// SwapPanesByTarget swaps two panes by their targets
	SwapPanesByTarget(src, dst string) error

	// CloseWindow closes a window
	CloseWindow(window string) error

	// FocusPane focuses a pane by role
	FocusPane(role Role) error
}

// Layout represents a tmux layout configuration
type Layout struct {
	Type string // "two-pane", "three-pane", etc.
}

// DefaultLayout returns the default 2-pane layout
func DefaultLayout() Layout {
	return Layout{
		Type: "two-pane",
	}
}

// controller implements the Controller interface
type controller struct {
	manager *tmux.Manager
}

// New creates a new Controller
func New() Controller {
	// Try to create a manager if we're already in a tmux session
	mgr, _ := tmux.NewManager()
	return &controller{
		manager: mgr,
	}
}

// Available checks if tmux is available
func (c *controller) Available() bool {
	// Check if tmux is available by trying to run a simple command
	_, err := tmuxCmd("display-message", "-p", "#{version}")
	return err == nil
}

// SessionExists checks if a session exists
func (c *controller) SessionExists(name string) bool {
	_, err := tmuxCmd("has-session", "-t", name)
	return err == nil
}

// EnsureSession creates a session if it doesn't exist
func (c *controller) EnsureSession(name string) error {
	if c.SessionExists(name) {
		return nil
	}

	// Create new session detached
	_, err := tmuxCmd("new-session", "-d", "-s", name)
	return err
}

// Init initializes a session with the given layout
func (c *controller) Init(sessionName string, layout Layout) error {
	// If we don't have a manager yet, create one
	if c.manager == nil {
		mgr, err := tmux.NewManager()
		if err != nil {
			return err
		}
		c.manager = mgr
	}

	// Setup the layout
	if err := c.manager.Setup(); err != nil {
		return err
	}

	return nil
}

// GetManager returns the underlying tmux.Manager
func (c *controller) GetManager() *tmux.Manager {
	return c.manager
}

// CreateWindow creates a new tmux window and returns its ID
func (c *controller) CreateWindow(name string) (string, error) {
	// Create window detached and get its ID
	windowID, err := tmuxCmd("new-window", "-d", "-n", name, "-P", "-F", "#{window_id}")
	if err != nil {
		return "", err
	}
	return windowID, nil
}

// RunInWindow runs a command in a window
func (c *controller) RunInWindow(window string, cmd []string, opts map[string]string) error {
	// Kill existing panes and respawn with command
	args := []string{"respawn-pane", "-t", window, "-k"}
	args = append(args, cmd...)

	_, err := tmuxCmd(args...)
	return err
}

// SwapPanesByTarget swaps two panes by their targets
func (c *controller) SwapPanesByTarget(src, dst string) error {
	_, err := tmuxCmd("swap-pane", "-s", src, "-t", dst)
	return err
}

// CloseWindow closes a window
func (c *controller) CloseWindow(window string) error {
	_, err := tmuxCmd("kill-window", "-t", window)
	return err
}

// FocusPane focuses a pane by role
func (c *controller) FocusPane(role Role) error {
	// For now, this is a no-op
	// In a full implementation, this would map role to specific pane IDs
	return nil
}

// tmuxCmd is a helper to run tmux commands
func tmuxCmd(args ...string) (string, error) {
	return tmux.TmuxCmd(args...)
}

// Package controller provides a public API for tmux pane management.
// This package wraps internal/tmux for use by external modules.
package controller

// PaneRole identifies the logical role of a pane.
type PaneRole string

const (
	RoleTop   PaneRole = "top"   // Top pane
	RoleLeft  PaneRole = "left"  // Bottom-left pane
	RoleRight PaneRole = "right" // Bottom-right pane
)

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

// ValidRoles returns all valid pane role names.
func ValidRoles() []PaneRole {
	return []PaneRole{RoleTop, RoleLeft, RoleRight}
}

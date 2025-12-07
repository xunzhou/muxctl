package embedded

import "fmt"

// WindowID wraps tmux's persistent window identifier (e.g., "@12").
// These IDs persist even when windows are renumbered.
type WindowID struct {
	TmuxID string // "@7", "@12", etc.
}

// NewWindowID creates a WindowID from a raw tmux identifier.
func NewWindowID(raw string) WindowID {
	return WindowID{TmuxID: raw}
}

// String returns the tmux identifier for logging/debugging.
func (id WindowID) String() string {
	return id.TmuxID
}

// PaneID wraps tmux's persistent pane identifier (e.g., "%7").
// These IDs persist even when panes are rearranged.
type PaneID struct {
	TmuxID string // "%3", "%17", etc.
}

// NewPaneID creates a PaneID from a raw tmux identifier.
func NewPaneID(raw string) PaneID {
	return PaneID{TmuxID: raw}
}

// String returns the tmux identifier for logging/debugging.
func (id PaneID) String() string {
	return id.TmuxID
}

// CaptureOptions defines options for capturing pane content.
type CaptureOptions struct {
	Lines        int  // Number of lines (e.g., last 500)
	VisibleOnly  bool // Only visible portion
	StartLine    int  // Start offset for partial capture (-N for last N lines)
	EndLine      int  // End offset (-1 for last line)
	StripEscapes bool // Strip ANSI escape codes
}

// Direction specifies split direction for panes.
type Direction int

const (
	DirectionHorizontal Direction = iota // Split left/right
	DirectionVertical                    // Split top/bottom
)

func (d Direction) String() string {
	switch d {
	case DirectionHorizontal:
		return "horizontal"
	case DirectionVertical:
		return "vertical"
	default:
		return fmt.Sprintf("unknown(%d)", d)
	}
}

// SessionOpts defines options for creating tmux sessions.
type SessionOpts struct {
	Detached bool   // Create detached session
	Width    int    // Initial width
	Height   int    // Initial height
	Shell    string // Shell to use (default: $SHELL)
}

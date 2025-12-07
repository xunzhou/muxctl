// Package embedded provides public exports of the embedded PTY architecture.
// This package re-exports types from internal/embedded for use by external packages.
package embedded

import (
	"github.com/xunzhou/muxctl/internal/debug"
	"github.com/xunzhou/muxctl/internal/embedded"
)

// Session represents an embedded tmux session with PTY.
type Session = embedded.Session

// ContextShellPool manages persistent tmux windows per Kubernetes context.
type ContextShellPool = embedded.ContextShellPool

// TerminalViewport is a Bubble Tea component for rendering PTY output.
type TerminalViewport = embedded.TerminalViewport

// TmuxController provides programmatic control over an embedded tmux server.
type TmuxController = embedded.TmuxController

// WindowID is a type-safe wrapper for tmux persistent window IDs.
type WindowID = embedded.WindowID

// PaneID is a type-safe wrapper for tmux persistent pane IDs.
type PaneID = embedded.PaneID

// CaptureOptions configures terminal pane content capture.
type CaptureOptions = embedded.CaptureOptions

// PtyOutputMsg is sent when PTY output is available.
type PtyOutputMsg = embedded.PtyOutputMsg

// NewEmbeddedSession creates a new embedded tmux session with PTY.
func NewEmbeddedSession(name string, cols, rows int) (*Session, error) {
	return embedded.NewEmbeddedSession(name, cols, rows)
}

// NewContextShellPool creates a pool for managing context shells.
func NewContextShellPool(ctrl *TmuxController, session string) *ContextShellPool {
	return embedded.NewContextShellPool(ctrl, session)
}

// EnableDebugLogging enables debug logging to /tmp/muxctl-debug.log.
func EnableDebugLogging() error {
	return debug.Enable()
}

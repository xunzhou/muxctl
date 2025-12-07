package embedded

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xunzhou/muxctl/internal/debug"
	"github.com/xunzhou/muxctl/internal/pty"
)

// Session represents an embedded tmux session with PTY.
// It combines PTY, TmuxController, and provides the foundation for TerminalViewport.
type Session struct {
	Name       string
	PTY        *pty.PTY
	Controller *TmuxController
	SocketPath string
}

// NewEmbeddedSession creates a new embedded tmux session with PTY.
// This initializes:
//   - PTY pair (master/slave)
//   - tmux server attached to PTY slave
//   - TmuxController for programmatic control
func NewEmbeddedSession(name string, cols, rows int) (*Session, error) {
	debug.Log("NewEmbeddedSession: name=%s cols=%d rows=%d", name, cols, rows)

	// Generate socket path
	socketPath, err := generateSocketPath(name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate socket path: %w", err)
	}

	debug.Log("NewEmbeddedSession: socket=%s", socketPath)

	// Create PTY
	ptyInstance, err := pty.New(rows, cols)
	if err != nil {
		return nil, fmt.Errorf("failed to create PTY: %w", err)
	}

	// Spawn tmux server attached to PTY
	if err := ptyInstance.SpawnTmux(socketPath, name); err != nil {
		ptyInstance.Close()
		return nil, fmt.Errorf("failed to spawn tmux: %w", err)
	}

	// Create controller for the socket
	controller := NewTmuxController(socketPath)
	controller.SetSession(name)

	// Wait for tmux server to be ready (retry up to 10 times with 50ms delay)
	var configErr error
	for i := 0; i < 10; i++ {
		if i > 0 {
			time.Sleep(50 * time.Millisecond)
		}

		// Try to configure tmux
		configErr = configureEmbeddedTmux(controller)
		if configErr == nil {
			debug.Log("NewEmbeddedSession: tmux configured successfully on attempt %d", i+1)
			break
		}

		debug.Log("NewEmbeddedSession: config attempt %d failed: %v", i+1, configErr)
	}

	if configErr != nil {
		debug.Log("NewEmbeddedSession: failed to configure tmux after retries (non-fatal): %v", configErr)
		// Non-fatal: continue even if configuration fails
	}

	return &Session{
		Name:       name,
		PTY:        ptyInstance,
		Controller: controller,
		SocketPath: socketPath,
	}, nil
}

// configureEmbeddedTmux applies the embedded mode settings from the spec.
func configureEmbeddedTmux(ctrl *TmuxController) error {
	debug.Log("configureEmbeddedTmux: applying settings")

	settings := map[string]string{
		"status":            "off",
		"mouse":             "on",
		"set-titles":        "off",
		"assume-paste-time": "0",
		"focus-events":      "on",
	}

	for option, value := range settings {
		if err := ctrl.SetOption(option, value); err != nil {
			return fmt.Errorf("failed to set %s=%s: %w", option, value, err)
		}
	}

	// Window options
	windowSettings := map[string]string{
		"history-limit":     "300",
		"monitor-activity":  "off",
	}

	for option, value := range windowSettings {
		if err := ctrl.SetWindowOption(option, value); err != nil {
			return fmt.Errorf("failed to set window option %s=%s: %w", option, value, err)
		}
	}

	return nil
}

// generateSocketPath creates a socket path following the spec's naming convention.
// Uses $XDG_RUNTIME_DIR/muxctl-{PID}-{RANDOM}.sock
func generateSocketPath(sessionName string) (string, error) {
	// Get runtime directory
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		// Fallback to /tmp
		runtimeDir = "/tmp"
	}

	// Generate path: muxctl-{session}-{PID}.sock
	pid := os.Getpid()
	socketName := fmt.Sprintf("muxctl-%s-%d.sock", sessionName, pid)
	socketPath := filepath.Join(runtimeDir, socketName)

	// Clean up any existing socket
	os.Remove(socketPath)

	return socketPath, nil
}

// Close terminates the tmux server and closes the PTY.
func (s *Session) Close() error {
	debug.Log("Session.Close: closing session %s", s.Name)

	// Kill tmux session first
	if s.Controller != nil {
		s.Controller.exec("kill-session", "-t", s.Name)
	}

	// Close PTY (this will also kill tmux if still running)
	if s.PTY != nil {
		s.PTY.Close()
	}

	// Clean up socket file
	if s.SocketPath != "" {
		os.Remove(s.SocketPath)
	}

	return nil
}

// CreateViewport creates a TerminalViewport for this session.
func (s *Session) CreateViewport(width, height int) *TerminalViewport {
	// Get the active pane ID for capture-pane
	paneID, err := s.Controller.GetActivePane()
	if err != nil {
		debug.Log("Session.CreateViewport: failed to get active pane: %v", err)
		// Fall back to empty pane ID
		paneID = NewPaneID("")
	}

	vp := NewTerminalViewport(s.PTY, width, height)
	vp.controller = s.Controller
	vp.paneID = paneID
	return vp
}

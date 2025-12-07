package embedded

import (
	"fmt"
	"sync"

	"github.com/xunzhou/muxctl/internal/debug"
)

// ContextShellPool manages persistent tmux windows per Kubernetes context.
// Each context gets its own dedicated window with a persistent shell.
type ContextShellPool struct {
	ctrl       *TmuxController
	session    string
	shells     map[string]WindowID // context name -> window ID
	shellsMu   sync.RWMutex
	shellCmd   []string // command to run in each shell (default: user's $SHELL)
}

// NewContextShellPool creates a pool for managing context shells.
func NewContextShellPool(ctrl *TmuxController, session string) *ContextShellPool {
	return &ContextShellPool{
		ctrl:     ctrl,
		session:  session,
		shells:   make(map[string]WindowID),
		shellCmd: []string{}, // Empty means use tmux default (user's shell)
	}
}

// SetShellCommand sets the command to run in each context shell.
// If not set, tmux will use the user's default shell.
func (p *ContextShellPool) SetShellCommand(cmd []string) {
	p.shellCmd = cmd
}

// GetOrCreate returns the window ID for the given context, creating it if needed.
// Window naming: "context-shell-<context-name>"
func (p *ContextShellPool) GetOrCreate(ctx string) (WindowID, error) {
	debug.Log("ContextShellPool.GetOrCreate: context=%s", ctx)

	p.shellsMu.RLock()
	if winID, exists := p.shells[ctx]; exists {
		p.shellsMu.RUnlock()
		debug.Log("ContextShellPool.GetOrCreate: found existing window %s for context %s", winID.TmuxID, ctx)
		return winID, nil
	}
	p.shellsMu.RUnlock()

	// Create new window for this context
	p.shellsMu.Lock()
	defer p.shellsMu.Unlock()

	// Double-check after acquiring write lock
	if winID, exists := p.shells[ctx]; exists {
		return winID, nil
	}

	windowName := fmt.Sprintf("context-shell-%s", ctx)
	winID, err := p.ctrl.NewWindow(windowName, p.shellCmd)
	if err != nil {
		return WindowID{}, fmt.Errorf("failed to create window for context %s: %w", ctx, err)
	}

	p.shells[ctx] = winID

	debug.Log("ContextShellPool.GetOrCreate: created window %s for context %s", winID.TmuxID, ctx)

	return winID, nil
}

// Switch switches to the window for the given context, creating it if needed.
func (p *ContextShellPool) Switch(ctx string) error {
	debug.Log("ContextShellPool.Switch: context=%s", ctx)

	winID, err := p.GetOrCreate(ctx)
	if err != nil {
		return err
	}

	return p.ctrl.SelectWindow(winID)
}

// Get returns the window ID for the given context if it exists.
func (p *ContextShellPool) Get(ctx string) (WindowID, bool) {
	p.shellsMu.RLock()
	defer p.shellsMu.RUnlock()

	winID, exists := p.shells[ctx]
	return winID, exists
}

// List returns all managed contexts.
func (p *ContextShellPool) List() []string {
	p.shellsMu.RLock()
	defer p.shellsMu.RUnlock()

	contexts := make([]string, 0, len(p.shells))
	for ctx := range p.shells {
		contexts = append(contexts, ctx)
	}

	return contexts
}

// Remove removes the window for the given context.
func (p *ContextShellPool) Remove(ctx string) error {
	debug.Log("ContextShellPool.Remove: context=%s", ctx)

	p.shellsMu.Lock()
	defer p.shellsMu.Unlock()

	winID, exists := p.shells[ctx]
	if !exists {
		return fmt.Errorf("context %s not found in pool", ctx)
	}

	// Kill the window
	if err := p.ctrl.KillWindow(winID); err != nil {
		return fmt.Errorf("failed to kill window for context %s: %w", ctx, err)
	}

	delete(p.shells, ctx)

	debug.Log("ContextShellPool.Remove: removed context %s", ctx)

	return nil
}

// Cleanup closes all managed windows.
func (p *ContextShellPool) Cleanup() error {
	debug.Log("ContextShellPool.Cleanup: cleaning up all windows")

	p.shellsMu.Lock()
	defer p.shellsMu.Unlock()

	var errors []error

	for ctx, winID := range p.shells {
		debug.Log("ContextShellPool.Cleanup: killing window %s for context %s", winID.TmuxID, ctx)

		if err := p.ctrl.KillWindow(winID); err != nil {
			errors = append(errors, fmt.Errorf("failed to kill window for %s: %w", ctx, err))
		}
	}

	// Clear the map
	p.shells = make(map[string]WindowID)

	if len(errors) > 0 {
		return fmt.Errorf("cleanup completed with %d errors: %v", len(errors), errors)
	}

	debug.Log("ContextShellPool.Cleanup: cleanup complete")

	return nil
}

// Count returns the number of active context shells.
func (p *ContextShellPool) Count() int {
	p.shellsMu.RLock()
	defer p.shellsMu.RUnlock()

	return len(p.shells)
}

// IsManaged returns true if the pool manages a window for the given context.
func (p *ContextShellPool) IsManaged(ctx string) bool {
	p.shellsMu.RLock()
	defer p.shellsMu.RUnlock()

	_, exists := p.shells[ctx]
	return exists
}

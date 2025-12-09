package pool

import (
	"fmt"
	"sync"

	"github.com/xunzhou/muxctl/pkg/tmux"
)

// WindowPool manages a pool of tmux windows with limits
type WindowPool struct {
	manager    *tmux.Manager
	maxWindows int
	prefix     string
	windows    map[string]string // id -> pane ID
	mu         sync.Mutex
}

// NewWindowPool creates a new window pool
// If maxWindows is 0, no limit is enforced
func NewWindowPool(manager *tmux.Manager, maxWindows int, prefix string) *WindowPool {
	return &WindowPool{
		manager:    manager,
		maxWindows: maxWindows,
		prefix:     prefix,
		windows:    make(map[string]string),
	}
}

// GetOrCreate gets an existing window or creates a new one
// setupFn is optional - if provided, it will be called with the window ID after creation
func (p *WindowPool) GetOrCreate(id string, setupFn ...func(int) error) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if window already exists
	if paneID, exists := p.windows[id]; exists {
		return paneID, nil
	}

	// Check if we've hit the limit
	if p.maxWindows > 0 && len(p.windows) >= p.maxWindows {
		return "", fmt.Errorf("window pool limit reached (%d)", p.maxWindows)
	}

	// Create a new resource window
	resourceID := fmt.Sprintf("%s%s", p.prefix, id)
	if err := p.manager.AttachResourceTerminal(resourceID); err != nil {
		return "", fmt.Errorf("failed to create window: %w", err)
	}

	// Get the pane ID
	paneID := p.manager.GetBottomPane()
	p.windows[id] = paneID

	// Call setup function if provided
	if len(setupFn) > 0 && setupFn[0] != nil {
		// Call with a dummy window ID (0 for now)
		if err := setupFn[0](0); err != nil {
			return "", fmt.Errorf("setup function failed: %w", err)
		}
	}

	return paneID, nil
}

// Switch switches to an existing window
func (p *WindowPool) Switch(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if window exists
	if _, exists := p.windows[id]; !exists {
		return fmt.Errorf("window %s does not exist", id)
	}

	// Switch to the resource
	resourceID := fmt.Sprintf("%s%s", p.prefix, id)
	return p.manager.AttachResourceTerminal(resourceID)
}

// Close closes a window
func (p *WindowPool) Close(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if window exists
	if _, exists := p.windows[id]; !exists {
		return fmt.Errorf("window %s does not exist", id)
	}

	// Close the resource pane
	resourceID := fmt.Sprintf("%s%s", p.prefix, id)
	if err := p.manager.CloseResourcePane(resourceID); err != nil {
		return fmt.Errorf("failed to close window: %w", err)
	}

	// Remove from tracking
	delete(p.windows, id)

	return nil
}

// Count returns the number of windows in the pool
func (p *WindowPool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.windows)
}

// List returns all window IDs
func (p *WindowPool) List() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	ids := make([]string, 0, len(p.windows))
	for id := range p.windows {
		ids = append(ids, id)
	}
	return ids
}

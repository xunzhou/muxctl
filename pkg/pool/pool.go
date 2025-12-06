package pool

import (
	"container/list"
	"fmt"
	"strconv"
	"time"

	"github.com/xunzhou/muxctl/pkg/controller"
)

// WindowPool manages a pool of diagnostic windows with LRU eviction.
// It ensures a maximum number of windows are open at once, automatically
// closing the least recently used window when the pool is full.
// If maxWindows is 0, no limit is enforced (unlimited windows).
type WindowPool struct {
	ctrl       controller.Controller
	maxWindows int
	windows    map[string]*WindowEntry
	lru        *list.List
	namePrefix string
}

// WindowEntry tracks metadata about a pooled window.
type WindowEntry struct {
	Name       string
	WindowID   int
	CreatedAt  time.Time
	LastAccess time.Time
	lruElement *list.Element
}

// NewWindowPool creates a new WindowPool with the given controller and settings.
// If maxWindows is 0, no limit is enforced (unlimited windows).
func NewWindowPool(ctrl controller.Controller, maxWindows int, prefix string) *WindowPool {
	return &WindowPool{
		ctrl:       ctrl,
		maxWindows: maxWindows,
		windows:    make(map[string]*WindowEntry),
		lru:        list.New(),
		namePrefix: prefix,
	}
}

// GetOrCreate returns an existing window or creates a new one.
// If the pool is full (and maxWindows > 0), it evicts the least recently used window.
// The setupFunc is called after creating a new window to initialize it.
func (p *WindowPool) GetOrCreate(name string, setupFunc func(windowID int) error) (int, error) {
	fullName := p.namePrefix + name

	// Check if window already exists
	if entry, exists := p.windows[fullName]; exists {
		p.touch(entry)

		// Switch to existing window
		if err := p.ctrl.SwitchToWindow(fullName); err != nil {
			return 0, fmt.Errorf("failed to switch to window: %w", err)
		}

		return entry.WindowID, nil
	}

	// Need to create new window - check if pool is full (if limit is set)
	if p.maxWindows > 0 && len(p.windows) >= p.maxWindows {
		if err := p.evictLRU(); err != nil {
			return 0, fmt.Errorf("failed to evict LRU window: %w", err)
		}
	}

	// Create the window
	windowID, err := p.ctrl.CreateWindow(fullName)
	if err != nil {
		return 0, fmt.Errorf("failed to create window: %w", err)
	}

	// Store last access time in window metadata
	if err := p.ctrl.SetWindowMetadata(fullName, "last_access", strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
		// Non-fatal - continue even if metadata fails
		fmt.Printf("Warning: failed to set window metadata: %v\n", err)
	}

	// Run setup function if provided
	if setupFunc != nil {
		if err := setupFunc(windowID); err != nil {
			// Cleanup on failure
			p.ctrl.CloseWindow(fullName)
			return 0, fmt.Errorf("setup failed: %w", err)
		}
	}

	// Track window in pool
	entry := &WindowEntry{
		Name:       fullName,
		WindowID:   windowID,
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
	}
	entry.lruElement = p.lru.PushFront(entry)
	p.windows[fullName] = entry

	// Switch to new window
	if err := p.ctrl.SwitchToWindow(fullName); err != nil {
		return 0, fmt.Errorf("failed to switch to new window: %w", err)
	}

	return windowID, nil
}

// evictLRU removes the least recently used window from the pool.
func (p *WindowPool) evictLRU() error {
	if p.lru.Len() == 0 {
		return fmt.Errorf("no windows to evict")
	}

	// Get LRU entry from back of list
	lruElem := p.lru.Back()
	entry := lruElem.Value.(*WindowEntry)

	// Don't evict the currently active window
	windows, err := p.ctrl.ListWindows()
	if err == nil {
		for _, w := range windows {
			if w.Name == entry.Name && w.Active {
				// This is the active window, try to evict second-to-last instead
				if p.lru.Len() > 1 {
					lruElem = lruElem.Prev()
					entry = lruElem.Value.(*WindowEntry)
				} else {
					return fmt.Errorf("cannot evict only window (it's active)")
				}
			}
		}
	}

	// Close the window
	if err := p.ctrl.CloseWindow(entry.Name); err != nil {
		return fmt.Errorf("failed to close window %s: %w", entry.Name, err)
	}

	// Remove from tracking
	p.lru.Remove(lruElem)
	delete(p.windows, entry.Name)

	return nil
}

// touch updates the last access time for a window and moves it to the front of the LRU list.
func (p *WindowPool) touch(entry *WindowEntry) {
	entry.LastAccess = time.Now()
	p.lru.MoveToFront(entry.lruElement)

	// Update metadata
	if err := p.ctrl.SetWindowMetadata(entry.Name, "last_access", strconv.FormatInt(entry.LastAccess.Unix(), 10)); err != nil {
		// Non-fatal
		fmt.Printf("Warning: failed to update window metadata: %v\n", err)
	}
}

// Close closes a specific window in the pool by name (without prefix).
func (p *WindowPool) Close(name string) error {
	fullName := p.namePrefix + name

	entry, exists := p.windows[fullName]
	if !exists {
		return fmt.Errorf("window not in pool: %s", name)
	}

	// Close window
	if err := p.ctrl.CloseWindow(fullName); err != nil {
		return fmt.Errorf("failed to close window: %w", err)
	}

	// Remove from tracking
	p.lru.Remove(entry.lruElement)
	delete(p.windows, fullName)

	return nil
}

// List returns all windows currently in the pool.
func (p *WindowPool) List() []*WindowEntry {
	entries := make([]*WindowEntry, 0, len(p.windows))
	for _, entry := range p.windows {
		entries = append(entries, entry)
	}
	return entries
}

// Size returns the current number of windows in the pool.
func (p *WindowPool) Size() int {
	return len(p.windows)
}

// MaxSize returns the maximum number of windows allowed in the pool.
// Returns 0 if unlimited.
func (p *WindowPool) MaxSize() int {
	return p.maxWindows
}

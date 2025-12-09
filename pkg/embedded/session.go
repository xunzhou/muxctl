package embedded

import (
	"fmt"

	"github.com/xunzhou/muxctl/pkg/tmux"
)

// Session represents an embedded tmux session
type Session struct {
	Manager    *tmux.Manager
	Controller *tmux.Manager // Alias for compatibility
	Name       string
	Width      int
	Height     int
}

// NewEmbeddedSession creates a new embedded session
func NewEmbeddedSession(name string, width, height int) (*Session, error) {
	// Check if tmux is available
	if _, err := tmux.TmuxCmd("display-message", "-p", "#{version}"); err != nil {
		return nil, fmt.Errorf("tmux not available: %w", err)
	}

	mgr, err := tmux.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	if err := mgr.Setup(); err != nil {
		return nil, fmt.Errorf("failed to setup: %w", err)
	}

	return &Session{
		Manager:    mgr,
		Controller: mgr,
		Name:       name,
		Width:      width,
		Height:     height,
	}, nil
}

// CreateViewport creates a new terminal viewport for this session
func (s *Session) CreateViewport(width, height int) *TerminalViewport {
	return NewTerminalViewport(s.Manager, width, height)
}

// Close closes the session
func (s *Session) Close() error {
	if s.Manager != nil {
		s.Manager.Cleanup()
	}
	return nil
}

// ContextShellPool manages separate shell sessions for different contexts (e.g., k8s clusters)
type ContextShellPool struct {
	manager *tmux.Manager
	prefix  string
	shells  map[string]string // context -> pane ID
}

// NewContextShellPool creates a new context shell pool
func NewContextShellPool(manager *tmux.Manager, prefix string) *ContextShellPool {
	return &ContextShellPool{
		manager: manager,
		prefix:  prefix,
		shells:  make(map[string]string),
	}
}

// GetOrCreateContext gets or creates a shell for the given context
func (p *ContextShellPool) GetOrCreateContext(ctx string) (string, error) {
	// Check if we already have a pane for this context
	if paneID, exists := p.shells[ctx]; exists {
		return paneID, nil
	}

	// Create a new resource terminal for this context
	resourceID := fmt.Sprintf("%s-%s", p.prefix, ctx)
	if err := p.manager.AttachResourceTerminal(resourceID); err != nil {
		return "", fmt.Errorf("failed to create context shell: %w", err)
	}

	// Get the pane ID
	paneID := p.manager.GetBottomPane()
	p.shells[ctx] = paneID

	return paneID, nil
}

// SwitchContext switches to the shell for the given context
func (p *ContextShellPool) SwitchContext(ctx string) error {
	resourceID := fmt.Sprintf("%s-%s", p.prefix, ctx)
	return p.manager.AttachResourceTerminal(resourceID)
}

// Switch switches to an existing context shell (alias for SwitchContext)
func (p *ContextShellPool) Switch(ctx string) error {
	return p.SwitchContext(ctx)
}

// Cleanup cleans up all context shells
func (p *ContextShellPool) Cleanup() error {
	// Close all shells
	for ctx := range p.shells {
		resourceID := fmt.Sprintf("%s-%s", p.prefix, ctx)
		p.manager.CloseResourcePane(resourceID)
	}
	p.shells = make(map[string]string)
	return nil
}

// TerminalViewport provides a view into a terminal pane
type TerminalViewport struct {
	manager      *tmux.Manager
	width        int
	height       int
	activePaneID string
	buffer       []byte
}

// NewTerminalViewport creates a new terminal viewport
func NewTerminalViewport(manager *tmux.Manager, width, height int) *TerminalViewport {
	return &TerminalViewport{
		manager: manager,
		width:   width,
		height:  height,
	}
}

// Update refreshes the viewport content
func (v *TerminalViewport) Update() (string, error) {
	// Capture output from the bottom pane
	paneID := v.manager.GetBottomPane()
	if paneID == "" {
		return "", nil
	}

	// Capture pane content
	output, err := tmux.TmuxCmd("capture-pane", "-t", paneID, "-p")
	if err != nil {
		return "", fmt.Errorf("failed to capture pane: %w", err)
	}

	return output, nil
}

// SendKeys sends keys to the active pane
func (v *TerminalViewport) SendKeys(keys string) error {
	paneID := v.manager.GetBottomPane()
	if paneID == "" {
		return fmt.Errorf("no active pane")
	}

	_, err := tmux.TmuxCmd("send-keys", "-t", paneID, keys)
	return err
}

// SetActivePane sets the active pane for this viewport
func (v *TerminalViewport) SetActivePane(paneID string) {
	v.activePaneID = paneID
}

// Resize resizes the viewport
func (v *TerminalViewport) Resize(width, height int) {
	v.width = width
	v.height = height
}

// SetProgram is a no-op for compatibility (Bubble Tea program management handled elsewhere)
func (v *TerminalViewport) SetProgram(p interface{}) {
	// No-op: program management is handled at a higher level
}

// Init initializes the viewport (returns nil Cmd for Bubble Tea compatibility)
func (v *TerminalViewport) Init() interface{} {
	// Return nil (valid tea.Cmd)
	return nil
}

// SetTargetPane sets the target pane for this viewport
func (v *TerminalViewport) SetTargetPane(paneID string) {
	v.activePaneID = paneID
}

// HandleKey handles a key press in terminal mode
func (v *TerminalViewport) HandleKey(keyMsg interface{}) error {
	// Extract the key string from the message
	// For now, just convert to string
	return v.SendKeys(fmt.Sprintf("%v", keyMsg))
}

// View returns the current view of the terminal
func (v *TerminalViewport) View() string {
	output, err := v.Update()
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return output
}

// CaptureOptions provides options for capturing pane content
type CaptureOptions struct {
	Start int  // Start line
	End   int  // End line
	Lines int  // Number of lines to capture
	Join  bool // Join wrapped lines
}

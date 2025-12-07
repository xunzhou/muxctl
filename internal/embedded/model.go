package embedded

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/xunzhou/muxctl/internal/debug"
)

// Mode represents the interaction mode for the TUI.
type Mode int

const (
	ModeTUI      Mode = iota // TUI mode: keys control Bubble Tea widgets
	ModeTerminal             // Terminal mode: keys forwarded raw to PTY
)

func (m Mode) String() string {
	switch m {
	case ModeTUI:
		return "TUI"
	case ModeTerminal:
		return "Terminal"
	default:
		return "Unknown"
	}
}

// Tab represents the different tabs in the TUI.
type Tab int

const (
	TabTerminal Tab = iota
	TabDetail
	TabAI
	TabHistory
)

func (t Tab) String() string {
	switch t {
	case TabTerminal:
		return "Terminal"
	case TabDetail:
		return "Detail"
	case TabAI:
		return "AI"
	case TabHistory:
		return "History"
	default:
		return "Unknown"
	}
}

// Model is the main Bubble Tea model implementing the spec's dual-mode system.
type Model struct {
	// Mode and state
	mode      Mode
	activeTab Tab

	// Components
	Viewport *TerminalViewport // Exported for external access
	width    int
	height   int

	// Session reference
	session *Session
}

// NewModel creates a new TUI model with the embedded session.
func NewModel(session *Session, width, height int) *Model {
	viewport := session.CreateViewport(width, height)

	return &Model{
		mode:      ModeTUI,
		activeTab: TabTerminal,
		Viewport:  viewport,
		width:     width,
		height:    height,
		session:   session,
	}
}

// Init implements tea.Model.Init().
func (m *Model) Init() tea.Cmd {
	return m.Viewport.Init()
}

// Update implements tea.Model.Update().
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.Viewport.Resize(msg.Width, msg.Height)
		return m, nil

	case PtyOutputMsg:
		// Forward PTY messages to viewport
		updatedViewport, cmd := m.Viewport.Update(msg)
		m.Viewport = updatedViewport.(*TerminalViewport)
		return m, cmd
	}

	return m, nil
}

// handleKeyMsg processes keyboard input based on current mode.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global mode switch keys (work in any mode/tab)

	// Ctrl+Alt+J: Jump to terminal (switch to Terminal tab + enter Terminal mode)
	if msg.Alt && msg.Type == tea.KeyCtrlJ {
		debug.Log("Model: Ctrl+Alt+J pressed - jumping to terminal mode")
		m.activeTab = TabTerminal
		m.mode = ModeTerminal
		return m, nil
	}

	// Ctrl+Alt+K: Escape to TUI (exit Terminal mode, stay on current tab)
	if msg.Alt && msg.Type == tea.KeyCtrlK {
		debug.Log("Model: Ctrl+Alt+K pressed - escaping to TUI mode")
		m.mode = ModeTUI
		return m, nil
	}

	// Ctrl+Q: Quit application
	if msg.Type == tea.KeyCtrlC {
		debug.Log("Model: Ctrl+C pressed - quitting")
		return m, tea.Quit
	}

	// In Terminal mode, forward all other keys to PTY
	if m.mode == ModeTerminal {
		if m.activeTab == TabTerminal {
			m.Viewport.HandleKey(msg)
		}
		return m, nil
	}

	// TUI mode: handle tab switching and widget navigation
	return m.handleTUIKeys(msg)
}

// handleTUIKeys processes keys in TUI mode (tab switching, navigation, etc.).
func (m *Model) handleTUIKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Tab switching with numbers (1-4)
	switch msg.String() {
	case "1":
		m.switchTab(TabTerminal)
	case "2":
		m.switchTab(TabDetail)
	case "3":
		m.switchTab(TabAI)
	case "4":
		m.switchTab(TabHistory)
	}

	// Tab navigation with arrow keys
	switch msg.Type {
	case tea.KeyRight:
		m.nextTab()
	case tea.KeyLeft:
		m.prevTab()
	}

	return m, nil
}

// switchTab changes the active tab and auto-exits Terminal mode.
func (m *Model) switchTab(tab Tab) {
	debug.Log("Model: switching to tab %s (from %s)", tab, m.activeTab)

	// Auto-exit Terminal mode when switching tabs (per spec)
	if m.mode == ModeTerminal {
		debug.Log("Model: auto-exiting Terminal mode due to tab switch")
		m.mode = ModeTUI
	}

	m.activeTab = tab
}

// nextTab moves to the next tab (wraps around).
func (m *Model) nextTab() {
	next := (m.activeTab + 1) % 4
	m.switchTab(Tab(next))
}

// prevTab moves to the previous tab (wraps around).
func (m *Model) prevTab() {
	prev := (m.activeTab + 3) % 4 // +3 is same as -1 mod 4
	m.switchTab(Tab(prev))
}

// View implements tea.Model.View().
func (m *Model) View() string {
	// Render tab bar
	tabBar := m.renderTabBar()

	// Render active tab content
	var content string
	switch m.activeTab {
	case TabTerminal:
		content = m.Viewport.View()
	case TabDetail:
		content = "Detail view (not implemented yet)"
	case TabAI:
		content = "AI view (not implemented yet)"
	case TabHistory:
		content = "History view (not implemented yet)"
	}

	// Render status bar (shows current mode)
	statusBar := m.renderStatusBar()

	return tabBar + "\n" + content + "\n" + statusBar
}

// renderTabBar renders the tab navigation bar.
func (m *Model) renderTabBar() string {
	tabs := []string{
		m.renderTab(TabTerminal, "[1]Terminal"),
		m.renderTab(TabDetail, "[2]Detail"),
		m.renderTab(TabAI, "[3]AI"),
		m.renderTab(TabHistory, "[4]History"),
	}

	// Join with spacing
	bar := "Tabs: "
	for i, tab := range tabs {
		if i > 0 {
			bar += "  "
		}
		bar += tab
	}

	return bar
}

// renderTab renders a single tab with highlighting if active.
func (m *Model) renderTab(tab Tab, label string) string {
	if m.activeTab == tab {
		return "\033[1;36m" + label + "\033[0m" // Cyan and bold
	}
	return label
}

// renderStatusBar shows the current mode and keyboard shortcuts.
func (m *Model) renderStatusBar() string {
	modeStr := "Mode: " + m.mode.String()

	var hints string
	if m.mode == ModeTUI {
		hints = "Ctrl+Alt+J: Enter Terminal | 1-4: Switch Tabs | Ctrl+C: Quit"
	} else {
		hints = "Ctrl+Alt+K: Exit Terminal Mode | Ctrl+C: Quit"
	}

	return modeStr + " | " + hints
}

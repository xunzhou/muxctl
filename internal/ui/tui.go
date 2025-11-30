package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xunzhou/muxctl/internal/context"
)

// formatActionError formats an action error for display in the status line.
// It extracts the most relevant error message and truncates if needed.
func formatActionError(action string, err error) string {
	errStr := err.Error()

	// Extract the innermost error message (after last colon)
	if idx := strings.LastIndex(errStr, ": "); idx != -1 {
		errStr = strings.TrimSpace(errStr[idx+2:])
	}

	// Truncate if too long
	maxLen := 60
	if len(errStr) > maxLen {
		errStr = errStr[:maxLen-3] + "..."
	}

	return fmt.Sprintf("%s failed: %s", action, errStr)
}

// Styles for the TUI
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170")).
			MarginBottom(1)

	contextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	actionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)
)

// Action represents a menu action.
type Action struct {
	Key         string
	Label       string
	Description string
}

// RefreshFunc is a function that refreshes the context.
type RefreshFunc func() (context.Context, error)

// ActionFunc is a function that executes an action (e.g., open logs pane).
type ActionFunc func(action string) error

// Model represents the Bubble Tea model for the TUI.
type Model struct {
	ctx         context.Context
	ctxChan     <-chan context.Context
	refreshFunc RefreshFunc
	actionFunc  ActionFunc
	width       int
	height      int
	status      string
	statusErr   bool
	quitting    bool
	actions     []Action
	selected    int
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, ctxChan <-chan context.Context, refreshFunc RefreshFunc, actionFunc ActionFunc) Model {
	return Model{
		ctx:         ctx,
		ctxChan:     ctxChan,
		refreshFunc: refreshFunc,
		actionFunc:  actionFunc,
		status:      "Ready",
		actions: []Action{
			{Key: "l", Label: "Logs", Description: "Open kubectl logs pane"},
			{Key: "s", Label: "Shell", Description: "Open new context shell"},
			{Key: "r", Label: "Refresh", Description: "Refresh context"},
			{Key: "1", Label: "AI Summarize", Description: "Summarize output with AI"},
			{Key: "2", Label: "AI Explain", Description: "Explain errors with AI"},
		},
		selected: 0,
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return waitForContextUpdate(m.ctxChan)
}

// Message types
type contextUpdateMsg struct {
	ctx context.Context
}

type refreshResultMsg struct {
	ctx context.Context
	err error
}

type actionResultMsg struct {
	action string
	err    error
}

// waitForContextUpdate waits for context updates from the channel.
func waitForContextUpdate(ch <-chan context.Context) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		ctx, ok := <-ch
		if !ok {
			return nil
		}
		return contextUpdateMsg{ctx: ctx}
	}
}

// doRefresh creates a command that refreshes the context.
func doRefresh(fn RefreshFunc) tea.Cmd {
	return func() tea.Msg {
		if fn == nil {
			return refreshResultMsg{err: fmt.Errorf("refresh not available")}
		}
		ctx, err := fn()
		return refreshResultMsg{ctx: ctx, err: err}
	}
}

// doAction creates a command that executes an action.
func doAction(fn ActionFunc, action string) tea.Cmd {
	return func() tea.Msg {
		if fn == nil {
			return actionResultMsg{action: action, err: fmt.Errorf("action not available")}
		}
		err := fn(action)
		return actionResultMsg{action: action, err: err}
	}
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil

		case "down", "j":
			if m.selected < len(m.actions)-1 {
				m.selected++
			}
			return m, nil

		case "enter", " ":
			return m.executeSelectedAction()

		case "l":
			m.status = "Opening logs pane..."
			m.statusErr = false
			return m, doAction(m.actionFunc, "logs")

		case "s":
			m.status = "Opening shell pane..."
			m.statusErr = false
			return m, doAction(m.actionFunc, "shell")

		case "r":
			m.status = "Refreshing context..."
			m.statusErr = false
			return m, doRefresh(m.refreshFunc)

		case "1":
			m.status = "Running AI summarize..."
			m.statusErr = false
			return m, doAction(m.actionFunc, "ai-summarize")

		case "2":
			m.status = "Running AI explain..."
			m.statusErr = false
			return m, doAction(m.actionFunc, "ai-explain")
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case contextUpdateMsg:
		m.ctx = msg.ctx
		m.status = "Context updated"
		m.statusErr = false
		return m, waitForContextUpdate(m.ctxChan)

	case refreshResultMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Refresh failed: %v", msg.err)
			m.statusErr = true
		} else {
			m.ctx = msg.ctx
			m.status = "Context refreshed"
			m.statusErr = false
		}
		return m, nil

	case actionResultMsg:
		if msg.err != nil {
			m.status = formatActionError(msg.action, msg.err)
			m.statusErr = true
		} else {
			m.status = fmt.Sprintf("Action '%s' completed", msg.action)
			m.statusErr = false
		}
		return m, nil
	}

	return m, nil
}

// executeSelectedAction executes the currently selected action.
func (m Model) executeSelectedAction() (tea.Model, tea.Cmd) {
	if m.selected >= len(m.actions) {
		return m, nil
	}

	action := m.actions[m.selected]
	switch action.Key {
	case "l":
		m.status = "Opening logs pane..."
		m.statusErr = false
		return m, doAction(m.actionFunc, "logs")
	case "s":
		m.status = "Opening shell pane..."
		m.statusErr = false
		return m, doAction(m.actionFunc, "shell")
	case "r":
		m.status = "Refreshing context..."
		m.statusErr = false
		return m, doRefresh(m.refreshFunc)
	case "1":
		m.status = "Running AI summarize..."
		m.statusErr = false
		return m, doAction(m.actionFunc, "ai-summarize")
	case "2":
		m.status = "Running AI explain..."
		m.statusErr = false
		return m, doAction(m.actionFunc, "ai-explain")
	}

	return m, nil
}

// View renders the TUI.
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	// Title
	title := titleStyle.Render("muxctl Dashboard")

	// Context info
	contextInfo := m.renderContext()

	// Actions menu
	actionsMenu := m.renderActions()

	// Status
	var status string
	if m.statusErr {
		status = errorStyle.Render(fmt.Sprintf("Status: %s", m.status))
	} else {
		status = statusStyle.Render(fmt.Sprintf("Status: %s", m.status))
	}

	// Help
	help := helpStyle.Render("q: quit • ↑/↓: navigate • enter: select • l: logs • s: shell • r: refresh • 1-2: AI")

	return fmt.Sprintf("%s\n\n%s\n%s\n%s\n\n%s", title, contextInfo, actionsMenu, status, help)
}

// renderContext renders the current context information.
func (m Model) renderContext() string {
	ctx := m.ctx

	lines := []string{}

	if ctx.Cluster != "" {
		lines = append(lines, fmt.Sprintf("Cluster:     %s", ctx.Cluster))
	} else {
		lines = append(lines, "Cluster:     (not set)")
	}

	if ctx.Environment != "" {
		lines = append(lines, fmt.Sprintf("Environment: %s", ctx.Environment))
	}

	if ctx.Region != "" {
		lines = append(lines, fmt.Sprintf("Region:      %s", ctx.Region))
	}

	if ctx.Namespace != "" {
		lines = append(lines, fmt.Sprintf("Namespace:   %s", ctx.Namespace))
	} else {
		lines = append(lines, "Namespace:   default")
	}

	if ctx.KubeContext != "" {
		lines = append(lines, fmt.Sprintf("KubeContext: %s", ctx.KubeContext))
	}

	// Show custom metadata if any
	for k, v := range ctx.Metadata {
		lines = append(lines, fmt.Sprintf("%s: %s", k, v))
	}

	result := ""
	for _, line := range lines {
		result += contextStyle.Render(line) + "\n"
	}

	return result
}

// renderActions renders the actions menu.
func (m Model) renderActions() string {
	result := actionStyle.Render("Actions:") + "\n"

	for i, action := range m.actions {
		prefix := "  "
		style := contextStyle
		if i == m.selected {
			prefix = "> "
			style = selectedStyle
		}
		line := fmt.Sprintf("%s[%s] %s - %s", prefix, action.Key, action.Label, action.Description)
		result += style.Render(line) + "\n"
	}

	return result
}

// RunTUI starts the Bubble Tea program.
func RunTUI(ctx context.Context, ctxChan <-chan context.Context, refreshFunc RefreshFunc, actionFunc ActionFunc) error {
	p := tea.NewProgram(
		NewModel(ctx, ctxChan, refreshFunc, actionFunc),
		tea.WithAltScreen(),
	)

	_, err := p.Run()
	return err
}

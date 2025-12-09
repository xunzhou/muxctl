package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Model is the Bubble Tea model for the terminal multiplexer
type Model struct {
	tmux             *TmuxManager
	resources        []string
	selectedIdx      int
	activeResourceID string
	message          string
	quitting         bool
}

// NewModel creates a new model
func NewModel(tmux *TmuxManager) *Model {
	return &Model{
		tmux: tmux,
		resources: []string{
			"pod-a",
			"pod-b",
			"pod-c",
			"service-x",
			"service-y",
		},
		selectedIdx: 0,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type tickMsg time.Time

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		// Periodic cleanup and status bar update
		m.tmux.updateStatusBar()
		return m, tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			// Use tmux confirm-before to ask for confirmation
			// This will show a prompt at the bottom of the screen
			tmuxCmd("confirm-before", "-p", "Really quit? (y/n)", "kill-session")
			return m, nil

		case "ctrl+c":
			// Ctrl+C still quits immediately without confirmation
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}

		case "down", "j":
			if m.selectedIdx < len(m.resources)-1 {
				m.selectedIdx++
			}

		case "enter":
			// Activate the selected resource
			resourceID := m.resources[m.selectedIdx]
			if err := m.tmux.AttachResourceTerminal(resourceID); err != nil {
				m.message = fmt.Sprintf("Error: %v", err)
			} else {
				m.activeResourceID = resourceID
				m.message = fmt.Sprintf("Activated: %s", resourceID)
			}

		case "x":
			// Close the selected resource pane
			resourceID := m.resources[m.selectedIdx]
			if err := m.tmux.CloseResourcePane(resourceID); err != nil {
				m.message = fmt.Sprintf("Error closing: %v", err)
			} else {
				// If we closed the active resource, clear it
				if m.activeResourceID == resourceID {
					m.activeResourceID = ""
				}
				m.message = fmt.Sprintf("Closed: %s", resourceID)
			}

		case "a":
			// Launch new AI chat
			if err := m.tmux.AttachAIChat(); err != nil {
				m.message = fmt.Sprintf("Error launching AI chat: %v", err)
			} else {
				m.activeResourceID = ""
				m.message = "Launched new AI chat"
			}

		case "A":
			// Show choose-tree for selecting AI chats
			m.tmux.ShowAIChooser()
			m.message = "Opening AI chat selector..."
		}
	}

	return m, nil
}

func (m *Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var b strings.Builder

	b.WriteString("╔═══════════════════════════════════╗\n")
	b.WriteString("║      Terminal Multiplexer         ║\n")
	b.WriteString("╚═══════════════════════════════════╝\n\n")

	b.WriteString("Resources:\n")
	stashedResources := m.tmux.GetStashedResources()
	stashedMap := make(map[string]bool)
	for _, res := range stashedResources {
		stashedMap[res] = true
	}

	for i, res := range m.resources {
		prefix := "  "
		if i == m.selectedIdx {
			prefix = "► "
		}

		marker := ""
		if res == m.activeResourceID {
			marker = " ●"
		} else if stashedMap[res] {
			marker = " ○"
		}

		b.WriteString(fmt.Sprintf("%s%s%s\n", prefix, res, marker))
	}

	b.WriteString("\nIndicators:\n")
	b.WriteString("  ●         - Active (visible)\n")
	b.WriteString("  ○         - Stashed (background)\n")
	b.WriteString("\nKeybindings:\n")
	b.WriteString("  ↑/k       - Move selection up\n")
	b.WriteString("  ↓/j       - Move selection down\n")
	b.WriteString("  ENTER     - Activate resource terminal\n")
	b.WriteString("  a         - Launch new AI chat\n")
	b.WriteString("  A         - Choose AI/Resource (^A=AI ^R=Res ^T=All)\n")
	b.WriteString("  x         - Close selected resource pane\n")
	b.WriteString("  Alt+Enter - Focus TUI (from terminal)\n")
	b.WriteString("  q         - Quit\n\n")

	if m.activeResourceID != "" {
		b.WriteString(fmt.Sprintf("Active: %s\n", m.activeResourceID))
	} else {
		b.WriteString("Active: None\n")
	}

	// Show compact pane list status
	paneInfo := m.tmux.GetPaneInfo()
	if len(paneInfo) > 0 {
		b.WriteString("\nPanes: ")
		var paneList []string
		for resID := range paneInfo {
			if resID == m.activeResourceID {
				paneList = append(paneList, fmt.Sprintf("[%s*]", resID))
			} else {
				paneList = append(paneList, fmt.Sprintf("[%s]", resID))
			}
		}
		b.WriteString(strings.Join(paneList, " "))
		b.WriteString("\n")
	}

	if m.message != "" {
		b.WriteString(fmt.Sprintf("\n%s\n", m.message))
	}

	b.WriteString("\nNote: Terminal shown below ↓\n")

	return b.String()
}

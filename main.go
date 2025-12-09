package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Check if running in tmux
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "Error: must run inside tmux")
		fmt.Fprintln(os.Stderr, "Start tmux first: tmux new-session")
		os.Exit(1)
	}

	// Initialize tmux manager
	mgr, err := NewTmuxManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing tmux: %v\n", err)
		os.Exit(1)
	}

	// Setup the layout
	if err := mgr.Setup(); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up layout: %v\n", err)
		os.Exit(1)
	}

	// Create Bubble Tea model
	model := NewModel(mgr)

	// Run the program
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}

	// Cleanup
	mgr.Cleanup()
}

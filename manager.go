package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// TmuxManager manages the tmux layout for the terminal multiplexer
type TmuxManager struct {
	mainWindow      string            // Main window ID
	tuiPane         string            // TUI pane ID (top)
	bottomPane      string            // Currently attached bottom pane ID
	stashWindow     string            // Stash window ID for resources
	aiStashWindow   string            // Stash window ID for AI chats
	resourcePanes   map[string]string // resourceID -> pane ID (tracks all resource panes)
	aiPanes         map[string]string // aiChatID -> pane ID (tracks all AI chat panes)
	activeResource  string            // Currently active resource ID
	activeAIChat    string            // Currently active AI chat ID
	stashedPanes    []string          // List of pane IDs in stash window
	aiCounter       int               // Counter for AI chat numbering
}

// NewTmuxManager creates a new tmux manager
func NewTmuxManager() (*TmuxManager, error) {
	mgr := &TmuxManager{
		resourcePanes: make(map[string]string),
		aiPanes:       make(map[string]string),
		aiCounter:     0,
	}

	// Get current window
	mainWin, err := tmuxCmd("display-message", "-p", "#{window_id}")
	if err != nil {
		return nil, fmt.Errorf("get window ID: %w", err)
	}
	mgr.mainWindow = mainWin

	// Get current pane (this is the TUI pane)
	tuiPane, err := tmuxCmd("display-message", "-p", "#{pane_id}")
	if err != nil {
		return nil, fmt.Errorf("get pane ID: %w", err)
	}
	mgr.tuiPane = tuiPane

	return mgr, nil
}

// Setup initializes the tmux layout
func (m *TmuxManager) Setup() error {
	// Rename the main window to "main"
	tmuxCmd("rename-window", "-t", m.mainWindow, "main")

	// Count existing panes in main window
	panes, err := m.listPanesInWindow(m.mainWindow)
	if err != nil {
		return fmt.Errorf("list panes: %w", err)
	}

	if len(panes) == 1 {
		// Only TUI pane exists, create initial bottom pane with 50% split
		// Use a wrapper that automatically respawns bash when it exits
		// Clear screen after each respawn for visual feedback
		wrapperCmd := "while true; do bash; clear; done"
		bottomPane, err := tmuxCmd("split-window", "-v", "-p", "50", "-t", m.tuiPane, "-P", "-F", "#{pane_id}", "bash", "-c", wrapperCmd)
		if err != nil {
			return fmt.Errorf("create bottom pane: %w", err)
		}
		m.bottomPane = bottomPane
	} else if len(panes) == 2 {
		// Find the bottom pane (not the TUI pane)
		for _, pane := range panes {
			if pane != m.tuiPane {
				m.bottomPane = pane
				break
			}
		}
	} else {
		return fmt.Errorf("unexpected pane count: %d (expected 1 or 2)", len(panes))
	}

	// Apply even-vertical layout for 50/50 split
	tmuxCmd("select-layout", "-t", m.mainWindow, "even-vertical")

	// Create stash window for resources (hidden from status bar)
	stashWin, err := tmuxCmd("new-window", "-d", "-n", "muxctl-stash", "-P", "-F", "#{window_id}", "bash")
	if err != nil {
		return fmt.Errorf("create stash window: %w", err)
	}
	m.stashWindow = stashWin

	// Hide the stash window from the status bar
	tmuxCmd("set-window-option", "-t", stashWin, "window-status-format", "")
	tmuxCmd("set-window-option", "-t", stashWin, "window-status-current-format", "")

	// Create AI stash window (hidden from status bar)
	aiStashWin, err := tmuxCmd("new-window", "-d", "-n", "muxctl-ai-stash", "-P", "-F", "#{window_id}", "bash")
	if err != nil {
		return fmt.Errorf("create AI stash window: %w", err)
	}
	m.aiStashWindow = aiStashWin

	// Hide the AI stash window from the status bar
	tmuxCmd("set-window-option", "-t", aiStashWin, "window-status-format", "")
	tmuxCmd("set-window-option", "-t", aiStashWin, "window-status-current-format", "")

	// Select the main window and TUI pane
	tmuxCmd("select-window", "-t", m.mainWindow)
	tmuxCmd("select-pane", "-t", m.tuiPane)

	// Initialize status bar - tabs on left, AI chats on right
	m.updateStatusBar()

	// Set status bar background to green (VSCode-style)
	tmuxCmd("set-option", "-g", "status-style", "bg=green,fg=black")

	// Hide window list from status bar
	tmuxCmd("set-option", "-g", "window-status-format", "")
	tmuxCmd("set-option", "-g", "window-status-current-format", "")

	// Bind Alt+Enter to focus TUI pane (escape from bottom pane)
	tmuxCmd("bind-key", "-n", "M-Enter", "select-pane", "-t", m.tuiPane)

	return nil
}

// AttachResourceTerminal switches the bottom pane to show the given resource
func (m *TmuxManager) AttachResourceTerminal(resourceID string) error {
	// Get or create resource pane in stash
	resourcePane, exists := m.resourcePanes[resourceID]
	if !exists {
		// Get the first pane in stash window to split from
		stashPanes, err := m.listPanesInWindow(m.stashWindow)
		if err != nil {
			return fmt.Errorf("list stash panes: %w", err)
		}

		if len(stashPanes) == 0 {
			return fmt.Errorf("stash window has no panes")
		}

		// Create a standalone window for the resource instead of splitting in stash window
		// This avoids tmux split limits entirely - each resource gets its own window
		// Use auto-respawn wrapper so Ctrl+D instantly restarts bash
		// Clear screen after each respawn for visual feedback
		wrapperCmd := fmt.Sprintf("while true; do PS1='[%s] $ ' bash; clear; done", resourceID)
		// Use a descriptive name like "Resource: pod-a" instead of "res-pod-a"
		windowName := fmt.Sprintf("Resource: %s", resourceID)

		winID, err := tmuxCmd("new-window", "-d", "-n", windowName, "-P", "-F", "#{window_id}", "bash", "-c", wrapperCmd)
		if err != nil {
			return fmt.Errorf("create resource window: %w", err)
		}

		// Get the pane ID from the newly created window
		newPane, err := tmuxCmd("display-message", "-t", winID, "-p", "#{pane_id}")
		if err != nil {
			return fmt.Errorf("get pane ID: %w", err)
		}

		// Hide this window from status bar
		tmuxCmd("set-window-option", "-t", winID, "window-status-format", "")
		tmuxCmd("set-window-option", "-t", winID, "window-status-current-format", "")

		m.resourcePanes[resourceID] = newPane
		resourcePane = newPane
	}

	// Verify we have exactly 2 panes in main window
	currentPanes, err := m.listPanesInWindow(m.mainWindow)
	if err != nil {
		return fmt.Errorf("list main window panes: %w", err)
	}

	if len(currentPanes) != 2 {
		return fmt.Errorf("expected 2 panes in main window, found %d", len(currentPanes))
	}

	// Swap the bottom pane in main window with the resource pane in stash
	// Note: swap-pane exchanges positions but pane IDs stay with their original content
	err = tmuxCmd2("swap-pane", "-s", m.bottomPane, "-t", resourcePane)
	if err != nil {
		return fmt.Errorf("swap pane failed: %w", err)
	}

	// After swap: resourcePane is now in main window bottom position
	// Update which pane ID is the current bottom pane
	m.bottomPane = resourcePane

	// Track the active resource
	m.activeResource = resourceID
	// Clear active AI chat since we're in resource mode
	m.activeAIChat = ""

	// Update stashed panes list
	m.updateStashTracking()

	// Ensure layout is correct with consistent sizing (50/50 split)
	tmuxCmd("select-layout", "-t", m.mainWindow, "even-vertical")

	// Update tmux status bar with pane list
	m.updateStatusBar()

	// Switch focus to the bottom pane (the resource terminal)
	tmuxCmd("select-pane", "-t", m.bottomPane)

	return nil
}


// AttachAIChat creates a new AI chat pane or switches to existing one
func (m *TmuxManager) AttachAIChat() error {
	// Find the next available AI chat number (reuse numbers from closed chats)
	aiChatID := ""
	for i := 1; ; i++ {
		candidateID := fmt.Sprintf("ai-%d", i)
		if _, exists := m.aiPanes[candidateID]; !exists {
			aiChatID = candidateID
			break
		}
	}

	// Update counter to track highest number used
	var aiNum int
	fmt.Sscanf(aiChatID, "ai-%d", &aiNum)
	if aiNum > m.aiCounter {
		m.aiCounter = aiNum
	}

	// Create a standalone window for the AI chat instead of splitting in stash window
	// This avoids tmux split limits entirely - each AI chat gets its own window
	// Windows are created detached (-d) and hidden from status bar
	// Use a descriptive name like "AI Chat 1" instead of "ai-ai-1"
	windowName := fmt.Sprintf("AI Chat %d", aiNum)

	// Start with claude directly - no need for bash wrapper or send-keys
	winID, err := tmuxCmd("new-window", "-d", "-n", windowName, "-P", "-F", "#{window_id}", "claude")
	if err != nil {
		return fmt.Errorf("create AI chat window: %w", err)
	}

	// Get the pane ID from the newly created window
	newPane, err := tmuxCmd("display-message", "-t", winID, "-p", "#{pane_id}")
	if err != nil {
		return fmt.Errorf("get pane ID: %w", err)
	}

	// Hide this window from status bar
	tmuxCmd("set-window-option", "-t", winID, "window-status-format", "")
	tmuxCmd("set-window-option", "-t", winID, "window-status-current-format", "")

	// Track the AI pane
	m.aiPanes[aiChatID] = newPane

	// Verify we have exactly 2 panes in main window
	currentPanes, err := m.listPanesInWindow(m.mainWindow)
	if err != nil {
		return fmt.Errorf("list main window panes: %w", err)
	}

	if len(currentPanes) != 2 {
		return fmt.Errorf("expected 2 panes in main window, found %d", len(currentPanes))
	}

	// Swap the bottom pane in main window with the AI chat pane
	err = tmuxCmd2("swap-pane", "-s", m.bottomPane, "-t", newPane)
	if err != nil {
		return fmt.Errorf("swap pane failed: %w", err)
	}

	// After swap: newPane is now in main window bottom position
	m.bottomPane = newPane

	// Track the active AI chat
	m.activeAIChat = aiChatID
	// Clear active resource since we're in AI mode
	m.activeResource = ""

	// Update stashed panes list
	m.updateStashTracking()

	// Ensure layout is correct with consistent sizing (50/50 split)
	tmuxCmd("select-layout", "-t", m.mainWindow, "even-vertical")

	// Update tmux status bar with pane list
	m.updateStatusBar()

	// Switch focus to the bottom pane (the AI chat)
	tmuxCmd("select-pane", "-t", m.bottomPane)

	return nil
}

// ShowAIChooser displays a unified fzf popup to select and swap AI chats or resources
func (m *TmuxManager) ShowAIChooser() {
	// Build lists of both AI chats and resources with their pane IDs
	var aiList []string
	var paneMap []string // Maps "type:id" to pane ID
	for aiID, paneID := range m.aiPanes {
		aiList = append(aiList, "ai:"+aiID)
		paneMap = append(paneMap, fmt.Sprintf("ai:%s:%s", aiID, paneID))
	}
	sort.Strings(aiList)

	var resList []string
	for resID, paneID := range m.resourcePanes {
		resList = append(resList, "res:"+resID)
		paneMap = append(paneMap, fmt.Sprintf("res:%s:%s", resID, paneID))
	}
	sort.Strings(resList)

	if len(aiList) == 0 && len(resList) == 0 {
		return // Nothing to show
	}

	// Combine both lists for display
	allItems := append(aiList, resList...)

	// Create a script with fzf that allows toggling between AI and Resources
	// Ctrl-A shows only AI chats, Ctrl-R shows only resources, Ctrl-T shows all
	script := fmt.Sprintf(`
		# Create temp files - one for display, one for the pane mapping
		tmpfile=$(mktemp)
		mapfile=$(mktemp)
		printf '%%s\n' %s > "$tmpfile"
		printf '%%s\n' %s > "$mapfile"

		# Use fzf with toggle bindings
		selected=$(cat "$tmpfile" | fzf \
			--prompt='Select (^A=AI ^R=Res ^T=All): ' \
			--height=60%% \
			--reverse \
			--border \
			--header='AI Chats & Resources' \
			--bind "ctrl-a:reload(awk /^ai:/ $tmpfile)" \
			--bind "ctrl-r:reload(awk /^res:/ $tmpfile)" \
			--bind "ctrl-t:reload(cat $tmpfile)")

		if [ -n "$selected" ]; then
			type=$(echo "$selected" | cut -d: -f1)
			id=$(echo "$selected" | cut -d: -f2)

			# Look up the pane ID from the map file
			# Map format is "type:id:paneID"
			pane_id=$(grep "^${type}:${id}:" "$mapfile" | cut -d: -f3)

			if [ -n "$pane_id" ]; then
				# Get the current bottom pane in the main window dynamically
				current_bottom=$(tmux list-panes -t main -F '#{pane_id} #{pane_index}' | grep ' 1$' | cut -d' ' -f1)

				# Only swap if the selected pane is not already the bottom pane
				if [ "$pane_id" != "$current_bottom" ]; then
					# Swap the pane with the bottom pane in main window
					tmux swap-pane -s "$current_bottom" -t "$pane_id"
				fi

				# Select the main window
				tmux select-window -t main

				# Focus the bottom pane by position (index 1)
				tmux select-pane -t main.1

				# Output the selected type and id so Go can update state
				echo "$type:$id"
			fi
		fi

		rm -f "$tmpfile" "$mapfile"
	`, strings.Join(allItems, " "), strings.Join(paneMap, " "))

	// Show the popup with the script
	// Note: display-popup with -E doesn't capture output well
	// Instead, write output to a temp file
	tmpfile := fmt.Sprintf("/tmp/muxctl-selector-%d", time.Now().Unix())
	scriptWithOutput := strings.Replace(script, `echo "$type:$id"`, fmt.Sprintf(`echo "$type:$id" > %s`, tmpfile), 1)

	tmuxCmd("display-popup", "-E", "-w", "60%", "-h", "60%", "bash", "-c", scriptWithOutput)

	// Read the output from the temp file
	output, err := os.ReadFile(tmpfile)
	os.Remove(tmpfile) // Clean up

	if err == nil && len(output) > 0 {
		selection := strings.TrimSpace(string(output))
		parts := strings.Split(selection, ":")
		if len(parts) == 2 {
			selectedType := parts[0]
			selectedID := parts[1]

			if selectedType == "ai" {
				m.activeAIChat = selectedID
				m.activeResource = ""
			} else if selectedType == "res" {
				m.activeResource = selectedID
				m.activeAIChat = ""
			}

			// After the swap in the bash script, the selected pane is now at position 1 (bottom)
			// We need to get the actual pane ID at that position
			panes, err := m.listPanesInWindow(m.mainWindow)
			if err == nil && len(panes) >= 2 {
				// The bottom pane is the one that's not the TUI pane
				for _, pane := range panes {
					if pane != m.tuiPane {
						m.bottomPane = pane
						break
					}
				}
			}

			// Update stashed panes tracking
			m.updateStashTracking()

			// Update status bar to reflect the change
			m.updateStatusBar()

			// Ensure the selected pane is focused
			tmuxCmd("select-window", "-t", m.mainWindow)
			tmuxCmd("select-pane", "-t", m.bottomPane)
		}
	}
}

// CloseResourcePane kills the pane for a given resource
func (m *TmuxManager) CloseResourcePane(resourceID string) error {
	// Get the pane ID for this resource
	paneID, exists := m.resourcePanes[resourceID]
	if !exists {
		return fmt.Errorf("resource %s has no pane", resourceID)
	}

	// If this is the active resource, we need to handle it specially
	if resourceID == m.activeResource {
		// Kill the bottom pane
		err := tmuxCmd2("kill-pane", "-t", paneID)
		if err != nil {
			return fmt.Errorf("kill active pane: %w", err)
		}

		// Create a new placeholder bottom pane
		newBottomPane, err := tmuxCmd("split-window", "-v", "-p", "50", "-t", m.tuiPane, "-P", "-F", "#{pane_id}", "bash")
		if err != nil {
			return fmt.Errorf("create replacement pane: %w", err)
		}

		m.bottomPane = newBottomPane
		m.activeResource = ""
		tmuxCmd("select-layout", "-t", m.mainWindow, "even-vertical")
	} else {
		// Resource is in stash, just kill it
		err := tmuxCmd2("kill-pane", "-t", paneID)
		if err != nil {
			return fmt.Errorf("kill stashed pane: %w", err)
		}
	}

	// Remove from tracking
	delete(m.resourcePanes, resourceID)

	// Update stash tracking
	m.updateStashTracking()

	// Update status bar
	m.updateStatusBar()

	return nil
}

// cleanupDeadPanes removes any panes from tracking that no longer exist
func (m *TmuxManager) cleanupDeadPanes() {
	// Get all existing pane IDs
	allPanes, err := tmuxCmd("list-panes", "-a", "-F", "#{pane_id}")
	if err != nil {
		return
	}

	existingPanes := make(map[string]bool)
	for _, paneID := range strings.Split(strings.TrimSpace(allPanes), "\n") {
		if paneID != "" {
			existingPanes[paneID] = true
		}
	}

	// Clean up resource panes that no longer exist
	for resID, paneID := range m.resourcePanes {
		if !existingPanes[paneID] {
			delete(m.resourcePanes, resID)
		}
	}

	// Clean up AI panes that no longer exist
	for aiID, paneID := range m.aiPanes {
		if !existingPanes[paneID] {
			delete(m.aiPanes, aiID)
			// If this was the active AI chat, clear it
			if aiID == m.activeAIChat {
				m.activeAIChat = ""
			}
		}
	}

	// Check if the current bottom pane is dead (e.g., AI chat exited and auto-swapped, or user pressed Ctrl+D)
	if !existingPanes[m.bottomPane] {
		// The bottom pane is dead, check the main window pane count
		mainPanes, err := m.listPanesInWindow(m.mainWindow)
		if err == nil {
			if len(mainPanes) == 1 {
				// Only TUI pane left - the default pane died (user pressed Ctrl+D)
				// Recreate the default bottom pane with auto-respawn wrapper
				wrapperCmd := "while true; do bash; clear; done"
				newBottomPane, err := tmuxCmd("split-window", "-v", "-p", "50", "-t", m.tuiPane, "-P", "-F", "#{pane_id}", "bash", "-c", wrapperCmd)
				if err == nil {
					m.bottomPane = newBottomPane
					m.activeResource = ""
					m.activeAIChat = ""
					tmuxCmd("select-layout", "-t", m.mainWindow, "even-vertical")
				}
			} else if len(mainPanes) == 2 {
				// Two panes exist, find which one is the bottom pane
				for _, paneID := range mainPanes {
					if paneID != m.tuiPane {
						m.bottomPane = paneID
						break
					}
				}
			}
		}
	}
}

// updateStashTracking refreshes the list of panes in the stash window
func (m *TmuxManager) updateStashTracking() {
	panes, err := m.listPanesInWindow(m.stashWindow)
	if err == nil {
		m.stashedPanes = panes
	}
}

// updateStatusBar updates the tmux status bar with clickable pane tabs
func (m *TmuxManager) updateStatusBar() {
	// Clean up any dead panes before updating status
	m.cleanupDeadPanes()

	// Determine which context is active for dimming
	inResourceMode := m.activeResource != ""
	inAIMode := m.activeAIChat != ""

	// Build pane list with clickable elements using status-format syntax
	var tabParts []string

	// Get all resource IDs and sort for consistent display
	var resourceIDs []string
	for resID := range m.resourcePanes {
		resourceIDs = append(resourceIDs, resID)
	}

	// Sort for consistent order
	// Using a simple bubble sort since we have few items
	for i := 0; i < len(resourceIDs); i++ {
		for j := i + 1; j < len(resourceIDs); j++ {
			if resourceIDs[i] > resourceIDs[j] {
				resourceIDs[i], resourceIDs[j] = resourceIDs[j], resourceIDs[i]
			}
		}
	}

	// Create styled tabs with minimal padding
	// Limit to first 10 tabs, but ensure active tab is always visible
	maxTabs := 10

	// Build list of tabs to display
	var displayIDs []string
	if len(resourceIDs) <= maxTabs {
		// All tabs fit, show them all
		displayIDs = resourceIDs
	} else {
		// Too many tabs - show first 9 + active (if not in first 9)
		displayIDs = resourceIDs[:maxTabs-1]

		// Check if active resource is in the displayed list
		activeInList := false
		for _, resID := range displayIDs {
			if resID == m.activeResource {
				activeInList = true
				break
			}
		}

		// If active resource is not in list, add it at the end
		if !activeInList && m.activeResource != "" {
			displayIDs = append(displayIDs, m.activeResource)
		}
	}

	for _, resID := range displayIDs {
		// Format the tab with visual styling
		var tabText string

		if resID == m.activeResource {
			// Active tab: reverse video (inverted colors)
			tabText = fmt.Sprintf(" #[reverse]%s#[noreverse] ", resID)
		} else {
			// Inactive tab: default styling with context-aware dimming
			if inAIMode {
				// Dim resource tabs when AI is active
				tabText = fmt.Sprintf(" #[dim]%s#[nodim] ", resID)
			} else {
				// Normal brightness when resource active or default pane
				tabText = fmt.Sprintf(" %s ", resID)
			}
		}

		tabParts = append(tabParts, tabText)
	}

	// If there are more tabs than displayed, add a count indicator
	if len(resourceIDs) > len(displayIDs) {
		remaining := len(resourceIDs) - len(displayIDs)
		tabParts = append(tabParts, fmt.Sprintf("+%d ", remaining))
	}

	// Create status bar content - tabs are directly adjacent with shared padding
	// Add explicit reset at the beginning to clear any previous state
	statusContent := "#[default]" + strings.Join(tabParts, "")

	// Calculate required length for status-left (add buffer for formatting codes)
	statusLeftLen := len(statusContent) + 50
	if statusLeftLen < 100 {
		statusLeftLen = 100
	}

	// Set tabs on the left side
	tmuxCmd("set-option", "-g", "status-left-length", fmt.Sprintf("%d", statusLeftLen))
	tmuxCmd("set-option", "-g", "status-left", statusContent)

	// Build AI chat list for the right side
	var aiParts []string
	var aiChatIDs []string
	for aiID := range m.aiPanes {
		aiChatIDs = append(aiChatIDs, aiID)
	}

	// Sort AI chats
	for i := 0; i < len(aiChatIDs); i++ {
		for j := i + 1; j < len(aiChatIDs); j++ {
			if aiChatIDs[i] > aiChatIDs[j] {
				aiChatIDs[i], aiChatIDs[j] = aiChatIDs[j], aiChatIDs[i]
			}
		}
	}

	// Create AI chat tabs
	// Limit to first 10 tabs, but ensure active AI chat is always visible
	maxAITabs := 10

	// Build list of AI tabs to display
	var displayAIIDs []string
	if len(aiChatIDs) <= maxAITabs {
		// All AI tabs fit, show them all
		displayAIIDs = aiChatIDs
	} else {
		// Too many AI tabs - show first 9 + active (if not in first 9)
		displayAIIDs = aiChatIDs[:maxAITabs-1]

		// Check if active AI chat is in the displayed list
		activeAIInList := false
		for _, aiID := range displayAIIDs {
			if aiID == m.activeAIChat {
				activeAIInList = true
				break
			}
		}

		// If active AI chat is not in list, add it at the end
		if !activeAIInList && m.activeAIChat != "" {
			displayAIIDs = append(displayAIIDs, m.activeAIChat)
		}
	}

	// Add "ai" prefix before the tab numbers
	if len(displayAIIDs) > 0 {
		aiParts = append(aiParts, "ai")
	}

	for _, aiID := range displayAIIDs {
		// Extract just the number from "ai-N"
		aiNum := strings.TrimPrefix(aiID, "ai-")

		// Format the tab with visual styling
		var aiTab string

		if aiID == m.activeAIChat {
			// Active tab: reverse video (inverted colors)
			aiTab = fmt.Sprintf(" #[reverse]%s#[noreverse]", aiNum)
		} else {
			// Inactive tab: default styling with context-aware dimming
			if inResourceMode {
				// Dim AI tabs when resource is active
				aiTab = fmt.Sprintf(" #[dim]%s#[nodim]", aiNum)
			} else {
				// Normal brightness when AI active or default pane
				aiTab = fmt.Sprintf(" %s", aiNum)
			}
		}
		aiParts = append(aiParts, aiTab)
	}

	// If there are more AI chat tabs than displayed, add a count indicator
	if len(aiChatIDs) > len(displayAIIDs) {
		remaining := len(aiChatIDs) - len(displayAIIDs)
		aiParts = append(aiParts, fmt.Sprintf(" +%d", remaining))
	}

	// Add explicit reset at the beginning to clear any previous state
	aiStatusContent := "#[default]" + strings.Join(aiParts, "") + " "

	// Calculate required length for status-right (add buffer for formatting codes)
	statusRightLen := len(aiStatusContent) + 50
	if statusRightLen < 100 {
		statusRightLen = 100
	}

	// Set AI chats on the right side
	tmuxCmd("set-option", "-g", "status-right-length", fmt.Sprintf("%d", statusRightLen))
	tmuxCmd("set-option", "-g", "status-right", aiStatusContent)
}

// GetActiveResource returns the currently active resource ID
func (m *TmuxManager) GetActiveResource() string {
	return m.activeResource
}

// GetStashedResources returns a list of resource IDs that are in the stash
func (m *TmuxManager) GetStashedResources() []string {
	var stashed []string
	for resID, paneID := range m.resourcePanes {
		if resID != m.activeResource {
			// Check if this pane is in stash
			for _, stashPaneID := range m.stashedPanes {
				if paneID == stashPaneID {
					stashed = append(stashed, resID)
					break
				}
			}
		}
	}
	return stashed
}

// GetPaneInfo returns detailed info about pane locations
func (m *TmuxManager) GetPaneInfo() map[string]string {
	info := make(map[string]string)

	for resID, paneID := range m.resourcePanes {
		if resID == m.activeResource {
			info[resID] = fmt.Sprintf("%s (active in main window)", paneID)
		} else {
			// Check if in stash
			inStash := false
			for _, stashPaneID := range m.stashedPanes {
				if paneID == stashPaneID {
					inStash = true
					break
				}
			}
			if inStash {
				info[resID] = fmt.Sprintf("%s (stashed)", paneID)
			} else {
				info[resID] = fmt.Sprintf("%s (unknown location)", paneID)
			}
		}
	}

	return info
}

// listPanesInWindow returns pane IDs in a window
func (m *TmuxManager) listPanesInWindow(windowID string) ([]string, error) {
	output, err := tmuxCmd("list-panes", "-t", windowID, "-F", "#{pane_id}")
	if err != nil {
		return nil, err
	}

	if output == "" {
		return []string{}, nil
	}

	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

// Cleanup removes the stash windows and resets status bar, then kills the tmux session
func (m *TmuxManager) Cleanup() {
	if m.stashWindow != "" {
		tmuxCmd("kill-window", "-t", m.stashWindow)
	}
	if m.aiStashWindow != "" {
		tmuxCmd("kill-window", "-t", m.aiStashWindow)
	}
	// Restore default status bar settings
	tmuxCmd("set-option", "-g", "status-left", "[#{session_name}] ")
	tmuxCmd("set-option", "-g", "status-right", "#{?window_bigger,[#{window_offset_x}#,#{window_offset_y}] ,}\"#{=21:pane_title}\" %H:%M %d-%b-%y")
	tmuxCmd("set-option", "-g", "window-status-format", "#I:#W#F")
	tmuxCmd("set-option", "-g", "window-status-current-format", "#I:#W#F")

	// Unbind Alt+Enter
	tmuxCmd("unbind-key", "-n", "M-Enter")

	// Kill the current tmux session
	tmuxCmd("kill-session")
}

// tmuxCmd runs a tmux command and returns stdout
func tmuxCmd(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// tmuxCmd2 runs a tmux command and only returns error (doesn't capture output)
func tmuxCmd2(args ...string) error {
	cmd := exec.Command("tmux", args...)
	return cmd.Run()
}

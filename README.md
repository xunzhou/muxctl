# muxctl - Terminal Multiplexer TUI

A terminal user interface (TUI) for managing multiple shell sessions and AI chats in tmux, with a VSCode-style tab experience.

## Overview

`muxctl` provides a clean interface for managing multiple terminal sessions (resources) and AI chat sessions within tmux. It features:

- **Split Layout**: TUI panel on top, active terminal on bottom
- **Smart Tab Switching**: Instant switching between resources and AI chats
- **Persistent Sessions**: Each resource/AI chat maintains its own shell history and state
- **Visual Status Bar**: Green status bar with tabs showing active sessions
- **Context-Aware UI**: Automatic dimming of inactive context tabs
- **Fuzzy Search**: Quick popup selector with `Shift+A` for finding sessions

## Prerequisites

- **tmux** (must be running)
- **Go 1.21+** for building
- **fzf** (optional, for the popup selector)
- **claude** CLI (optional, for AI chat feature)

## Quick Start

```bash
# Build
make build

# Run (must be inside tmux)
./muxctl

# Or build and run
make run

# Install to ~/bin
make install
```

## Keybindings

### Navigation
- `↑` / `k` - Move selection up
- `↓` / `j` - Move selection down
- `ENTER` - Activate selected resource terminal
- `Alt+Enter` - Return to TUI from terminal

### Features
- `a` - Launch new AI chat
- `A` (Shift+A) - Open AI/Resource selector popup
  - `Ctrl+A` - Filter AI chats only
  - `Ctrl+R` - Filter resources only
  - `Ctrl+T` - Show all (toggle back)
- `x` - Close selected resource pane
- `q` - Quit (with confirmation)
- `Ctrl+C` - Force quit (no confirmation)

## How It Works

### Layout

```
╔═══════════════════════════════════╗
║      Terminal Multiplexer         ║  ← TUI Panel
╠═══════════════════════════════════╣
║ Resources:                        ║
║ ► pod-a                          ●║  ● = Active (visible)
║   pod-b                          ○║  ○ = Stashed (background)
║   pod-c                           ║
║   service-x                       ║
║   service-y                       ║
╚═══════════════════════════════════╝

┌───────────────────────────────────┐
│ [pod-a] $ _                       │  ← Active Terminal
│                                   │
└───────────────────────────────────┘

pod-a pod-b                 ai 1 2 3  ← Status Bar Tabs
```

### Session Management

When you activate a resource or AI chat:
1. A dedicated shell session is created (if new)
2. The session is swapped into the visible terminal pane
3. Previous session is stashed but keeps its state
4. Status bar updates to show active tabs

All sessions persist until closed, maintaining:
- Command history
- Working directory
- Running processes
- Environment variables

### Popup Selector

Press `Shift+A` to open a fuzzy search popup showing all resources and AI chats:
- Type to filter
- Use `Ctrl+A` / `Ctrl+R` / `Ctrl+T` to toggle filters
- Press Enter to switch to selected session
- Press Esc to cancel

## Features

### Resource Management
- Pre-defined resource list (pod-a, pod-b, pod-c, service-x, service-y)
- Create terminal session for any resource on-demand
- Each resource gets its own persistent bash session
- Custom prompt shows resource name: `[pod-a] $`

### AI Chat Integration
- Launch new AI chat sessions with `a` key
- Numbered AI chats: ai-1, ai-2, ai-3, etc.
- Compact status bar display: `ai 1 2 3`
- Uses `claude` CLI directly

### Visual Indicators
- **TUI List**: `►` shows selection, `●` shows active, `○` shows stashed
- **Status Bar**: Active tab highlighted, inactive tabs dimmed by context
- **Pane List**: Shows all open panes at bottom of TUI

## Architecture

### Core Components

- **`main.go`** - Entry point, initializes tmux manager and Bubble Tea
- **`manager.go`** - Tmux session management, pane swapping, status bar
- **`model.go`** - Bubble Tea model, keyboard handling, UI rendering

### Tmux Integration

Uses tmux features:
- **Pane Swapping**: Exchange panes without losing state
- **Standalone Windows**: Each session in its own hidden window
- **Status Bar Customization**: Dynamic tab display
- **Keybinding**: `Alt+Enter` to return to TUI

## Development

```bash
# Download dependencies
make deps

# Build
make build

# Clean
make clean

# Show all make targets
make help
```

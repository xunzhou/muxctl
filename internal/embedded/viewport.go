package embedded

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xunzhou/muxctl/internal/debug"
	"github.com/xunzhou/muxctl/internal/pty"
)

// TerminalViewport is a Bubble Tea component that renders tmux PTY output.
// It implements the spec's coalescing (8ms) and throttling (33ms) strategies.
type TerminalViewport struct {
	pty        *pty.PTY
	controller *TmuxController
	paneID     PaneID
	width      int
	height     int

	// Buffering and rendering
	buffer       bytes.Buffer
	bufferMu     sync.Mutex
	dirty        bool
	lastRedraw   time.Time
	redrawTicker *time.Ticker

	// Coalescing
	coalesceTimer    *time.Timer
	coalesceDuration time.Duration
	pendingData      []byte
	pendingMu        sync.Mutex

	// Channels
	program *tea.Program
}

// PtyOutputMsg is sent when PTY output is available.
type PtyOutputMsg struct {
	Data []byte
	Err  error
}

// NewTerminalViewport creates a viewport for the given PTY.
func NewTerminalViewport(ptyInstance *pty.PTY, width, height int) *TerminalViewport {
	debug.Log("TerminalViewport.New: width=%d height=%d", width, height)

	return &TerminalViewport{
		pty:              ptyInstance,
		width:            width,
		height:           height,
		coalesceDuration: 8 * time.Millisecond,
		lastRedraw:       time.Now(),
	}
}

// SetProgram attaches the Bubble Tea program for sending messages.
func (v *TerminalViewport) SetProgram(program *tea.Program) {
	v.program = program
}

// Start initiates the PTY read loop and coalescing loop.
// Returns a tea.Cmd that can be used in Bubble Tea's Init().
func (v *TerminalViewport) Start() tea.Cmd {
	debug.Log("TerminalViewport.Start: starting read and coalesce loops")

	// Start PTY read loop
	v.pty.StartReadLoop()

	// Start coalescing loop
	go v.coalesceLoop()

	// Return a command that listens for PTY output/errors
	return func() tea.Msg {
		select {
		case data := <-v.pty.OutputChan():
			return PtyOutputMsg{Data: data}
		case err := <-v.pty.ErrorChan():
			return PtyOutputMsg{Err: err}
		}
	}
}

// coalesceLoop aggregates PTY output chunks over 8ms intervals before sending to Bubble Tea.
// This reduces the number of Update() calls and improves performance.
func (v *TerminalViewport) coalesceLoop() {
	ticker := time.NewTicker(v.coalesceDuration)
	defer ticker.Stop()

	for {
		select {
		case data := <-v.pty.OutputChan():
			// Accumulate data
			v.pendingMu.Lock()
			v.pendingData = append(v.pendingData, data...)
			v.pendingMu.Unlock()

		case <-ticker.C:
			// Flush pending data to Bubble Tea
			v.pendingMu.Lock()
			if len(v.pendingData) > 0 && v.program != nil {
				// Make a copy to avoid race
				data := make([]byte, len(v.pendingData))
				copy(data, v.pendingData)
				v.pendingData = nil
				v.pendingMu.Unlock()

				// Send to Bubble Tea
				v.program.Send(PtyOutputMsg{Data: data})
			} else {
				v.pendingMu.Unlock()
			}

		case err := <-v.pty.ErrorChan():
			// Forward errors immediately
			if v.program != nil {
				v.program.Send(PtyOutputMsg{Err: err})
			}
			return
		}
	}
}

// Update handles Bubble Tea messages.
// Implements tea.Model.Update() pattern.
func (v *TerminalViewport) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case PtyOutputMsg:
		if msg.Err != nil {
			debug.Log("TerminalViewport.Update: error: %v", msg.Err)
			// Handle PTY error (e.g., show "session terminated" message)
			return v, nil
		}

		// Append to buffer
		v.bufferMu.Lock()

		// Check if the data contains a clear screen sequence
		// If so, reset the buffer to match tmux's cleared display
		// Also strip the clear sequence from data to prevent it from leaking to TUI
		dataToWrite := msg.Data
		if containsClearSequence(msg.Data) {
			debug.Log("TerminalViewport.Update: detected clear sequence, resetting buffer and stripping sequence")
			v.buffer.Reset()
			// Strip clear sequences from the data before writing to buffer
			// This prevents the escape sequence from leaking to the main TUI
			dataToWrite = bytes.ReplaceAll(dataToWrite, []byte("\x1b[2J"), []byte(""))
			dataToWrite = bytes.ReplaceAll(dataToWrite, []byte("\x1b[3J"), []byte(""))
			dataToWrite = bytes.ReplaceAll(dataToWrite, []byte("\x1b[H"), []byte(""))
		}

		v.buffer.Write(dataToWrite)

		// Limit buffer size to prevent unbounded growth
		// Keep only last 10000 bytes (~100 lines of output)
		if v.buffer.Len() > 10000 {
			// Keep last 10000 bytes
			content := v.buffer.Bytes()
			v.buffer.Reset()
			v.buffer.Write(content[len(content)-10000:])
		}

		v.dirty = true
		v.bufferMu.Unlock()

		// Throttle redraws: only redraw if 33ms has passed since last redraw
		if time.Since(v.lastRedraw) >= 33*time.Millisecond {
			return v, v.scheduleRedraw()
		}

		return v, nil

	case tea.WindowSizeMsg:
		v.Resize(msg.Width, msg.Height)
		return v, nil
	}

	return v, nil
}

// scheduleRedraw returns a command that triggers a redraw.
func (v *TerminalViewport) scheduleRedraw() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(33 * time.Millisecond)
		return redrawMsg{}
	}
}

type redrawMsg struct{}

// containsClearSequence checks for common terminal clear sequences.
// Detects escape sequences like \x1b[2J (clear screen), \x1b[3J (clear scrollback).
func containsClearSequence(data []byte) bool {
	// \x1b[2J = clear entire screen (CSI 2 J)
	// \x1b[3J = clear scrollback buffer (CSI 3 J)
	// \x1b[H\x1b[2J = clear and home cursor (common combination)
	return bytes.Contains(data, []byte("\x1b[2J")) ||
		bytes.Contains(data, []byte("\x1b[3J"))
}

// truncateString truncates a string to maxLen characters for debug output.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stripAnsiEscapes removes dangerous ANSI escape sequences while preserving colors.
// This prevents escape codes from leaking into the main TUI and affecting other components.
// KEEPS: SGR (color/style) sequences that end with 'm'
// STRIPS: Cursor movement, clear screen, title changes, and other control sequences
func stripAnsiEscapes(content string) string {
	var result strings.Builder
	result.Grow(len(content))

	i := 0
	for i < len(content) {
		if content[i] == '\x1b' && i+1 < len(content) {
			// ESC found, check what follows
			switch content[i+1] {
			case '[': // CSI sequence (colors, cursor movement, clear, etc.)
				// Parse the full sequence to determine if it's a color code
				start := i
				i += 2
				// Skip parameter bytes (digits, semicolons)
				for i < len(content) && ((content[i] >= '0' && content[i] <= '9') || content[i] == ';') {
					i++
				}
				// Check the command byte
				if i < len(content) {
					cmd := content[i]
					i++ // Skip command byte

					// Keep SGR sequences (colors/styles) that end with 'm'
					// Strip everything else (cursor movement, clear, etc.)
					if cmd == 'm' {
						// This is a color/style sequence, keep it
						result.WriteString(content[start:i])
					}
					// Otherwise skip (cursor movement, clear, etc.)
				}
			case ']': // OSC sequence (e.g., terminal title) - always strip
				i += 2
				for i < len(content) {
					if content[i] == '\x07' {
						i++
						break
					}
					if content[i] == '\x1b' && i+1 < len(content) && content[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default:
				// Other escape sequences, skip 2 characters
				i += 2
			}
		} else {
			// Regular character, keep it
			result.WriteByte(content[i])
			i++
		}
	}

	return result.String()
}

// View renders the current buffer to a string.
// Implements tea.Model.View().
func (v *TerminalViewport) View() string {
	v.dirty = false
	v.lastRedraw = time.Now()

	// If we have a controller, use capture-pane to get clean rendered output
	if v.controller != nil && v.paneID.TmuxID != "" {
		debug.Log("TerminalViewport.View: calling capture-pane for pane=%s height=%d", v.paneID.TmuxID, v.height)
		content, err := v.controller.CapturePane(v.paneID, CaptureOptions{
			// Don't specify StartLine/EndLine to capture the current visible screen
			// Using negative offsets only works if there's enough scrollback history
			StripEscapes: true,
		})
		if err == nil && content != "" {
			// Debug: log raw content before stripping
			if len(content) < 200 {
				debug.Log("TerminalViewport.View: raw capture-pane content: %q", content)
			} else {
				debug.Log("TerminalViewport.View: raw capture-pane first 200 chars: %q", content[:200])
			}

			// Strip ALL ANSI escape sequences to prevent them from leaking to the main TUI
			// This is critical because any escape sequence (clear, cursor movement, etc.)
			// would affect the entire TUI screen, not just our viewport
			cleanContent := stripAnsiEscapes(content)
			lines := strings.Split(cleanContent, "\n")
			debug.Log("TerminalViewport.View: capture-pane returned %d lines, stripped to %d chars", len(lines), len(cleanContent))

			// Debug: show first 100 chars of clean content
			if len(cleanContent) < 100 {
				debug.Log("TerminalViewport.View: clean content: %q", cleanContent)
			} else {
				debug.Log("TerminalViewport.View: clean first 100 chars: %q", cleanContent[:100])
			}

			return cleanContent
		}
		// Fall through to buffer on error
		debug.Log("TerminalViewport.View: capture-pane failed: %v", err)
	} else {
		debug.Log("TerminalViewport.View: no controller (ctrl=%v) or paneID (id=%q)", v.controller != nil, v.paneID.TmuxID)
	}

	// Fallback: use buffered PTY output
	v.bufferMu.Lock()
	defer v.bufferMu.Unlock()

	// Get raw buffer content
	content := v.buffer.String()

	// Truncate to height if configured
	if v.height > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > v.height {
			// Take last N lines (most recent output)
			lines = lines[len(lines)-v.height:]
		}
		content = strings.Join(lines, "\n")
	}

	return content
}

// HandleKey processes keyboard input and forwards to PTY in Terminal mode.
func (v *TerminalViewport) HandleKey(msg tea.KeyMsg) {
	debug.Log("TerminalViewport.HandleKey: key=%s", msg.String())

	// Convert Bubble Tea key to bytes and send to PTY
	// This is a simplified implementation; a complete version would handle
	// special keys, modifiers, etc.

	var data []byte

	switch msg.Type {
	case tea.KeyEnter:
		data = []byte("\r")
	case tea.KeyBackspace:
		data = []byte("\x7f")
	case tea.KeyTab:
		data = []byte("\t")
	case tea.KeySpace:
		data = []byte(" ")
	case tea.KeyEsc:
		data = []byte("\x1b")
	case tea.KeyUp:
		data = []byte("\x1b[A")
	case tea.KeyDown:
		data = []byte("\x1b[B")
	case tea.KeyRight:
		data = []byte("\x1b[C")
	case tea.KeyLeft:
		data = []byte("\x1b[D")
	case tea.KeyCtrlC:
		data = []byte("\x03")
	case tea.KeyCtrlD:
		data = []byte("\x04")
	case tea.KeyCtrlL:
		data = []byte("\x0c")
	case tea.KeyRunes:
		// Regular character input
		data = []byte(string(msg.Runes))
	default:
		// Ignore unknown keys
		return
	}

	if len(data) > 0 {
		v.pty.Write(data)
	}
}

// Resize changes the viewport and PTY dimensions.
func (v *TerminalViewport) Resize(width, height int) {
	debug.Log("TerminalViewport.Resize: %dx%d -> %dx%d", v.width, v.height, width, height)

	v.width = width
	v.height = height

	// Resize PTY (sends TIOCSWINSZ to tmux)
	if err := v.pty.Resize(height, width); err != nil {
		debug.Log("TerminalViewport.Resize: failed to resize PTY: %v", err)
	}
}

// CapturePane captures the current pane content via tmux capture-pane command.
// This is a convenience wrapper around TmuxController.CapturePane().
func (v *TerminalViewport) CapturePane(opts CaptureOptions) (string, error) {
	// This method will be implemented to use the TmuxController
	// For now, return a placeholder
	return "", fmt.Errorf("not implemented yet")
}

// SetTargetPane updates the pane that the viewport captures and forwards input to.
// This should be called when the active tmux window/pane changes (e.g., context switch).
func (v *TerminalViewport) SetTargetPane(pane PaneID) {
	debug.Log("TerminalViewport.SetTargetPane: pane=%s", pane.TmuxID)
	v.paneID = pane

	// Drop any buffered content from the previous pane so we don't render stale output.
	v.bufferMu.Lock()
	v.buffer.Reset()
	v.dirty = true
	v.bufferMu.Unlock()
}

// GetSize returns the current viewport dimensions.
func (v *TerminalViewport) GetSize() (width, height int) {
	return v.width, v.height
}

// Init implements tea.Model.Init().
func (v *TerminalViewport) Init() tea.Cmd {
	return v.Start()
}

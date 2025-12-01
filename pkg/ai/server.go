package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"

	intai "github.com/xunzhou/muxctl/internal/ai"
	intctx "github.com/xunzhou/muxctl/internal/context"
	"github.com/xunzhou/muxctl/internal/debug"
	"github.com/xunzhou/muxctl/internal/tmux"
)

// Server handles AI requests over a Unix socket.
type Server struct {
	session    string
	socketPath string
	listener   net.Listener
	tmuxCtrl   *tmux.TmuxController
	engine     *intai.Engine

	mu       sync.Mutex
	running  bool
	shutdown chan struct{}
}

// NewServer creates a new AI socket server.
func NewServer(session string, tmuxCtrl *tmux.TmuxController) (*Server, error) {
	// Load AI config
	cfg, err := intai.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load AI config: %w", err)
	}

	if !cfg.IsEnabled() {
		return nil, fmt.Errorf("AI features are disabled (provider: none)")
	}

	engine, err := intai.NewEngine(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create AI engine: %w", err)
	}

	return &Server{
		session:    session,
		socketPath: SocketPath(session),
		tmuxCtrl:   tmuxCtrl,
		engine:     engine,
		shutdown:   make(chan struct{}),
	}, nil
}

// Start begins listening for requests.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}

	// Remove existing socket if present
	os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", s.socketPath, err)
	}

	s.listener = listener
	s.running = true
	s.mu.Unlock()

	debug.Log("AI server listening on %s", s.socketPath)

	go s.acceptLoop()

	return nil
}

// Stop shuts down the server.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	close(s.shutdown)
	s.listener.Close()
	os.Remove(s.socketPath)
	s.running = false

	debug.Log("AI server stopped")
}

// SocketPath returns the socket path for this server.
func (s *Server) GetSocketPath() string {
	return s.socketPath
}

// acceptLoop handles incoming connections.
func (s *Server) acceptLoop() {
	for {
		select {
		case <-s.shutdown:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.shutdown:
				return
			default:
				debug.Log("AI server accept error: %v", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

// handleConnection processes a single request.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Decode request
	var req Request
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&req); err != nil {
		s.sendResponse(conn, Response{
			Success: false,
			Error:   fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	debug.Log("AI server received request: action=%s target=%s source=%s",
		req.Action, req.TargetPane, req.SourcePane)

	// Validate request
	if err := req.Validate(); err != nil {
		s.sendResponse(conn, Response{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Process request
	if err := s.processRequest(req); err != nil {
		s.sendResponse(conn, Response{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	s.sendResponse(conn, Response{Success: true})
}

// processRequest handles the AI action and outputs to target pane.
func (s *Server) processRequest(req Request) error {
	// Get content from source pane if not provided
	paneContent := req.Context.PaneContent
	if paneContent == "" && req.SourcePane != "" {
		role, err := tmux.ParseRole(req.SourcePane)
		if err != nil {
			return fmt.Errorf("invalid source_pane: %w", err)
		}

		maxLines := req.Options.MaxLines
		if maxLines == 0 {
			maxLines = 300
		}

		if req.Options.LastCommand {
			// Last command mode
			cmdCapture, err := s.tmuxCtrl.CaptureLastCommand(role)
			if err != nil {
				return fmt.Errorf("failed to capture last command: %w", err)
			}
			// Build content from command capture
			paneContent = fmt.Sprintf("Command: %s\nExit code: %s\nOutput:\n%s",
				cmdCapture.Command, cmdCapture.ExitCode, cmdCapture.Output)
		} else {
			// Standard capture
			content, err := s.tmuxCtrl.CapturePane(role, maxLines)
			if err != nil {
				return fmt.Errorf("failed to capture pane: %w", err)
			}
			paneContent = content
		}
	}

	// Build AI input
	input := intai.ActionInput{
		PaneContent: paneContent,
		Context: intctx.Context{
			Cluster:     req.Context.Cluster,
			Namespace:   req.Context.Namespace,
			KubeContext: req.Context.KubeContext,
		},
		MaxLines: req.Options.MaxLines,
	}

	// Add alert/resource context to metadata if provided
	if req.Context.SelectedAlert != nil {
		if input.Context.Metadata == nil {
			input.Context.Metadata = make(map[string]string)
		}
		input.Context.Metadata["alert_name"] = req.Context.SelectedAlert.Name
		if req.Context.SelectedAlert.Severity != "" {
			input.Context.Metadata["alert_severity"] = req.Context.SelectedAlert.Severity
		}
	}
	if req.Context.SelectedResource != nil {
		if input.Context.Metadata == nil {
			input.Context.Metadata = make(map[string]string)
		}
		input.Context.Metadata["resource_kind"] = req.Context.SelectedResource.Kind
		input.Context.Metadata["resource_name"] = req.Context.SelectedResource.Name
	}

	// Run AI action
	action := intai.ActionType(req.Action)
	result, err := s.engine.Run(context.Background(), action, input)
	if err != nil {
		return fmt.Errorf("AI action failed: %w", err)
	}

	// Output to target pane
	targetRole, err := tmux.ParseRole(req.TargetPane)
	if err != nil {
		return fmt.Errorf("invalid target_pane: %w", err)
	}

	// Clear target pane and display result
	s.tmuxCtrl.ClearPane(targetRole)

	// Use echo to display result (handles multiline)
	// We'll send the content line by line to avoid issues
	lines := splitLines(result.Content)
	for _, line := range lines {
		if line == "" {
			s.tmuxCtrl.SendKeys(targetRole, "Enter")
		} else {
			// Echo the line
			s.tmuxCtrl.RunInPane(targetRole, []string{"echo", line}, nil)
		}
	}

	return nil
}

// sendResponse writes a JSON response to the connection.
func (s *Server) sendResponse(conn net.Conn, resp Response) {
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(resp); err != nil {
		debug.Log("AI server response error: %v", err)
	}
}

// splitLines splits content into lines, preserving empty lines.
func splitLines(content string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

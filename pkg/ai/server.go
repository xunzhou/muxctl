package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

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
	aiConfig   intai.Config
	convMgr    *ConversationManager

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
		aiConfig:   cfg,
		convMgr:    NewConversationManager(),
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

	// Peek at JSON to determine request type
	// Try to decode as conversation request first
	decoder := json.NewDecoder(conn)

	// Decode into a generic map to inspect the request
	var rawReq map[string]interface{}
	if err := decoder.Decode(&rawReq); err != nil {
		s.sendResponse(conn, Response{
			Success: false,
			Error:   fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	// Check if this is a conversation request (has "action" field with conversation actions)
	if action, ok := rawReq["action"].(string); ok {
		switch ConversationAction(action) {
		case ConvActionStart, ConvActionSend, ConvActionEnd, ConvActionResize, ConvActionCompact:
			// Re-marshal and decode as ConversationRequest
			data, _ := json.Marshal(rawReq)
			var convReq ConversationRequest
			if err := json.Unmarshal(data, &convReq); err != nil {
				s.sendConvResponse(conn, ConversationResponse{
					Success: false,
					Error:   fmt.Sprintf("invalid conversation request: %v", err),
				})
				return
			}

			debug.Log("AI server received conversation request: action=%s conv_id=%s",
				convReq.Action, convReq.ConversationID)

			// Validate and process conversation request
			if err := convReq.Validate(); err != nil {
				s.sendConvResponse(conn, ConversationResponse{
					Success: false,
					Error:   err.Error(),
				})
				return
			}

			resp := s.processConversationRequest(convReq)
			s.sendConvResponse(conn, resp)
			return
		}
	}

	// Otherwise, process as regular Request
	data, _ := json.Marshal(rawReq)
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
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

// processConversationRequest handles conversation-specific actions.
func (s *Server) processConversationRequest(req ConversationRequest) ConversationResponse {
	switch req.Action {
	case ConvActionStart:
		return s.handleConversationStart(req)
	case ConvActionSend:
		return s.handleConversationSend(req)
	case ConvActionEnd:
		return s.handleConversationEnd(req)
	case ConvActionResize:
		return s.handleConversationResize(req)
	case ConvActionCompact:
		return s.handleConversationCompact(req)
	default:
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("unsupported conversation action: %s", req.Action),
		}
	}
}

// handleConversationStart initiates a new conversation.
func (s *Server) handleConversationStart(req ConversationRequest) ConversationResponse {
	// Create conversation context
	ctx := ConversationContext{
		AlertFingerprint: req.Context.AlertFingerprint,
		Cluster:          req.Context.Cluster,
		Namespace:        req.Context.Namespace,
		InitialSummary:   req.Context.InitialSummary,
		Metadata:         make(map[string]string),
	}

	// Copy metadata
	for k, v := range req.Context.Metadata {
		if str, ok := v.(string); ok {
			ctx.Metadata[k] = str
		}
	}

	// Start conversation
	conv, err := s.convMgr.Start(ctx)
	if err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to start conversation: %v", err),
		}
	}

	// Resize right pane if requested
	if req.Options.ExpandWidth > 0 {
		if err := s.tmuxCtrl.ResizePane(tmux.RoleRight, req.Options.ExpandWidth); err != nil {
			debug.Log("Failed to resize pane: %v", err)
			// Non-fatal, continue
		}
	}

	// Focus right pane for conversation
	s.tmuxCtrl.FocusPane(tmux.RoleRight)

	// Clear right pane
	s.tmuxCtrl.ClearPane(tmux.RoleRight)

	debug.Log("Started conversation: id=%s cluster=%s alert=%s",
		conv.ID, ctx.Cluster, ctx.AlertFingerprint[:8])

	return ConversationResponse{
		Success:        true,
		ConversationID: conv.ID,
		TurnCount:      0,
		State:          string(conv.State),
	}
}

// handleConversationSend sends a message and gets AI response.
func (s *Server) handleConversationSend(req ConversationRequest) ConversationResponse {
	conv, err := s.convMgr.Get(req.ConversationID)
	if err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("conversation not found: %v", err),
		}
	}

	// Add user message to conversation
	if err := s.convMgr.AddTurn(conv.ID, "user", req.Message); err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to add user message: %v", err),
		}
	}

	// Get AI response by calling Chat with conversation messages
	messages := conv.GetMessages()

	// Create AI client for this request
	aiClient, err := intai.NewClient(s.aiConfig)
	if err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create AI client: %v", err),
		}
	}

	response, err := aiClient.Chat(context.Background(), convertMessages(messages))
	if err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("AI request failed: %v", err),
		}
	}

	// Add assistant response to conversation
	if err := s.convMgr.AddTurn(conv.ID, "assistant", response); err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to add assistant message: %v", err),
		}
	}

	// Display response in right pane
	s.displayConversationInPane(conv)

	debug.Log("Conversation turn completed: id=%s turns=%d", conv.ID, conv.TurnCount())

	return ConversationResponse{
		Success:        true,
		ConversationID: conv.ID,
		Message:        response,
		TurnCount:      conv.TurnCount(),
		State:          string(conv.State),
	}
}

// handleConversationEnd terminates a conversation.
func (s *Server) handleConversationEnd(req ConversationRequest) ConversationResponse {
	conv, err := s.convMgr.End(req.ConversationID)
	if err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to end conversation: %v", err),
		}
	}

	// TODO: Persist conversation to disk for future resume capability
	// For now, just delete from memory after a delay
	// (In future: save to ~/.config/muxctl/conversations/)

	debug.Log("Ended conversation: id=%s turns=%d", conv.ID, conv.TurnCount())

	// Restore right pane to default size (40%)
	s.tmuxCtrl.ResizePane(tmux.RoleRight, 40)

	return ConversationResponse{
		Success:        true,
		ConversationID: conv.ID,
		TurnCount:      conv.TurnCount(),
		State:          string(conv.State),
	}
}

// handleConversationResize changes the conversation pane size.
func (s *Server) handleConversationResize(req ConversationRequest) ConversationResponse {
	width := req.Options.ExpandWidth
	if width == 0 {
		width = 60 // Default expansion
	}

	if err := s.tmuxCtrl.ResizePane(tmux.RoleRight, width); err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to resize pane: %v", err),
		}
	}

	return ConversationResponse{
		Success:        true,
		ConversationID: req.ConversationID,
	}
}

// displayConversationInPane shows the full conversation history in the right pane.
func (s *Server) displayConversationInPane(conv *Conversation) {
	s.tmuxCtrl.ClearPane(tmux.RoleRight)

	for _, turn := range conv.Turns {
		// Format: "User: message" or "Assistant: message"
		prefix := "Assistant"
		if turn.Role == "user" {
			prefix = "You"
		}

		// Display role header
		s.tmuxCtrl.RunInPane(tmux.RoleRight, []string{"echo", fmt.Sprintf("=== %s ===", prefix)}, nil)

		// Display message content
		lines := splitLines(turn.Content)
		for _, line := range lines {
			if line == "" {
				s.tmuxCtrl.SendKeys(tmux.RoleRight, "Enter")
			} else {
				s.tmuxCtrl.RunInPane(tmux.RoleRight, []string{"echo", line}, nil)
			}
		}

		// Add separator
		s.tmuxCtrl.SendKeys(tmux.RoleRight, "Enter")
	}
}

// convertMessages converts pkg/ai.Message to internal/ai.Message.
func convertMessages(messages []Message) []intai.Message {
	result := make([]intai.Message, len(messages))
	for i, msg := range messages {
		result[i] = intai.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}
	return result
}

// handleConversationCompact triggers conversation compaction/summarization.
func (s *Server) handleConversationCompact(req ConversationRequest) ConversationResponse {
	// Validate conversation ID
	if req.ConversationID == "" {
		return ConversationResponse{
			Success: false,
			Error:   "conversation_id is required for compact action",
		}
	}

	// Verify conversation exists
	_, err := s.convMgr.Get(req.ConversationID)
	if err != nil {
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("conversation not found: %v", err),
		}
	}

	// Trigger compaction via AI engine
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.engine.CompactConversation(ctx); err != nil {
		debug.Log("Conversation compaction failed: %v", err)
		return ConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("compaction failed: %v", err),
		}
	}

	debug.Log("Conversation compacted: id=%s provider=%s", req.ConversationID, s.engine.GetProvider())

	return ConversationResponse{
		Success:        true,
		ConversationID: req.ConversationID,
		Message:        "Conversation compacted successfully",
	}
}

// sendConvResponse writes a ConversationResponse to the connection.
func (s *Server) sendConvResponse(conn net.Conn, resp ConversationResponse) {
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(resp); err != nil {
		debug.Log("AI server conversation response error: %v", err)
	}
}

// Package ai provides a public API for AI requests over Unix socket.
// This package defines the protocol for client applications to communicate with muxctl.
package ai

import (
	"fmt"
)

// Message represents a chat message for AI interactions.
type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"`
}

// ActionType represents the type of AI action to perform.
type ActionType string

const (
	ActionSummarize ActionType = "summarize"
	ActionExplain   ActionType = "explain"
)

// Request is sent from client to muxctl over the socket.
type Request struct {
	// Action to perform (summarize, explain, or custom action name)
	Action string `json:"action"`

	// TargetPane where AI output should be displayed (top, left, right)
	TargetPane string `json:"target_pane"`

	// SourcePane to capture content from (top, left, right)
	// If empty, uses Context.PaneContent instead
	SourcePane string `json:"source_pane,omitempty"`

	// Context provides application-specific context for the AI
	Context RequestContext `json:"context"`

	// Options for the request
	Options RequestOptions `json:"options,omitempty"`
}

// RequestContext contains context information for the AI request.
type RequestContext struct {
	// Kubernetes context
	Cluster     string `json:"cluster,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	KubeContext string `json:"kube_context,omitempty"`

	// Selected alert (if applicable)
	SelectedAlert *AlertInfo `json:"selected_alert,omitempty"`

	// Selected resource (if applicable)
	SelectedResource *ResourceInfo `json:"selected_resource,omitempty"`

	// Pre-captured pane content (alternative to SourcePane)
	PaneContent string `json:"pane_content,omitempty"`

	// Custom metadata (application-specific)
	Custom map[string]interface{} `json:"custom,omitempty"`
}

// AlertInfo contains information about a selected alert.
type AlertInfo struct {
	Name     string `json:"name"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`
}

// ResourceInfo contains information about a selected resource.
type ResourceInfo struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// RequestOptions contains optional parameters for the request.
type RequestOptions struct {
	// MaxLines to capture from source pane (default: 300)
	MaxLines int `json:"max_lines,omitempty"`

	// LastCommand mode: capture only last command and its output
	LastCommand bool `json:"last_command,omitempty"`
}

// Response is sent from muxctl back to client.
type Response struct {
	// Success indicates if the request was processed successfully
	Success bool `json:"success"`

	// Error message if Success is false
	Error string `json:"error,omitempty"`

	// RequestID for tracking (optional)
	RequestID string `json:"request_id,omitempty"`
}

// SocketPath returns the socket path for a given session.
func SocketPath(session string) string {
	return fmt.Sprintf("/tmp/muxctl-%s.sock", session)
}

// Validate checks if the request is valid.
func (r *Request) Validate() error {
	if r.Action == "" {
		return fmt.Errorf("action is required")
	}
	if r.TargetPane == "" {
		return fmt.Errorf("target_pane is required")
	}
	// Either SourcePane or PaneContent must be provided
	if r.SourcePane == "" && r.Context.PaneContent == "" {
		return fmt.Errorf("either source_pane or context.pane_content is required")
	}
	return nil
}

// ConversationAction represents the type of conversation operation.
type ConversationAction string

const (
	// ConvActionStart initiates a new conversation
	ConvActionStart ConversationAction = "start"
	// ConvActionSend sends a user message and gets response
	ConvActionSend ConversationAction = "send"
	// ConvActionEnd terminates the conversation
	ConvActionEnd ConversationAction = "end"
	// ConvActionResize changes the conversation pane size
	ConvActionResize ConversationAction = "resize"
	// ConvActionCompact triggers conversation compaction/summarization
	ConvActionCompact ConversationAction = "compact"
)

// ConversationRequest is sent from client to muxctl for conversation operations.
type ConversationRequest struct {
	// Action specifies the conversation operation (start, send, end, resize)
	Action ConversationAction `json:"action"`

	// ConversationID identifies an existing conversation (empty for "start")
	ConversationID string `json:"conversation_id,omitempty"`

	// Message is the user's message (for "send" action)
	Message string `json:"message,omitempty"`

	// Context provides the initial conversation context (for "start" action)
	Context ConversationRequestContext `json:"context,omitempty"`

	// Options contains optional parameters
	Options ConversationOptions `json:"options,omitempty"`
}

// ConversationRequestContext contains context for starting a conversation.
type ConversationRequestContext struct {
	// AlertFingerprint uniquely identifies the alert
	AlertFingerprint string `json:"alert_fingerprint"`

	// Cluster is the Kubernetes cluster context
	Cluster string `json:"cluster"`

	// Namespace is the Kubernetes namespace
	Namespace string `json:"namespace,omitempty"`

	// InitialSummary contains the AI summary shown before conversation started
	InitialSummary string `json:"initial_summary,omitempty"`

	// Metadata contains additional context-specific data
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ConversationOptions contains optional parameters for conversation requests.
type ConversationOptions struct {
	// ExpandWidth specifies the pane width percentage (40-80)
	ExpandWidth int `json:"expand_width,omitempty"`

	// Stream enables streaming responses (future feature)
	Stream bool `json:"stream,omitempty"`
}

// ConversationResponse is sent from muxctl back to client.
type ConversationResponse struct {
	// Success indicates if the request was processed successfully
	Success bool `json:"success"`

	// Error message if Success is false
	Error string `json:"error,omitempty"`

	// ConversationID for the conversation (returned on "start")
	ConversationID string `json:"conversation_id,omitempty"`

	// Message is the assistant's response (for "send" action)
	Message string `json:"message,omitempty"`

	// TurnCount is the total number of turns in the conversation
	TurnCount int `json:"turn_count,omitempty"`

	// State is the current conversation state
	State string `json:"state,omitempty"`
}

// Validate checks if the conversation request is valid.
func (r *ConversationRequest) Validate() error {
	if r.Action == "" {
		return fmt.Errorf("action is required")
	}

	switch r.Action {
	case ConvActionStart:
		if r.Context.AlertFingerprint == "" {
			return fmt.Errorf("context.alert_fingerprint is required for start action")
		}
		if r.Context.Cluster == "" {
			return fmt.Errorf("context.cluster is required for start action")
		}
	case ConvActionSend:
		if r.ConversationID == "" {
			return fmt.Errorf("conversation_id is required for send action")
		}
		if r.Message == "" {
			return fmt.Errorf("message is required for send action")
		}
	case ConvActionEnd, ConvActionResize:
		if r.ConversationID == "" {
			return fmt.Errorf("conversation_id is required for %s action", r.Action)
		}
	default:
		return fmt.Errorf("invalid action: %s", r.Action)
	}

	return nil
}

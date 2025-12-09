package ai

import (
	"fmt"
	"path/filepath"
)

// ConversationAction represents an action to perform on a conversation
type ConversationAction string

const (
	ConvActionStart  ConversationAction = "start"
	ConvActionSend   ConversationAction = "send"
	ConvActionEnd    ConversationAction = "end"
	ConvActionResize ConversationAction = "resize"
)

// ConversationRequestContext provides context for a conversation
type ConversationRequestContext struct {
	AlertFingerprint string `json:"alert_fingerprint"`
	Cluster          string `json:"cluster"`
	Namespace        string `json:"namespace"`
	InitialSummary   string `json:"initial_summary"`
}

// ConversationOptions provides options for conversation management
type ConversationOptions struct {
	ExpandWidth int `json:"expand_width"` // Width percentage for conversation pane
}

// ConversationRequest represents a request to the AI conversation service
type ConversationRequest struct {
	Action         ConversationAction         `json:"action"`
	ConversationID string                     `json:"conversation_id,omitempty"`
	Message        string                     `json:"message,omitempty"`
	Context        ConversationRequestContext `json:"context,omitempty"`
	Options        ConversationOptions        `json:"options,omitempty"`
}

// ConversationResponse represents a response from the AI conversation service
type ConversationResponse struct {
	Success        bool   `json:"success"`
	ConversationID string `json:"conversation_id,omitempty"`
	Message        string `json:"message,omitempty"`
	Error          string `json:"error,omitempty"`
}

// Validate validates a conversation request
func (r *ConversationRequest) Validate() error {
	if r.Action == "" {
		return fmt.Errorf("action is required")
	}

	switch r.Action {
	case ConvActionStart:
		if r.Context.AlertFingerprint == "" {
			return fmt.Errorf("alert_fingerprint is required for start action")
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
		return fmt.Errorf("unknown action: %s", r.Action)
	}

	return nil
}

// SocketPath returns the Unix socket path for the given session
func SocketPath(sessionName string) string {
	return filepath.Join("/tmp", fmt.Sprintf("muxctl-%s-ai.sock", sessionName))
}

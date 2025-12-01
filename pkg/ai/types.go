// Package ai provides a public API for AI requests over Unix socket.
// This package defines the protocol for sctl to communicate with muxctl.
package ai

import (
	"fmt"
)

// ActionType represents the type of AI action to perform.
type ActionType string

const (
	ActionSummarize ActionType = "summarize"
	ActionExplain   ActionType = "explain"
)

// Request is sent from sctl to muxctl over the socket.
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

// Response is sent from muxctl back to sctl.
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

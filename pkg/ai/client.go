package ai

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Client communicates with muxctl AI server over Unix socket.
type Client struct {
	session    string
	socketPath string
	timeout    time.Duration
}

// NewClient creates a new AI client for the given session.
func NewClient(session string) *Client {
	return &Client{
		session:    session,
		socketPath: SocketPath(session),
		timeout:    30 * time.Second,
	}
}

// NewClientFromEnv creates a client using MUXCTL_SESSION environment variable.
func NewClientFromEnv() (*Client, error) {
	session := os.Getenv("MUXCTL_SESSION")
	if session == "" {
		// Fall back to MUXCTL (older env var name)
		session = os.Getenv("MUXCTL")
	}
	if session == "" {
		return nil, fmt.Errorf("not running inside muxctl (MUXCTL_SESSION not set)")
	}
	return NewClient(session), nil
}

// SetTimeout sets the request timeout.
func (c *Client) SetTimeout(d time.Duration) {
	c.timeout = d
}

// Send sends a request to the muxctl AI server and waits for response.
func (c *Client) Send(req Request) (*Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to muxctl socket %s: %w", c.socketPath, err)
	}
	defer conn.Close()

	// Set deadline
	conn.SetDeadline(time.Now().Add(c.timeout))

	// Send request
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	var resp Response
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// Summarize sends a summarize request for the given source pane.
func (c *Client) Summarize(ctx RequestContext, sourcePane, targetPane string) error {
	req := Request{
		Action:     string(ActionSummarize),
		SourcePane: sourcePane,
		TargetPane: targetPane,
		Context:    ctx,
		Options: RequestOptions{
			MaxLines: 300,
		},
	}

	resp, err := c.Send(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("AI request failed: %s", resp.Error)
	}

	return nil
}

// Explain sends an explain request for the given source pane.
func (c *Client) Explain(ctx RequestContext, sourcePane, targetPane string) error {
	req := Request{
		Action:     string(ActionExplain),
		SourcePane: sourcePane,
		TargetPane: targetPane,
		Context:    ctx,
		Options: RequestOptions{
			MaxLines: 100,
		},
	}

	resp, err := c.Send(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("AI request failed: %s", resp.Error)
	}

	return nil
}

// CustomAction sends a custom action request.
func (c *Client) CustomAction(action string, ctx RequestContext, sourcePane, targetPane string, opts RequestOptions) error {
	req := Request{
		Action:     action,
		SourcePane: sourcePane,
		TargetPane: targetPane,
		Context:    ctx,
		Options:    opts,
	}

	resp, err := c.Send(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("AI request failed: %s", resp.Error)
	}

	return nil
}

// IsServerRunning checks if the muxctl AI server is running.
func (c *Client) IsServerRunning() bool {
	conn, err := net.DialTimeout("unix", c.socketPath, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

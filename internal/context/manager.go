package context

import (
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// Context holds the current working context for muxctl.
type Context struct {
	// Kubernetes context
	Cluster     string `json:"cluster"`
	Environment string `json:"environment"`
	Region      string `json:"region"`
	Namespace   string `json:"namespace"`
	KubeContext string `json:"kube_context"`

	// Resource context (generic)
	ResourceKind string `json:"resource_kind,omitempty"`
	ResourceName string `json:"resource_name,omitempty"`

	// Custom metadata (application-specific, passed through as env vars)
	// Applications can set these via MUXCTL_CONTEXT_* environment variables
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ContextUpdate represents a partial context update.
type ContextUpdate struct {
	Cluster      *string
	Environment  *string
	Region       *string
	Namespace    *string
	KubeContext  *string
	ResourceKind *string
	ResourceName *string
	Metadata     map[string]string // Merged into existing metadata
}

// Manager manages the current context.
type Manager interface {
	Current() Context
	Set(update ContextUpdate) Context
	Subscribe(ch chan<- Context)
	Refresh() error
}

// ContextManager implements Manager.
type ContextManager struct {
	mu          sync.RWMutex
	ctx         Context
	subscribers []chan<- Context
}

// NewManager creates a new ContextManager.
func NewManager() *ContextManager {
	return &ContextManager{
		ctx:         Context{},
		subscribers: make([]chan<- Context, 0),
	}
}

// Current returns the current context.
func (m *ContextManager) Current() Context {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ctx
}

// Set applies updates to the context and notifies subscribers.
func (m *ContextManager) Set(update ContextUpdate) Context {
	m.mu.Lock()
	defer m.mu.Unlock()

	if update.Cluster != nil {
		m.ctx.Cluster = *update.Cluster
	}
	if update.Environment != nil {
		m.ctx.Environment = *update.Environment
	}
	if update.Region != nil {
		m.ctx.Region = *update.Region
	}
	if update.Namespace != nil {
		m.ctx.Namespace = *update.Namespace
	}
	if update.KubeContext != nil {
		m.ctx.KubeContext = *update.KubeContext
	}
	if update.ResourceKind != nil {
		m.ctx.ResourceKind = *update.ResourceKind
	}
	if update.ResourceName != nil {
		m.ctx.ResourceName = *update.ResourceName
	}
	if update.Metadata != nil {
		if m.ctx.Metadata == nil {
			m.ctx.Metadata = make(map[string]string)
		}
		for k, v := range update.Metadata {
			m.ctx.Metadata[k] = v
		}
	}

	// Notify subscribers
	for _, ch := range m.subscribers {
		select {
		case ch <- m.ctx:
		default:
			// Don't block if subscriber is not ready
		}
	}

	return m.ctx
}

// Subscribe registers a channel to receive context updates.
func (m *ContextManager) Subscribe(ch chan<- Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers = append(m.subscribers, ch)
}

// Refresh reloads context from external sources (kubectl).
func (m *ContextManager) Refresh() error {
	return m.loadKubeContext()
}

// loadKubeContext loads context from kubectl.
// Runs both kubectl commands in parallel for better performance.
func (m *ContextManager) loadKubeContext() error {
	type result struct {
		kind   string
		output string
		err    error
	}

	results := make(chan result, 2)

	// Fetch current-context in parallel
	go func() {
		cmd := exec.Command("kubectl", "config", "current-context")
		output, err := cmd.Output()
		results <- result{kind: "context", output: strings.TrimSpace(string(output)), err: err}
	}()

	// Fetch namespace in parallel
	go func() {
		cmd := exec.Command("kubectl", "config", "view", "--minify", "-o", "jsonpath={..namespace}")
		output, err := cmd.Output()
		results <- result{kind: "namespace", output: string(output), err: err}
	}()

	var kubeCtx, namespace string

	// Collect results
	for i := 0; i < 2; i++ {
		res := <-results
		switch res.kind {
		case "context":
			if res.err != nil {
				// kubectl might not be configured, that's okay
				return nil
			}
			kubeCtx = res.output
		case "namespace":
			if res.err == nil && len(res.output) > 0 {
				namespace = res.output
			}
		}
	}

	// Update context with lock held only once
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ctx.KubeContext = kubeCtx

	// Try to derive cluster and region from context name
	cluster, region := DeriveClusterRegionFromKubeContext(kubeCtx)
	if cluster != "" {
		m.ctx.Cluster = cluster
	}
	if region != "" {
		m.ctx.Region = region
	}

	if namespace != "" {
		m.ctx.Namespace = namespace
	}
	if m.ctx.Namespace == "" {
		m.ctx.Namespace = "default"
	}

	return nil
}

// DeriveClusterRegionFromKubeContext extracts cluster and region from a kubecontext name.
// Example: "teleport.com-prod-us-ashburn-1" -> cluster="prod-us", region="us-ashburn-1"
func DeriveClusterRegionFromKubeContext(name string) (cluster, region string) {
	// Pattern: prefix-{env}-{region-parts}
	// Try to match common patterns

	// Pattern 1: something-prod-us-region-n
	re1 := regexp.MustCompile(`-?(prod|stage|dev|staging)-([a-z]+-[a-z]+-\d+)$`)
	if matches := re1.FindStringSubmatch(name); len(matches) >= 3 {
		env := matches[1]
		regionPart := matches[2]
		// Extract region prefix (e.g., "us" from "us-ashburn-1")
		regionPrefix := strings.Split(regionPart, "-")[0]
		return env + "-" + regionPrefix, regionPart
	}

	// Pattern 2: something-prod-us or env-region
	re2 := regexp.MustCompile(`-?(prod|stage|dev|staging)-([a-z]+)$`)
	if matches := re2.FindStringSubmatch(name); len(matches) >= 3 {
		return matches[1] + "-" + matches[2], matches[2]
	}

	// Pattern 3: just the context name if short
	if !strings.Contains(name, ".") && len(name) < 30 {
		return name, ""
	}

	return "", ""
}

// Env returns environment variables for the current context.
func (c Context) Env() map[string]string {
	env := map[string]string{}

	if c.Cluster != "" {
		env["MUXCTL_CONTEXT_CLUSTER"] = c.Cluster
	}
	if c.Environment != "" {
		env["MUXCTL_CONTEXT_ENVIRONMENT"] = c.Environment
	}
	if c.Region != "" {
		env["MUXCTL_CONTEXT_REGION"] = c.Region
	}
	if c.Namespace != "" {
		env["MUXCTL_CONTEXT_NAMESPACE"] = c.Namespace
	}
	if c.KubeContext != "" {
		env["MUXCTL_CONTEXT_KUBECONTEXT"] = c.KubeContext
	}
	if c.ResourceKind != "" {
		env["MUXCTL_CONTEXT_RESOURCE_KIND"] = c.ResourceKind
	}
	if c.ResourceName != "" {
		env["MUXCTL_CONTEXT_RESOURCE_NAME"] = c.ResourceName
	}

	// Export custom metadata as MUXCTL_CONTEXT_* variables
	for k, v := range c.Metadata {
		env["MUXCTL_CONTEXT_"+strings.ToUpper(k)] = v
	}

	return env
}

// WindowNameBase returns a base name for tmux window.
func (c Context) WindowNameBase() string {
	parts := []string{}

	if c.Cluster != "" {
		parts = append(parts, c.Cluster)
	}
	if c.Namespace != "" && c.Namespace != "default" {
		parts = append(parts, "ns:"+c.Namespace)
	}

	if len(parts) == 0 {
		return "muxctl"
	}

	return strings.Join(parts, "/")
}

// PaneTitle returns a title for a pane with the given role.
func (c Context) PaneTitle(role string) string {
	base := c.WindowNameBase()
	return "[" + role + "] " + base
}

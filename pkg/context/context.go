// Package context provides a public API for context management.
// This package wraps internal/context for use by external modules.
package context

import (
	intctx "github.com/xunzhou/muxctl/internal/context"
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

	// Custom metadata (application-specific)
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

// ContextManager wraps internal/context.ContextManager for public use.
type ContextManager struct {
	impl *intctx.ContextManager
}

// NewManager creates a new ContextManager.
func NewManager() *ContextManager {
	return &ContextManager{
		impl: intctx.NewManager(),
	}
}

// Current returns the current context.
func (m *ContextManager) Current() Context {
	c := m.impl.Current()
	return Context{
		Cluster:      c.Cluster,
		Environment:  c.Environment,
		Region:       c.Region,
		Namespace:    c.Namespace,
		KubeContext:  c.KubeContext,
		ResourceKind: c.ResourceKind,
		ResourceName: c.ResourceName,
		Metadata:     c.Metadata,
	}
}

// Set applies updates to the context and notifies subscribers.
func (m *ContextManager) Set(update ContextUpdate) Context {
	c := m.impl.Set(intctx.ContextUpdate{
		Cluster:      update.Cluster,
		Environment:  update.Environment,
		Region:       update.Region,
		Namespace:    update.Namespace,
		KubeContext:  update.KubeContext,
		ResourceKind: update.ResourceKind,
		ResourceName: update.ResourceName,
		Metadata:     update.Metadata,
	})
	return Context{
		Cluster:      c.Cluster,
		Environment:  c.Environment,
		Region:       c.Region,
		Namespace:    c.Namespace,
		KubeContext:  c.KubeContext,
		ResourceKind: c.ResourceKind,
		ResourceName: c.ResourceName,
		Metadata:     c.Metadata,
	}
}

// Subscribe registers a channel to receive context updates.
// Note: The channel receives internal Context type, caller must convert.
func (m *ContextManager) Subscribe(ch chan<- Context) {
	// Create internal channel and forward
	intCh := make(chan intctx.Context, 1)
	m.impl.Subscribe(intCh)

	// Forward in goroutine
	go func() {
		for c := range intCh {
			ch <- Context{
				Cluster:      c.Cluster,
				Environment:  c.Environment,
				Region:       c.Region,
				Namespace:    c.Namespace,
				KubeContext:  c.KubeContext,
				ResourceKind: c.ResourceKind,
				ResourceName: c.ResourceName,
				Metadata:     c.Metadata,
			}
		}
	}()
}

// Refresh reloads context from external sources (kubectl).
func (m *ContextManager) Refresh() error {
	return m.impl.Refresh()
}

// Env returns environment variables for the context.
func (c Context) Env() map[string]string {
	// Re-use internal implementation via conversion
	intC := intctx.Context{
		Cluster:      c.Cluster,
		Environment:  c.Environment,
		Region:       c.Region,
		Namespace:    c.Namespace,
		KubeContext:  c.KubeContext,
		ResourceKind: c.ResourceKind,
		ResourceName: c.ResourceName,
		Metadata:     c.Metadata,
	}
	return intC.Env()
}

package context

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("NewManager() returned nil")
	}

	ctx := m.Current()
	if ctx.Cluster != "" {
		t.Errorf("expected empty Cluster, got %q", ctx.Cluster)
	}
}

func TestContextManager_Set(t *testing.T) {
	m := NewManager()

	cluster := "prod-us"
	namespace := "default"

	ctx := m.Set(ContextUpdate{
		Cluster:   &cluster,
		Namespace: &namespace,
	})

	if ctx.Cluster != cluster {
		t.Errorf("Set() Cluster = %q, want %q", ctx.Cluster, cluster)
	}
	if ctx.Namespace != namespace {
		t.Errorf("Set() Namespace = %q, want %q", ctx.Namespace, namespace)
	}

	// Verify Current() returns the same
	current := m.Current()
	if current.Cluster != cluster {
		t.Errorf("Current() Cluster = %q, want %q", current.Cluster, cluster)
	}
}

func TestContextManager_SetMetadata(t *testing.T) {
	m := NewManager()

	// Set initial metadata
	m.Set(ContextUpdate{
		Metadata: map[string]string{"key1": "value1"},
	})

	ctx := m.Current()
	if ctx.Metadata["key1"] != "value1" {
		t.Errorf("Metadata[key1] = %q, want 'value1'", ctx.Metadata["key1"])
	}

	// Add more metadata - should merge
	m.Set(ContextUpdate{
		Metadata: map[string]string{"key2": "value2"},
	})

	ctx = m.Current()
	if ctx.Metadata["key1"] != "value1" {
		t.Errorf("Metadata[key1] after merge = %q, want 'value1'", ctx.Metadata["key1"])
	}
	if ctx.Metadata["key2"] != "value2" {
		t.Errorf("Metadata[key2] = %q, want 'value2'", ctx.Metadata["key2"])
	}
}

func TestContextManager_Subscribe(t *testing.T) {
	m := NewManager()
	ch := make(chan Context, 1)

	m.Subscribe(ch)

	cluster := "test-cluster"
	m.Set(ContextUpdate{Cluster: &cluster})

	select {
	case ctx := <-ch:
		if ctx.Cluster != cluster {
			t.Errorf("received Cluster = %q, want %q", ctx.Cluster, cluster)
		}
	default:
		t.Error("expected to receive context update on channel")
	}
}

func TestContext_Env(t *testing.T) {
	ctx := Context{
		Cluster:      "prod",
		Environment:  "production",
		Region:       "us-east-1",
		Namespace:    "app",
		KubeContext:  "prod-cluster",
		ResourceKind: "deployment",
		ResourceName: "my-app",
		Metadata:     map[string]string{"custom": "value"},
	}

	env := ctx.Env()

	expected := map[string]string{
		"MUXCTL_CONTEXT_CLUSTER":       "prod",
		"MUXCTL_CONTEXT_ENVIRONMENT":   "production",
		"MUXCTL_CONTEXT_REGION":        "us-east-1",
		"MUXCTL_CONTEXT_NAMESPACE":     "app",
		"MUXCTL_CONTEXT_KUBECONTEXT":   "prod-cluster",
		"MUXCTL_CONTEXT_RESOURCE_KIND": "deployment",
		"MUXCTL_CONTEXT_RESOURCE_NAME": "my-app",
		"MUXCTL_CONTEXT_CUSTOM":        "value",
	}

	for k, v := range expected {
		if env[k] != v {
			t.Errorf("Env()[%q] = %q, want %q", k, env[k], v)
		}
	}
}

func TestContext_Env_Empty(t *testing.T) {
	ctx := Context{}
	env := ctx.Env()

	if len(env) != 0 {
		t.Errorf("Env() for empty context has %d entries, want 0", len(env))
	}
}

func TestContext_WindowNameBase(t *testing.T) {
	tests := []struct {
		name     string
		ctx      Context
		expected string
	}{
		{
			name:     "empty context",
			ctx:      Context{},
			expected: "muxctl",
		},
		{
			name:     "cluster only",
			ctx:      Context{Cluster: "prod"},
			expected: "prod",
		},
		{
			name:     "cluster and namespace",
			ctx:      Context{Cluster: "prod", Namespace: "myapp"},
			expected: "prod/ns:myapp",
		},
		{
			name:     "cluster and default namespace",
			ctx:      Context{Cluster: "prod", Namespace: "default"},
			expected: "prod",
		},
		{
			name:     "namespace only (non-default)",
			ctx:      Context{Namespace: "myapp"},
			expected: "ns:myapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ctx.WindowNameBase()
			if got != tt.expected {
				t.Errorf("WindowNameBase() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestContext_PaneTitle(t *testing.T) {
	ctx := Context{Cluster: "prod", Namespace: "app"}
	title := ctx.PaneTitle("left")

	expected := "[left] prod/ns:app"
	if title != expected {
		t.Errorf("PaneTitle() = %q, want %q", title, expected)
	}
}

func TestDeriveClusterRegionFromKubeContext(t *testing.T) {
	tests := []struct {
		name           string
		kubeContext    string
		expectedClust  string
		expectedRegion string
	}{
		{
			name:           "teleport pattern",
			kubeContext:    "teleport.com-prod-us-ashburn-1",
			expectedClust:  "prod-us",
			expectedRegion: "us-ashburn-1",
		},
		{
			name:           "simple env-region",
			kubeContext:    "mycompany-prod-us",
			expectedClust:  "prod-us",
			expectedRegion: "us",
		},
		{
			name:           "staging environment",
			kubeContext:    "cluster-staging-eu",
			expectedClust:  "staging-eu",
			expectedRegion: "eu",
		},
		{
			name:           "dev environment",
			kubeContext:    "app-dev-local",
			expectedClust:  "dev-local",
			expectedRegion: "local",
		},
		{
			name:           "short name passthrough",
			kubeContext:    "minikube",
			expectedClust:  "minikube",
			expectedRegion: "",
		},
		{
			name:           "complex domain ignored",
			kubeContext:    "arn:aws:eks:us-west-2:123456789:cluster/my-cluster.example.com",
			expectedClust:  "",
			expectedRegion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, region := DeriveClusterRegionFromKubeContext(tt.kubeContext)
			if cluster != tt.expectedClust {
				t.Errorf("cluster = %q, want %q", cluster, tt.expectedClust)
			}
			if region != tt.expectedRegion {
				t.Errorf("region = %q, want %q", region, tt.expectedRegion)
			}
		})
	}
}

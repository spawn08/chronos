package sandbox

import (
	"context"
	"fmt"
	"time"
)

// K8sJobSandbox implements Sandbox using Kubernetes Jobs for cluster-level isolation.
// Each execution creates a Kubernetes Job, waits for completion, and collects output.
type K8sJobSandbox struct {
	image          string
	namespace      string
	serviceAccount string
}

// K8sJobConfig holds Kubernetes Job sandbox configuration.
type K8sJobConfig struct {
	Image          string
	Namespace      string
	ServiceAccount string
}

// NewK8sJobSandbox creates a Kubernetes Job-based sandbox.
func NewK8sJobSandbox(cfg K8sJobConfig) *K8sJobSandbox {
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	return &K8sJobSandbox{
		image:          cfg.Image,
		namespace:      cfg.Namespace,
		serviceAccount: cfg.ServiceAccount,
	}
}

func (s *K8sJobSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	// K8s Job creation would use the Kubernetes client-go library.
	// For now, return a descriptive error until K8s client is integrated.
	return nil, fmt.Errorf("k8s sandbox: Kubernetes client not yet integrated (image: %s, namespace: %s)", s.image, s.namespace)
}

func (s *K8sJobSandbox) Close() error { return nil }

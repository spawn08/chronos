package sandbox

import (
	"context"
	"fmt"
	"time"
)

// K8sJobSandbox is a placeholder for a future Kubernetes Job isolation backend.
// It is not yet implemented: no Kubernetes client (client-go) is wired in.
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

// NewK8sJobSandbox reports that the Kubernetes Job backend is not implemented.
//
// Per P2-001 the stub backends fail at construction rather than deferring the
// error to execution time, so callers discover the missing capability
// immediately. It always returns a nil sandbox and a non-nil error.
func NewK8sJobSandbox(cfg K8sJobConfig) (*K8sJobSandbox, error) {
	return nil, fmt.Errorf("k8s sandbox: not implemented (no Kubernetes client integrated; image %q, namespace %q)", cfg.Image, cfg.Namespace)
}

func (s *K8sJobSandbox) Execute(_ context.Context, command string, _ []string, _ time.Duration) (*Result, error) {
	return nil, fmt.Errorf("k8s sandbox: not implemented (image: %s, namespace: %s, command: %s)", s.image, s.namespace, command)
}

func (s *K8sJobSandbox) Close() error { return nil }

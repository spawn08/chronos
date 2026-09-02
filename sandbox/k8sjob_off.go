//go:build !sandbox_k8s

package sandbox

import (
	"context"
	"fmt"
	"time"
)

// K8sJobConfig mirrors the real K8sJobConfig (see k8sjob.go, built with
// -tags sandbox_k8s) so callers compile identically either way.
type K8sJobConfig struct {
	Image          string
	Namespace      string
	ServiceAccount string
}

// K8sJobSandbox is a stub in the default build; see k8sjob.go under
// -tags sandbox_k8s for the real Kubernetes-Job-backed implementation.
type K8sJobSandbox struct{}

func (s *K8sJobSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	return nil, fmt.Errorf("sandbox: k8s backend not built (rebuild with -tags sandbox_k8s)")
}

func (s *K8sJobSandbox) Close() error { return nil }

// NewK8sJobSandbox is disabled in the default build to keep k8s.io/client-go
// (and its large transitive dependency graph) out of the binary — see
// chronos-code's ROADMAP.md "binary size" goal. Rebuild with
// -tags sandbox_k8s to enable the Kubernetes Job sandbox backend.
func NewK8sJobSandbox(cfg K8sJobConfig) (*K8sJobSandbox, error) {
	return nil, fmt.Errorf("sandbox: k8s backend not built (rebuild with -tags sandbox_k8s)")
}

// Example: k8s_sandbox runs an untrusted command as a one-shot Kubernetes Job
// using the K8s sandbox backend. Each execution creates a hardened Job (non-root,
// all capabilities dropped, no privilege escalation, read-only root filesystem),
// waits for it to finish, collects the pod's logs and exit code, and deletes the
// Job.
//
// This example needs a reachable Kubernetes cluster. It resolves credentials the
// same way kubectl does: the in-cluster service account when running inside a
// pod, otherwise your local kubeconfig (KUBECONFIG or ~/.kube/config). With no
// cluster configured it prints setup guidance and exits cleanly.
//
//	# Point kubectl at a cluster first (any of these work):
//	#   kind create cluster        (https://kind.sigs.k8s.io)
//	#   minikube start
//	#   k3d cluster create
//	go run ./examples/k8s_sandbox/
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spawn08/chronos/sandbox"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Kubernetes Job Sandbox Example            ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	ctx := context.Background()

	// Construction builds a Kubernetes client from the ambient config and fails
	// fast if no cluster is reachable.
	sb, err := sandbox.NewFromConfig(sandbox.Config{
		Backend:   sandbox.BackendK8sJob,
		Image:     "alpine:3.19",
		Namespace: "default",
	})
	if err != nil {
		fmt.Println("\nNo Kubernetes cluster is configured, so this example cannot run.")
		fmt.Printf("Reason: %v\n\n", err)
		fmt.Println("To try it, start a local cluster and re-run:")
		fmt.Println("  kind create cluster      # or: minikube start / k3d cluster create")
		fmt.Println("  kubectl get nodes        # confirm connectivity")
		fmt.Println("  go run ./examples/k8s_sandbox/")
		return
	}
	defer sb.Close()

	fmt.Println("\n━━━ Running a command as a Kubernetes Job ━━━")
	// Note: the image must already be pullable by the cluster.
	result, err := sb.Execute(ctx, "sh", []string{"-c", "echo 'hello from a k8s job' && id"}, 2*time.Minute)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Println("  the job did not finish before the deadline (image pull can be slow on first run)")
		}
		fmt.Printf("  execute error: %v\n", err)
		return
	}
	fmt.Printf("  exit code: %d\n", result.ExitCode)
	fmt.Printf("  logs:\n%s\n", result.Stdout)

	fmt.Println("✓ Kubernetes Job sandbox example completed.")
}

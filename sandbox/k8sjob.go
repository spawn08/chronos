//go:build sandbox_k8s

package sandbox

import (
	"context"
	"fmt"
	"io"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// jobNameLabel is the label Kubernetes stamps on pods it creates for a Job.
// It is used to find the pod backing a sandbox Job so its logs and exit code
// can be read.
const jobNameLabel = "batch.kubernetes.io/job-name"

// defaultJobPollInterval is how often Execute polls a Job for completion.
const defaultJobPollInterval = time.Second

// K8sJobSandbox runs untrusted commands as one-shot Kubernetes Jobs. Each
// Execute creates a Job whose pod runs the command in the configured image,
// waits for it to finish (or hit its deadline), collects the pod's logs and
// exit code, and deletes the Job. The pod runs with a hardened security context
// (non-root, all capabilities dropped, no privilege escalation, read-only root
// filesystem) so a compromised workload has minimal reach into the cluster.
type K8sJobSandbox struct {
	client         kubernetes.Interface
	image          string
	namespace      string
	serviceAccount string
	pollInterval   time.Duration
}

// K8sJobConfig holds Kubernetes Job sandbox configuration.
type K8sJobConfig struct {
	// Image is the container image the Job's pod runs. Required.
	Image string
	// Namespace is where Jobs are created. Empty defaults to "default".
	Namespace string
	// ServiceAccount, when set, is the pod's service account. Empty uses the
	// namespace default.
	ServiceAccount string
}

// NewK8sJobSandbox builds a sandbox backed by the ambient Kubernetes cluster.
// It resolves credentials from the in-cluster service account when running
// inside a pod, otherwise from the local kubeconfig (KUBECONFIG or
// ~/.kube/config). It returns an error when no cluster configuration is
// reachable, so a misconfigured environment fails at construction rather than
// at execution time.
func NewK8sJobSandbox(cfg K8sJobConfig) (*K8sJobSandbox, error) {
	if cfg.Image == "" {
		return nil, fmt.Errorf("k8s sandbox: image is required")
	}
	restCfg, err := loadKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("k8s sandbox: load cluster config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("k8s sandbox: build client: %w", err)
	}
	return newK8sJobSandbox(clientset, cfg), nil
}

// newK8sJobSandbox constructs a sandbox from an existing client. It is the seam
// tests use to inject a fake clientset, bypassing cluster discovery.
func newK8sJobSandbox(client kubernetes.Interface, cfg K8sJobConfig) *K8sJobSandbox {
	ns := cfg.Namespace
	if ns == "" {
		ns = "default"
	}
	return &K8sJobSandbox{
		client:         client,
		image:          cfg.Image,
		namespace:      ns,
		serviceAccount: cfg.ServiceAccount,
		pollInterval:   defaultJobPollInterval,
	}
}

// loadKubeConfig resolves a REST config from the in-cluster environment first,
// then falls back to the standard kubeconfig loading rules.
func loadKubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Execute runs command (with args) as a Kubernetes Job and returns its combined
// logs and exit code. The timeout bounds the run both client-side (via ctx) and
// server-side (via the Job's activeDeadlineSeconds). A workload that exits
// non-zero yields a Result with the matching ExitCode and a nil error; only
// orchestration failures (create, timeout, API errors) return an error.
func (s *K8sJobSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	jobName := fmt.Sprintf("chronos-sbx-%d", time.Now().UnixNano())
	job := s.buildJob(jobName, command, args, timeout)

	if _, err := s.client.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("k8s sandbox: create job %q: %w", jobName, err)
	}
	defer s.deleteJob(jobName)

	if err := s.waitForJob(ctx, jobName); err != nil {
		return nil, err
	}
	return s.collectResult(ctx, jobName)
}

// buildJob assembles the hardened Job spec. It is separated from Execute so the
// generated spec can be unit-tested without a cluster.
func (s *K8sJobSandbox) buildJob(name, command string, args []string, timeout time.Duration) *batchv1.Job {
	backoffLimit := int32(0)                 // no retries: one shot
	ttl := int32(60)                         // server-side GC 60s after finish
	deadline := int64(timeout / time.Second) // server-side timeout
	if deadline < 1 {
		deadline = 1
	}
	runAsNonRoot := true
	uid := int64(65534) // "nobody"
	allowPrivEsc := false
	readOnlyRoot := true

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: s.serviceAccount,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &uid,
					},
					Containers: []corev1.Container{{
						Name:    "sandbox",
						Image:   s.image,
						Command: append([]string{command}, args...),
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: &allowPrivEsc,
							ReadOnlyRootFilesystem:   &readOnlyRoot,
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

// waitForJob polls the Job until it reports success or failure, or ctx expires.
func (s *K8sJobSandbox) waitForJob(ctx context.Context, jobName string) error {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		job, err := s.client.BatchV1().Jobs(s.namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err == nil && (job.Status.Succeeded > 0 || job.Status.Failed > 0) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("k8s sandbox: job %q did not complete: %w", jobName, ctx.Err())
		case <-ticker.C:
		}
	}
}

// collectResult reads the backing pod's logs and terminated exit code.
func (s *K8sJobSandbox) collectResult(ctx context.Context, jobName string) (*Result, error) {
	pods, err := s.client.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: jobNameLabel + "=" + jobName,
	})
	if err != nil {
		return nil, fmt.Errorf("k8s sandbox: list pods for job %q: %w", jobName, err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("k8s sandbox: no pod found for job %q", jobName)
	}
	pod := pods.Items[0]

	result := &Result{ExitCode: exitCodeFromPod(&pod)}

	logs, err := s.client.CoreV1().Pods(s.namespace).
		GetLogs(pod.Name, &corev1.PodLogOptions{}).Stream(ctx)
	if err != nil {
		// Logs are best-effort: a completed run with an unreadable log stream
		// still returns its exit code.
		return result, nil
	}
	defer logs.Close()
	data, _ := io.ReadAll(io.LimitReader(logs, 1<<20))
	result.Stdout = string(data)
	return result, nil
}

// exitCodeFromPod extracts the container's terminated exit code, defaulting to
// 0 when no terminated state is reported.
func exitCodeFromPod(pod *corev1.Pod) int {
	for i := range pod.Status.ContainerStatuses {
		if term := pod.Status.ContainerStatuses[i].State.Terminated; term != nil {
			return int(term.ExitCode)
		}
	}
	return 0
}

// deleteJob removes the Job and its pods. It runs on its own short-lived context
// so cleanup proceeds even after the caller's context is canceled.
func (s *K8sJobSandbox) deleteJob(jobName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	propagation := metav1.DeletePropagationBackground
	_ = s.client.BatchV1().Jobs(s.namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
}

// Close releases the sandbox. The Kubernetes client holds no long-lived
// resources that require teardown, so this is a no-op.
func (s *K8sJobSandbox) Close() error { return nil }

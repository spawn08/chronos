//go:build sandbox_k8s

package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newFakeK8sSandbox wires a K8sJobSandbox to a fake clientset that reports the
// backing Job as finished and its pod as terminated with the given exit code,
// so Execute's create/wait/collect path can be exercised without a cluster.
func newFakeK8sSandbox(t *testing.T, exitCode int32, jobFailed bool) *K8sJobSandbox {
	t.Helper()
	client := fake.NewSimpleClientset()

	// Report the Job as complete on the first status poll.
	client.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		name := action.(k8stesting.GetAction).GetName()
		status := batchv1.JobStatus{Succeeded: 1}
		if jobFailed {
			status = batchv1.JobStatus{Failed: 1}
		}
		return true, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Status:     status,
		}, nil
	})

	// Report a single terminated pod for the Job. The generated fake List
	// applies the caller's label selector after this reactor returns, so the
	// pod must carry the labels the selector matches on.
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		sel := action.(k8stesting.ListActionImpl).GetListRestrictions().Labels
		podLabels, _ := labels.ConvertSelectorToLabelsMap(sel.String())
		return true, &corev1.PodList{Items: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "sbx-pod", Namespace: "default", Labels: podLabels},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name: "sandbox",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: exitCode,
				}},
			}}},
		}}}, nil
	})

	sb := newK8sJobSandbox(client, K8sJobConfig{Image: "alpine:3.19", Namespace: "default"})
	sb.pollInterval = 5 * time.Millisecond
	return sb
}

func TestK8sJobSandbox_Execute(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int32
	}{
		{"success", 0},
		{"nonzero exit", 42},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sb := newFakeK8sSandbox(t, tc.exitCode, false)
			res, err := sb.Execute(context.Background(), "echo", []string{"hi"}, 5*time.Second)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.ExitCode != int(tc.exitCode) {
				t.Errorf("ExitCode = %d, want %d", res.ExitCode, tc.exitCode)
			}
		})
	}
}

func TestK8sJobSandbox_BuildJobHardening(t *testing.T) {
	sb := newK8sJobSandbox(fake.NewSimpleClientset(), K8sJobConfig{
		Image:          "alpine:3.19",
		Namespace:      "sandboxes",
		ServiceAccount: "runner",
	})
	job := sb.buildJob("job-1", "sh", []string{"-c", "echo hi"}, 30*time.Second)

	if job.Namespace != "sandboxes" {
		t.Errorf("namespace = %q, want sandboxes", job.Namespace)
	}
	if got := *job.Spec.BackoffLimit; got != 0 {
		t.Errorf("BackoffLimit = %d, want 0 (no retries)", got)
	}
	if got := *job.Spec.ActiveDeadlineSeconds; got != 30 {
		t.Errorf("ActiveDeadlineSeconds = %d, want 30", got)
	}
	pod := job.Spec.Template.Spec
	if pod.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("RestartPolicy = %q, want Never", pod.RestartPolicy)
	}
	if pod.ServiceAccountName != "runner" {
		t.Errorf("ServiceAccountName = %q, want runner", pod.ServiceAccountName)
	}
	c := pod.Containers[0]
	if c.Image != "alpine:3.19" {
		t.Errorf("Image = %q", c.Image)
	}
	if *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("AllowPrivilegeEscalation should be false")
	}
	if !*c.SecurityContext.ReadOnlyRootFilesystem {
		t.Error("ReadOnlyRootFilesystem should be true")
	}
	if len(c.SecurityContext.Capabilities.Drop) != 1 || c.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Errorf("Capabilities.Drop = %v, want [ALL]", c.SecurityContext.Capabilities.Drop)
	}
}

func TestK8sJobSandbox_ExecuteTimeout(t *testing.T) {
	// A Job that never reports completion must surface the context deadline.
	client := fake.NewSimpleClientset()
	sb := newK8sJobSandbox(client, K8sJobConfig{Image: "alpine:3.19"})
	sb.pollInterval = 5 * time.Millisecond

	_, err := sb.Execute(context.Background(), "sleep", []string{"100"}, 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error for a job that never completes")
	}
}

func TestK8sJobSandbox_Close(t *testing.T) {
	sb := newK8sJobSandbox(fake.NewSimpleClientset(), K8sJobConfig{Image: "alpine"})
	if err := sb.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestK8sJobSandbox_NewRequiresImage(t *testing.T) {
	// An empty image is rejected before any cluster discovery, so this check is
	// environment-independent (unlike a real config load).
	sb, err := NewK8sJobSandbox(K8sJobConfig{})
	if err == nil {
		t.Fatal("expected error for empty image")
		return
	}
	if sb != nil {
		t.Errorf("expected nil sandbox, got %v", sb)
	}
	if !strings.Contains(err.Error(), "image is required") {
		t.Errorf("error = %v, want an image-required error", err)
	}
}

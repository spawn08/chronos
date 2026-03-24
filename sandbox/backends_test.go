package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewFromConfig_Process(t *testing.T) {
	sb, err := NewFromConfig(Config{Backend: BackendProcess, WorkDir: os.TempDir()})
	if err != nil {
		t.Fatalf("NewFromConfig(process): %v", err)
	}
	if sb == nil {
		t.Fatal("expected non-nil sandbox")
	}
}

func TestNewFromConfig_ProcessDefault(t *testing.T) {
	// empty backend defaults to process
	sb, err := NewFromConfig(Config{})
	if err != nil {
		t.Fatalf("NewFromConfig(empty): %v", err)
	}
	if sb == nil {
		t.Fatal("expected non-nil sandbox")
	}
}

func TestNewFromConfig_Container(t *testing.T) {
	sb, err := NewFromConfig(Config{Backend: BackendContainer, Image: "alpine:latest"})
	if err != nil {
		t.Fatalf("NewFromConfig(container): %v", err)
	}
	if sb == nil {
		t.Fatal("expected non-nil sandbox")
	}
}

func TestNewFromConfig_WASM_NoPath(t *testing.T) {
	_, err := NewFromConfig(Config{Backend: BackendWASM})
	if err == nil {
		t.Fatal("expected error for wasm without path")
	}
}

func TestNewFromConfig_WASM_WithPath(t *testing.T) {
	sb, err := NewFromConfig(Config{Backend: BackendWASM, WASMPath: "/path/to/module.wasm"})
	if err != nil {
		t.Fatalf("NewFromConfig(wasm): %v", err)
	}
	if sb == nil {
		t.Fatal("expected non-nil sandbox")
	}
}

func TestNewFromConfig_K8s_NoImage(t *testing.T) {
	_, err := NewFromConfig(Config{Backend: BackendK8sJob})
	if err == nil {
		t.Fatal("expected error for k8s without image")
	}
}

func TestNewFromConfig_K8s_WithImage(t *testing.T) {
	sb, err := NewFromConfig(Config{Backend: BackendK8sJob, Image: "alpine:latest", Namespace: "default"})
	if err != nil {
		t.Fatalf("NewFromConfig(k8s): %v", err)
	}
	if sb == nil {
		t.Fatal("expected non-nil sandbox")
	}
}

func TestNewFromConfig_Unknown(t *testing.T) {
	_, err := NewFromConfig(Config{Backend: "unknown-backend"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestParseBackend(t *testing.T) {
	tests := []struct {
		input string
		want  Backend
	}{
		{"process", BackendProcess},
		{"proc", BackendProcess},
		{"PROCESS", BackendProcess},
		{"container", BackendContainer},
		{"docker", BackendContainer},
		{"wasm", BackendWASM},
		{"wasi", BackendWASM},
		{"k8s", BackendK8sJob},
		{"kubernetes", BackendK8sJob},
		{"job", BackendK8sJob},
		{"custom", Backend("custom")},
	}
	for _, tt := range tests {
		got := ParseBackend(tt.input)
		if got != tt.want {
			t.Errorf("ParseBackend(%q)=%q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWASMSandbox_NewWASMSandbox(t *testing.T) {
	sb := NewWASMSandbox("/path/to/mod.wasm")
	if sb == nil {
		t.Fatal("expected non-nil wasm sandbox")
	}
	if sb.wasmPath != "/path/to/mod.wasm" {
		t.Errorf("wasmPath=%q", sb.wasmPath)
	}
}

func TestWASMSandbox_Execute_ReturnsError(t *testing.T) {
	sb := NewWASMSandbox("/not/a/real/module.wasm")
	_, err := sb.Execute(context.Background(), "run", nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from WASM sandbox (not implemented)")
	}
}

func TestWASMSandbox_Close(t *testing.T) {
	sb := NewWASMSandbox("/path/mod.wasm")
	if err := sb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestK8sJobSandbox_Execute(t *testing.T) {
	sb := NewK8sJobSandbox(K8sJobConfig{Image: "alpine", Namespace: "default"})
	_, err := sb.Execute(context.Background(), "echo", []string{"hi"}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from k8s sandbox (not implemented)")
	}
}

func TestK8sJobSandbox_Close(t *testing.T) {
	sb := NewK8sJobSandbox(K8sJobConfig{Image: "alpine"})
	if err := sb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestContainerSandbox_Close(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{Image: "alpine"})
	_ = sb.Close()
}

func TestK8sJobSandbox_DefaultNamespace(t *testing.T) {
	sb := NewK8sJobSandbox(K8sJobConfig{Image: "test"})
	if sb.namespace != "default" {
		t.Errorf("namespace = %q, want default", sb.namespace)
	}
}

func TestK8sJobSandbox_CustomNamespace(t *testing.T) {
	sb := NewK8sJobSandbox(K8sJobConfig{Image: "test", Namespace: "prod"})
	if sb.namespace != "prod" {
		t.Errorf("namespace = %q", sb.namespace)
	}
}

func TestK8sJobSandbox_ServiceAccount(t *testing.T) {
	sb := NewK8sJobSandbox(K8sJobConfig{Image: "test", ServiceAccount: "runner"})
	if sb.serviceAccount != "runner" {
		t.Errorf("serviceAccount = %q", sb.serviceAccount)
	}
}

func TestWASMSandbox_ExecuteContainsModulePath(t *testing.T) {
	sb := NewWASMSandbox("/mod.wasm")
	_, err := sb.Execute(context.Background(), "run", nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "/mod.wasm") {
		t.Errorf("error should contain module path: %v", err)
	}
}

func TestK8sJobSandbox_ExecuteContainsImage(t *testing.T) {
	sb := NewK8sJobSandbox(K8sJobConfig{Image: "myimg:v1", Namespace: "ci"})
	_, err := sb.Execute(context.Background(), "echo", []string{"hi"}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "myimg:v1") {
		t.Errorf("error should contain image: %v", err)
	}
}

func TestNewFromConfig_ProcessWithWorkDir(t *testing.T) {
	dir := t.TempDir()
	sb, err := NewFromConfig(Config{Backend: BackendProcess, WorkDir: dir})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	ps, ok := sb.(*ProcessSandbox)
	if !ok {
		t.Fatal("expected *ProcessSandbox")
	}
	if ps.WorkDir != dir {
		t.Errorf("WorkDir = %q, want %q", ps.WorkDir, dir)
	}
}

func TestNewFromConfig_ProcessEmptyWorkDir(t *testing.T) {
	sb, err := NewFromConfig(Config{Backend: BackendProcess})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	ps, ok := sb.(*ProcessSandbox)
	if !ok {
		t.Fatal("expected *ProcessSandbox")
	}
	if ps.WorkDir != "." {
		t.Errorf("WorkDir = %q, want '.'", ps.WorkDir)
	}
}

func TestNewFromConfig_K8sWithServiceAccount(t *testing.T) {
	sb, err := NewFromConfig(Config{
		Backend:    BackendK8sJob,
		Image:      "alpine",
		Namespace:  "test-ns",
		ServiceAcc: "sa-test",
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	k8s, ok := sb.(*K8sJobSandbox)
	if !ok {
		t.Fatal("expected *K8sJobSandbox")
	}
	if k8s.serviceAccount != "sa-test" {
		t.Errorf("serviceAccount = %q", k8s.serviceAccount)
	}
}

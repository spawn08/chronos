package sandbox

import (
	"context"
	"os"
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
	// Close should not panic even without a real Docker client
	_ = sb.Close()
}

package sandbox

import (
	"os"
	"strings"
	"testing"
)

func TestNewFromConfig_Process(t *testing.T) {
	sb, err := NewFromConfig(Config{Backend: BackendProcess, WorkDir: os.TempDir()})
	if err != nil {
		t.Fatalf("NewFromConfig(process): %v", err)
	}
	if sb == nil {
		t.Fatal("expected non-nil sandbox")
		return
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
		return
	}
}

func TestNewFromConfig_Container(t *testing.T) {
	sb, err := NewFromConfig(Config{Backend: BackendContainer, Image: "alpine:latest"})
	if err != nil {
		t.Fatalf("NewFromConfig(container): %v", err)
	}
	if sb == nil {
		t.Fatal("expected non-nil sandbox")
		return
	}
}

func TestNewFromConfig_WASM_NoPath(t *testing.T) {
	_, err := NewFromConfig(Config{Backend: BackendWASM})
	if err == nil {
		t.Fatal("expected error for wasm without path")
		return
	}
}

func TestNewFromConfig_K8s_NoImage(t *testing.T) {
	_, err := NewFromConfig(Config{Backend: BackendK8sJob})
	if err == nil {
		t.Fatal("expected error for k8s without image")
		return
	}
}

func TestNewFromConfig_K8s_RequiresImage(t *testing.T) {
	// The factory rejects a k8s backend without an image before touching any
	// cluster configuration, so this assertion is environment-independent.
	_, err := NewFromConfig(Config{Backend: BackendK8sJob, Namespace: "default"})
	if err == nil {
		t.Fatal("expected error for k8s backend without image")
		return
	}
	if !strings.Contains(err.Error(), "requires image") {
		t.Errorf("error = %v, want a requires-image error", err)
	}
}

func TestNewFromConfig_Unknown(t *testing.T) {
	_, err := NewFromConfig(Config{Backend: "unknown-backend"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
		return
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

func TestContainerSandbox_Close(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{Image: "alpine"})
	_ = sb.Close()
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

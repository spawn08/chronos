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

func TestNewFromConfig_WASM_WithPath(t *testing.T) {
	// P2-001: the WASM stub fails at construction, even with a valid path.
	_, err := NewFromConfig(Config{Backend: BackendWASM, WASMPath: "/path/to/module.wasm"})
	if err == nil {
		t.Fatal("expected not-implemented error for wasm backend")
		return
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %v, want not-implemented", err)
	}
}

func TestNewFromConfig_K8s_NoImage(t *testing.T) {
	_, err := NewFromConfig(Config{Backend: BackendK8sJob})
	if err == nil {
		t.Fatal("expected error for k8s without image")
		return
	}
}

func TestNewFromConfig_K8s_WithImage(t *testing.T) {
	// P2-001: the K8s stub fails at construction, even with a valid image.
	_, err := NewFromConfig(Config{Backend: BackendK8sJob, Image: "alpine:latest", Namespace: "default"})
	if err == nil {
		t.Fatal("expected not-implemented error for k8s backend")
		return
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %v, want not-implemented", err)
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

func TestWASMSandbox_NewFailsAtConstruction(t *testing.T) {
	// P2-001: the WASM stub fails at construction, not at execution time.
	sb, err := NewWASMSandbox("/path/to/mod.wasm")
	if err == nil {
		t.Fatal("expected not-implemented error at construction")
		return
	}
	if sb != nil {
		t.Errorf("expected nil sandbox, got %v", sb)
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %v, want not-implemented", err)
	}
	if !strings.Contains(err.Error(), "/path/to/mod.wasm") {
		t.Errorf("error should contain module path: %v", err)
	}
}

func TestK8sJobSandbox_NewFailsAtConstruction(t *testing.T) {
	// P2-001: the K8s stub fails at construction, not at execution time.
	tests := []struct {
		name string
		cfg  K8sJobConfig
	}{
		{"with namespace", K8sJobConfig{Image: "alpine", Namespace: "default"}},
		{"image only", K8sJobConfig{Image: "myimg:v1"}},
		{"with service account", K8sJobConfig{Image: "test", ServiceAccount: "runner"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sb, err := NewK8sJobSandbox(tc.cfg)
			if err == nil {
				t.Fatal("expected not-implemented error at construction")
				return
			}
			if sb != nil {
				t.Errorf("expected nil sandbox, got %v", sb)
			}
			if !strings.Contains(err.Error(), "not implemented") {
				t.Errorf("error = %v, want not-implemented", err)
			}
		})
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

func TestNewFromConfig_K8sWithServiceAccount(t *testing.T) {
	// P2-001: the K8s stub fails at construction regardless of config.
	_, err := NewFromConfig(Config{
		Backend:    BackendK8sJob,
		Image:      "alpine",
		Namespace:  "test-ns",
		ServiceAcc: "sa-test",
	})
	if err == nil {
		t.Fatal("expected not-implemented error for k8s backend")
		return
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %v, want not-implemented", err)
	}
}

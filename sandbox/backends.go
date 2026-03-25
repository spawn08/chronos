package sandbox

import (
	"fmt"
	"strings"
)

// Backend identifies a sandbox backend type.
type Backend string

const (
	BackendProcess   Backend = "process"
	BackendContainer Backend = "container"
	BackendWASM      Backend = "wasm"
	BackendK8sJob    Backend = "k8s"
)

// Config holds configuration for sandbox creation.
type Config struct {
	Backend Backend `json:"backend"`
	WorkDir string  `json:"work_dir,omitempty"`
	// Container-specific
	Image   string `json:"image,omitempty"`
	Network string `json:"network,omitempty"`
	// K8s-specific
	Namespace  string `json:"namespace,omitempty"`
	ServiceAcc string `json:"service_account,omitempty"`
	// WASM-specific
	WASMPath string `json:"wasm_path,omitempty"`
}

// NewFromConfig creates a sandbox based on the configuration.
// This is the factory function that selects the backend by config string.
func NewFromConfig(cfg Config) (Sandbox, error) {
	switch cfg.Backend {
	case BackendProcess, "":
		workDir := cfg.WorkDir
		if workDir == "" {
			workDir = "."
		}
		return NewProcessSandbox(workDir), nil

	case BackendContainer:
		return NewContainerSandbox(ContainerConfig{
			Image:       cfg.Image,
			NetworkMode: cfg.Network,
		}), nil

	case BackendWASM:
		if cfg.WASMPath == "" {
			return nil, fmt.Errorf("sandbox: wasm backend requires wasm_path")
		}
		return NewWASMSandbox(cfg.WASMPath), nil

	case BackendK8sJob:
		if cfg.Image == "" {
			return nil, fmt.Errorf("sandbox: k8s backend requires image")
		}
		return NewK8sJobSandbox(K8sJobConfig{
			Image:          cfg.Image,
			Namespace:      cfg.Namespace,
			ServiceAccount: cfg.ServiceAcc,
		}), nil

	default:
		return nil, fmt.Errorf("sandbox: unknown backend %q (supported: process, container, wasm, k8s)", cfg.Backend)
	}
}

// ParseBackend parses a backend string.
func ParseBackend(s string) Backend {
	switch strings.ToLower(s) {
	case "process", "proc":
		return BackendProcess
	case "container", "docker":
		return BackendContainer
	case "wasm", "wasi":
		return BackendWASM
	case "k8s", "kubernetes", "job":
		return BackendK8sJob
	default:
		return Backend(s)
	}
}

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Hardened container defaults. These are applied unless overridden by
// ContainerConfig so that the container backend is safe by default.
const (
	// defaultContainerUser runs the workload as the unprivileged "nobody" user.
	defaultContainerUser = "65534:65534"
	// defaultPidsLimit caps the number of processes to blunt fork bombs.
	defaultPidsLimit = 128
	// defaultNofileLimit caps open file descriptors.
	defaultNofileLimit = 1024
	// defaultNprocLimit caps the number of processes/threads via ulimit.
	defaultNprocLimit = 128
	// defaultTmpfsMount provides a small writable /tmp when the rootfs is read-only.
	defaultTmpfsMount   = "/tmp"
	defaultTmpfsOptions = "rw,noexec,nosuid,nodev,size=64m"
)

// ContainerSandbox implements Sandbox using Docker Engine API for production-grade isolation.
type ContainerSandbox struct {
	Image    string
	client   *http.Client
	sockPath string
	// Resource limits
	MemoryBytes int64
	CPUQuota    int64
	NetworkMode string
	// Hardening
	User           string            // non-root user/uid:gid the workload runs as
	PidsLimit      int64             // max processes inside the container
	NofileLimit    int64             // RLIMIT_NOFILE (open files)
	NprocLimit     int64             // RLIMIT_NPROC (processes/threads)
	ReadonlyRootfs bool              // mount the container root filesystem read-only
	CapAdd         []string          // capabilities to re-add on top of CapDrop ALL
	SeccompProfile string            // custom seccomp profile JSON; empty keeps Docker's default
	Runtime        string            // OCI runtime (e.g. "runsc" for gVisor, "kata-runtime")
	Tmpfs          map[string]string // writable tmpfs mounts (path -> mount options)
}

// ContainerConfig holds container sandbox configuration.
type ContainerConfig struct {
	Image       string
	SocketPath  string
	MemoryBytes int64
	CPUQuota    int64
	NetworkMode string
	// Hardening. Zero values fall back to hardened defaults.
	User           string
	PidsLimit      int64
	NofileLimit    int64
	NprocLimit     int64
	CapAdd         []string
	SeccompProfile string
	// Runtime selects a hardened OCI runtime such as "runsc" (gVisor) or
	// "kata-runtime". Empty uses the daemon default (runc). It is never required.
	Runtime string
	// Tmpfs overrides the default writable /tmp mount. Nil uses the default.
	Tmpfs map[string]string
	// WritableRootfs disables the read-only rootfs hardening when true.
	WritableRootfs bool
}

// NewContainerSandbox creates a Docker-based sandbox with a hardened default profile:
// non-root user, all capabilities dropped, no-new-privileges, the default seccomp
// profile, a pids limit, file/process ulimits, a read-only rootfs backed by a small
// tmpfs /tmp, and memory/CPU limits.
func NewContainerSandbox(cfg ContainerConfig) *ContainerSandbox {
	if cfg.SocketPath == "" {
		cfg.SocketPath = "/var/run/docker.sock"
	}
	if cfg.Image == "" {
		cfg.Image = "alpine:3.19"
	}
	if cfg.MemoryBytes == 0 {
		cfg.MemoryBytes = 256 * 1024 * 1024 // 256 MiB
	}
	if cfg.CPUQuota == 0 {
		cfg.CPUQuota = 50000 // 50% of one core
	}
	if cfg.NetworkMode == "" {
		cfg.NetworkMode = "none"
	}
	if cfg.User == "" {
		cfg.User = defaultContainerUser
	}
	if cfg.PidsLimit == 0 {
		cfg.PidsLimit = defaultPidsLimit
	}
	if cfg.NofileLimit == 0 {
		cfg.NofileLimit = defaultNofileLimit
	}
	if cfg.NprocLimit == 0 {
		cfg.NprocLimit = defaultNprocLimit
	}
	tmpfs := cfg.Tmpfs
	if tmpfs == nil {
		tmpfs = map[string]string{defaultTmpfsMount: defaultTmpfsOptions}
	}

	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", cfg.SocketPath)
		},
	}

	return &ContainerSandbox{
		Image:          cfg.Image,
		sockPath:       cfg.SocketPath,
		MemoryBytes:    cfg.MemoryBytes,
		CPUQuota:       cfg.CPUQuota,
		NetworkMode:    cfg.NetworkMode,
		User:           cfg.User,
		PidsLimit:      cfg.PidsLimit,
		NofileLimit:    cfg.NofileLimit,
		NprocLimit:     cfg.NprocLimit,
		ReadonlyRootfs: !cfg.WritableRootfs,
		CapAdd:         cfg.CapAdd,
		SeccompProfile: cfg.SeccompProfile,
		Runtime:        cfg.Runtime,
		Tmpfs:          tmpfs,
		client: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
		},
	}
}

// buildCreateBody assembles the Docker /containers/create request body with the
// hardened security profile. It is separated from Execute so the generated flags
// can be unit-tested without a running Docker daemon.
func (c *ContainerSandbox) buildCreateBody(cmd []string) map[string]any {
	// Drop every capability; re-add only those explicitly requested.
	securityOpt := []string{"no-new-privileges:true"}
	if c.SeccompProfile != "" {
		securityOpt = append(securityOpt, "seccomp="+c.SeccompProfile)
	}
	// When SeccompProfile is empty we intentionally omit a seccomp opt so the
	// daemon applies its default (restrictive) seccomp profile.

	hostConfig := map[string]any{
		"Memory":         c.MemoryBytes,
		"MemorySwap":     c.MemoryBytes, // disable swap: swap == memory
		"CpuQuota":       c.CPUQuota,
		"NetworkMode":    c.NetworkMode,
		"AutoRemove":     false,
		"ReadonlyRootfs": c.ReadonlyRootfs,
		"CapDrop":        []string{"ALL"},
		"SecurityOpt":    securityOpt,
		"PidsLimit":      c.PidsLimit,
		"Privileged":     false,
		"Ulimits": []map[string]any{
			{"Name": "nofile", "Soft": c.NofileLimit, "Hard": c.NofileLimit},
			{"Name": "nproc", "Soft": c.NprocLimit, "Hard": c.NprocLimit},
		},
	}
	if len(c.CapAdd) > 0 {
		hostConfig["CapAdd"] = c.CapAdd
	}
	if len(c.Tmpfs) > 0 {
		hostConfig["Tmpfs"] = c.Tmpfs
	}
	if c.Runtime != "" {
		hostConfig["Runtime"] = c.Runtime
	}

	return map[string]any{
		"Image":           c.Image,
		"Cmd":             cmd,
		"User":            c.User,
		"AttachStdout":    true,
		"AttachStderr":    true,
		"NetworkDisabled": c.NetworkMode == "none",
		"HostConfig":      hostConfig,
	}
}

func (c *ContainerSandbox) dockerAPI(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("container sandbox: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}

func (c *ContainerSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := append([]string{command}, args...)
	createBody := c.buildCreateBody(cmd)

	// 1. Create container
	resp, err := c.dockerAPI(ctx, http.MethodPost, "/v1.41/containers/create", createBody)
	if err != nil {
		return nil, fmt.Errorf("container create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("container create: %s: %s", resp.Status, string(errBody))
	}

	var createResp struct {
		ID string `json:"Id"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&createResp); decodeErr != nil {
		return nil, fmt.Errorf("container create decode: %w", decodeErr)
	}
	containerID := createResp.ID

	defer c.removeContainer(containerID)

	// 2. Start container
	startResp, err := c.dockerAPI(ctx, http.MethodPost, fmt.Sprintf("/v1.41/containers/%s/start", containerID), nil)
	if err != nil {
		return nil, fmt.Errorf("container start: %w", err)
	}
	startResp.Body.Close()

	// 3. Wait for completion
	waitResp, err := c.dockerAPI(ctx, http.MethodPost, fmt.Sprintf("/v1.41/containers/%s/wait", containerID), nil)
	if err != nil {
		return nil, fmt.Errorf("container wait: %w", err)
	}
	defer waitResp.Body.Close()

	var waitResult struct {
		StatusCode int `json:"StatusCode"`
	}
	_ = json.NewDecoder(waitResp.Body).Decode(&waitResult)

	// 4. Collect logs
	stdout, stderr := c.collectLogs(ctx, containerID)

	return &Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: waitResult.StatusCode,
	}, nil
}

func (c *ContainerSandbox) collectLogs(ctx context.Context, containerID string) (stdout, stderr string) {
	stdoutResp, err := c.dockerAPI(ctx, http.MethodGet, fmt.Sprintf("/v1.41/containers/%s/logs?stdout=1&stderr=0", containerID), nil)
	if err != nil {
		return "", ""
	}
	stdoutBytes, _ := io.ReadAll(io.LimitReader(stdoutResp.Body, 1<<20))
	stdoutResp.Body.Close()

	stderrResp, err := c.dockerAPI(ctx, http.MethodGet, fmt.Sprintf("/v1.41/containers/%s/logs?stdout=0&stderr=1", containerID), nil)
	if err != nil {
		return stripDockerLogHeaders(stdoutBytes), ""
	}
	stderrBytes, _ := io.ReadAll(io.LimitReader(stderrResp.Body, 1<<20))
	stderrResp.Body.Close()

	return stripDockerLogHeaders(stdoutBytes), stripDockerLogHeaders(stderrBytes)
}

// Docker multiplexed stream has 8-byte headers per frame.
func stripDockerLogHeaders(data []byte) string {
	var out bytes.Buffer
	for len(data) >= 8 {
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		data = data[8:]
		if size > len(data) {
			size = len(data)
		}
		out.Write(data[:size])
		data = data[size:]
	}
	if out.Len() == 0 {
		return string(data)
	}
	return out.String()
}

func (c *ContainerSandbox) removeContainer(containerID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.dockerAPI(ctx, http.MethodDelete, fmt.Sprintf("/v1.41/containers/%s?force=true", containerID), nil)
	if err == nil {
		resp.Body.Close()
	}
}

func (c *ContainerSandbox) Close() error { return nil }

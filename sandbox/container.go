package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	Image      string
	client     *http.Client
	sockPath   string
	apiVersion string
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
	Mounts         []BindMount       // explicitly granted host paths
	WorkingDir     string            // directory inside the container
	Env            []string          // explicit environment; no host environment inheritance
}

type BindMount struct {
	Source   string
	Target   string
	ReadOnly bool
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
	Mounts         []BindMount
	WorkingDir     string
	Env            []string
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
		apiVersion:     "v1.41",
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
		Mounts:         append([]BindMount(nil), cfg.Mounts...),
		WorkingDir:     cfg.WorkingDir,
		Env:            append([]string(nil), cfg.Env...),
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
	if len(c.Mounts) > 0 {
		mounts := make([]map[string]any, 0, len(c.Mounts))
		for _, mount := range c.Mounts {
			mounts = append(mounts, map[string]any{
				"Type": "bind", "Source": mount.Source, "Target": mount.Target, "ReadOnly": mount.ReadOnly,
			})
		}
		hostConfig["Mounts"] = mounts
	}

	body := map[string]any{
		"Image":           c.Image,
		"Cmd":             cmd,
		"User":            c.User,
		"AttachStdout":    true,
		"AttachStderr":    true,
		"NetworkDisabled": c.NetworkMode == "none",
		"HostConfig":      hostConfig,
	}
	if c.WorkingDir != "" {
		body["WorkingDir"] = c.WorkingDir
	}
	if len(c.Env) > 0 {
		body["Env"] = c.Env
	}
	return body
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

// Preflight requires a reachable daemon and a locally available image. It
// never pulls an image as an implicit admission side effect.
func (c *ContainerSandbox) Preflight(ctx context.Context) error {
	versionResponse, err := c.dockerAPI(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return fmt.Errorf("container sandbox daemon preflight: %w", err)
	}
	if versionResponse.StatusCode != http.StatusOK {
		versionResponse.Body.Close()
		return fmt.Errorf("container sandbox daemon preflight: HTTP %d", versionResponse.StatusCode)
	}
	var version struct {
		API    string `json:"ApiVersion"`
		MinAPI string `json:"MinAPIVersion"`
	}
	err = json.NewDecoder(versionResponse.Body).Decode(&version)
	versionResponse.Body.Close()
	if err != nil {
		return fmt.Errorf("container sandbox daemon version: %w", err)
	}
	parseMinor := func(value string) (int, error) {
		parts := strings.Split(value, ".")
		if len(parts) != 2 || parts[0] != "1" {
			return 0, fmt.Errorf("unsupported Docker API version %q", value)
		}
		return strconv.Atoi(parts[1])
	}
	maxMinor, err := parseMinor(version.API)
	if err != nil {
		return fmt.Errorf("container sandbox daemon version: %w", err)
	}
	minMinor, err := parseMinor(version.MinAPI)
	if err != nil {
		return fmt.Errorf("container sandbox daemon minimum version: %w", err)
	}
	selected := 41
	if minMinor > selected {
		selected = minMinor
	}
	if maxMinor < selected {
		return fmt.Errorf("container sandbox API version 1.%d is unsupported", selected)
	}
	c.apiVersion = fmt.Sprintf("v1.%d", selected)
	resp, err := c.dockerAPI(ctx, http.MethodGet, "/"+c.apiVersion+"/images/"+url.PathEscape(c.Image)+"/json", nil)
	if err != nil {
		return fmt.Errorf("container sandbox preflight: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("container sandbox image %q is unavailable: HTTP %d", c.Image, resp.StatusCode)
	}
	return nil
}

func (c *ContainerSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := append([]string{command}, args...)
	createBody := c.buildCreateBody(cmd)

	// 1. Create container
	resp, err := c.dockerAPI(ctx, http.MethodPost, "/"+c.apiVersion+"/containers/create", createBody)
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
	startResp, err := c.dockerAPI(ctx, http.MethodPost, fmt.Sprintf("/%s/containers/%s/start", c.apiVersion, containerID), nil)
	if err != nil {
		return nil, fmt.Errorf("container start: %w", err)
	}
	startResp.Body.Close()
	if startResp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("container start: HTTP %d", startResp.StatusCode)
	}

	// 3. Wait for completion
	waitResp, err := c.dockerAPI(ctx, http.MethodPost, fmt.Sprintf("/%s/containers/%s/wait", c.apiVersion, containerID), nil)
	if err != nil {
		return nil, fmt.Errorf("container wait: %w", err)
	}
	defer waitResp.Body.Close()

	var waitResult struct {
		StatusCode int `json:"StatusCode"`
	}
	if waitResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("container wait: HTTP %d", waitResp.StatusCode)
	}
	if err := json.NewDecoder(waitResp.Body).Decode(&waitResult); err != nil {
		return nil, fmt.Errorf("container wait decode: %w", err)
	}

	// 4. Collect logs
	stdout, stderr, err := c.collectLogs(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("container result unavailable: %w", err)
	}

	return &Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: waitResult.StatusCode,
	}, nil
}

func (c *ContainerSandbox) collectLogs(ctx context.Context, containerID string) (stdout, stderr string, err error) {
	stdoutResp, err := c.dockerAPI(ctx, http.MethodGet, fmt.Sprintf("/%s/containers/%s/logs?stdout=1&stderr=0", c.apiVersion, containerID), nil)
	if err != nil {
		return "", "", fmt.Errorf("read container stdout: %w", err)
	}
	if stdoutResp.StatusCode != http.StatusOK {
		stdoutResp.Body.Close()
		return "", "", fmt.Errorf("read container stdout: HTTP %d", stdoutResp.StatusCode)
	}
	stdoutBytes, err := io.ReadAll(io.LimitReader(stdoutResp.Body, 1<<20+1))
	stdoutResp.Body.Close()
	if err != nil {
		return "", "", fmt.Errorf("read container stdout: %w", err)
	}
	if len(stdoutBytes) > 1<<20 {
		return "", "", fmt.Errorf("container stdout exceeds 1 MiB receipt limit")
	}

	stderrResp, err := c.dockerAPI(ctx, http.MethodGet, fmt.Sprintf("/%s/containers/%s/logs?stdout=0&stderr=1", c.apiVersion, containerID), nil)
	if err != nil {
		return stripDockerLogHeaders(stdoutBytes), "", fmt.Errorf("read container stderr: %w", err)
	}
	if stderrResp.StatusCode != http.StatusOK {
		stderrResp.Body.Close()
		return stripDockerLogHeaders(stdoutBytes), "", fmt.Errorf("read container stderr: HTTP %d", stderrResp.StatusCode)
	}
	stderrBytes, err := io.ReadAll(io.LimitReader(stderrResp.Body, 1<<20+1))
	stderrResp.Body.Close()
	if err != nil {
		return stripDockerLogHeaders(stdoutBytes), "", fmt.Errorf("read container stderr: %w", err)
	}
	if len(stderrBytes) > 1<<20 {
		return stripDockerLogHeaders(stdoutBytes), "", fmt.Errorf("container stderr exceeds 1 MiB receipt limit")
	}

	return stripDockerLogHeaders(stdoutBytes), stripDockerLogHeaders(stderrBytes), nil
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
	resp, err := c.dockerAPI(ctx, http.MethodDelete, fmt.Sprintf("/%s/containers/%s?force=true", c.apiVersion, containerID), nil)
	if err == nil {
		resp.Body.Close()
	}
}

func (c *ContainerSandbox) Close() error { return nil }

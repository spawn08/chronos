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

// ContainerSandbox implements Sandbox using Docker Engine API for production-grade isolation.
type ContainerSandbox struct {
	Image    string
	client   *http.Client
	sockPath string
	// Resource limits
	MemoryBytes int64
	CPUQuota    int64
	NetworkMode string
}

// ContainerConfig holds container sandbox configuration.
type ContainerConfig struct {
	Image       string
	SocketPath  string
	MemoryBytes int64
	CPUQuota    int64
	NetworkMode string
}

// NewContainerSandbox creates a Docker-based sandbox.
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

	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", cfg.SocketPath)
		},
	}

	return &ContainerSandbox{
		Image:       cfg.Image,
		sockPath:    cfg.SocketPath,
		MemoryBytes: cfg.MemoryBytes,
		CPUQuota:    cfg.CPUQuota,
		NetworkMode: cfg.NetworkMode,
		client: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
		},
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
	createBody := map[string]any{
		"Image":           c.Image,
		"Cmd":             cmd,
		"AttachStdout":    true,
		"AttachStderr":    true,
		"NetworkDisabled": c.NetworkMode == "none",
		"HostConfig": map[string]any{
			"Memory":         c.MemoryBytes,
			"CpuQuota":       c.CPUQuota,
			"NetworkMode":    c.NetworkMode,
			"AutoRemove":     false,
			"ReadonlyRootfs": true,
		},
	}

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
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return nil, fmt.Errorf("container create decode: %w", err)
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

func (c *ContainerSandbox) collectLogs(ctx context.Context, containerID string) (string, string) {
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

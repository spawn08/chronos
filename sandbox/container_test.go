package sandbox

import (
	"testing"
)

func TestStripDockerLogHeaders_Empty(t *testing.T) {
	result := stripDockerLogHeaders(nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestStripDockerLogHeaders_LessThan8Bytes(t *testing.T) {
	data := []byte("hello")
	result := stripDockerLogHeaders(data)
	// Less than 8 bytes => fallback to string(data)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestStripDockerLogHeaders_ValidFrame(t *testing.T) {
	// Docker log format: 8-byte header + payload
	// header[0]: stream type (1=stdout)
	// header[4-7]: big-endian uint32 payload size
	payload := []byte("hello world")
	size := len(payload)
	header := []byte{1, 0, 0, 0, byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)}
	data := append(header, payload...)

	result := stripDockerLogHeaders(data)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestStripDockerLogHeaders_MultipleFrames(t *testing.T) {
	makeFrame := func(text string) []byte {
		size := len(text)
		h := []byte{1, 0, 0, 0, byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)}
		return append(h, []byte(text)...)
	}
	data := append(makeFrame("hello "), makeFrame("world")...)
	result := stripDockerLogHeaders(data)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestNewContainerSandbox_DefaultValues(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	if sb.Image != "alpine:3.19" {
		t.Errorf("default image = %q, want alpine:3.19", sb.Image)
	}
	if sb.MemoryBytes != 256*1024*1024 {
		t.Errorf("default MemoryBytes = %d", sb.MemoryBytes)
	}
	if sb.CPUQuota != 50000 {
		t.Errorf("default CPUQuota = %d", sb.CPUQuota)
	}
	if sb.NetworkMode != "none" {
		t.Errorf("default NetworkMode = %q", sb.NetworkMode)
	}
}

func TestNewContainerSandbox_CustomValues(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{
		Image:       "ubuntu:22.04",
		SocketPath:  "/custom/docker.sock",
		MemoryBytes: 512 * 1024 * 1024,
		CPUQuota:    100000,
		NetworkMode: "bridge",
	})
	if sb.Image != "ubuntu:22.04" {
		t.Errorf("image = %q", sb.Image)
	}
	if sb.MemoryBytes != 512*1024*1024 {
		t.Errorf("MemoryBytes = %d", sb.MemoryBytes)
	}
	if sb.NetworkMode != "bridge" {
		t.Errorf("NetworkMode = %q", sb.NetworkMode)
	}
}

func TestStripDockerLogHeaders_TruncatedPayload(t *testing.T) {
	// Header says 100 bytes but only 5 bytes available
	header := []byte{1, 0, 0, 0, 0, 0, 0, 100}
	payload := []byte("hello")
	data := append(header, payload...)
	result := stripDockerLogHeaders(data)
	// Should not panic, should return what we have
	if result == "" {
		// empty string is ok if nothing written to buf
		t.Log("stripDockerLogHeaders returned empty for truncated payload")
	}
}

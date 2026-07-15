package sandbox

import (
	"reflect"
	"testing"
)

// TestBuildCreateBody_HardenedDefaults asserts the hardened container profile
// (P2-001) is present in the generated Docker create body. It requires no
// running daemon.
func TestBuildCreateBody_HardenedDefaults(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	body := sb.buildCreateBody([]string{"echo", "hi"})

	// Non-root user.
	if body["User"] != defaultContainerUser {
		t.Errorf("User = %v, want %q", body["User"], defaultContainerUser)
	}
	if body["NetworkDisabled"] != true {
		t.Errorf("NetworkDisabled = %v, want true (network mode none)", body["NetworkDisabled"])
	}

	hc, ok := body["HostConfig"].(map[string]any)
	if !ok {
		t.Fatalf("HostConfig type = %T", body["HostConfig"])
	}

	// CapDrop ALL.
	capDrop, ok := hc["CapDrop"].([]string)
	if !ok || len(capDrop) != 1 || capDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", hc["CapDrop"])
	}

	// no-new-privileges and default seccomp (no seccomp=unconfined).
	secOpt, ok := hc["SecurityOpt"].([]string)
	if !ok {
		t.Fatalf("SecurityOpt type = %T", hc["SecurityOpt"])
	}
	if !containsString(secOpt, "no-new-privileges:true") {
		t.Errorf("SecurityOpt = %v, want no-new-privileges:true", secOpt)
	}
	for _, o := range secOpt {
		if o == "seccomp=unconfined" {
			t.Errorf("SecurityOpt must not disable seccomp: %v", secOpt)
		}
	}

	// Read-only rootfs.
	if hc["ReadonlyRootfs"] != true {
		t.Errorf("ReadonlyRootfs = %v, want true", hc["ReadonlyRootfs"])
	}
	// Not privileged.
	if hc["Privileged"] != false {
		t.Errorf("Privileged = %v, want false", hc["Privileged"])
	}
	// Pids limit.
	if hc["PidsLimit"] != int64(defaultPidsLimit) {
		t.Errorf("PidsLimit = %v, want %d", hc["PidsLimit"], defaultPidsLimit)
	}
	// Memory/swap and CPU limits.
	if hc["Memory"] != int64(256*1024*1024) {
		t.Errorf("Memory = %v", hc["Memory"])
	}
	if hc["MemorySwap"] != hc["Memory"] {
		t.Errorf("MemorySwap = %v, want == Memory %v", hc["MemorySwap"], hc["Memory"])
	}
	if hc["CpuQuota"] != int64(50000) {
		t.Errorf("CpuQuota = %v", hc["CpuQuota"])
	}
	// Ulimits present for nofile and nproc.
	ulimits, ok := hc["Ulimits"].([]map[string]any)
	if !ok || len(ulimits) != 2 {
		t.Fatalf("Ulimits = %v, want 2 entries", hc["Ulimits"])
	}
	names := map[string]bool{}
	for _, u := range ulimits {
		names[u["Name"].(string)] = true
	}
	if !names["nofile"] || !names["nproc"] {
		t.Errorf("Ulimits names = %v, want nofile and nproc", names)
	}
	// Tmpfs /tmp for a writable scratch dir with a read-only rootfs.
	tmpfs, ok := hc["Tmpfs"].(map[string]string)
	if !ok || tmpfs[defaultTmpfsMount] == "" {
		t.Errorf("Tmpfs = %v, want %s mount", hc["Tmpfs"], defaultTmpfsMount)
	}
	// Runtime omitted by default (uses daemon default runc).
	if _, present := hc["Runtime"]; present {
		t.Errorf("Runtime should be omitted by default, got %v", hc["Runtime"])
	}
	// CapAdd omitted when empty.
	if _, present := hc["CapAdd"]; present {
		t.Errorf("CapAdd should be omitted when empty, got %v", hc["CapAdd"])
	}
}

func TestBuildCreateBody_Overrides(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{
		User:           "1000:1000",
		PidsLimit:      64,
		NofileLimit:    2048,
		NprocLimit:     32,
		CapAdd:         []string{"NET_BIND_SERVICE"},
		SeccompProfile: `{"defaultAction":"SCMP_ACT_ERRNO"}`,
		Runtime:        "runsc",
		WritableRootfs: true,
		Tmpfs:          map[string]string{"/scratch": "rw,size=1m"},
	})
	body := sb.buildCreateBody([]string{"sh"})
	hc := body["HostConfig"].(map[string]any)

	if body["User"] != "1000:1000" {
		t.Errorf("User = %v", body["User"])
	}
	if hc["ReadonlyRootfs"] != false {
		t.Errorf("ReadonlyRootfs = %v, want false (WritableRootfs)", hc["ReadonlyRootfs"])
	}
	if hc["PidsLimit"] != int64(64) {
		t.Errorf("PidsLimit = %v", hc["PidsLimit"])
	}
	if hc["Runtime"] != "runsc" {
		t.Errorf("Runtime = %v, want runsc", hc["Runtime"])
	}
	capAdd, _ := hc["CapAdd"].([]string)
	if !reflect.DeepEqual(capAdd, []string{"NET_BIND_SERVICE"}) {
		t.Errorf("CapAdd = %v", hc["CapAdd"])
	}
	secOpt := hc["SecurityOpt"].([]string)
	if !containsString(secOpt, `seccomp={"defaultAction":"SCMP_ACT_ERRNO"}`) {
		t.Errorf("SecurityOpt missing custom seccomp profile: %v", secOpt)
	}
	tmpfs := hc["Tmpfs"].(map[string]string)
	if tmpfs["/scratch"] == "" {
		t.Errorf("Tmpfs = %v, want /scratch override", tmpfs)
	}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

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
	data := make([]byte, 0, len(header)+len(payload))
	data = append(data, header...)
	data = append(data, payload...)

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
	data := make([]byte, 0, len(header)+len(payload))
	data = append(data, header...)
	data = append(data, payload...)
	result := stripDockerLogHeaders(data)
	// Should not panic, should return what we have
	if result == "" {
		t.Log("stripDockerLogHeaders returned empty for truncated payload")
	}
}

func TestStripDockerLogHeaders_EmptySlice(t *testing.T) {
	result := stripDockerLogHeaders([]byte{})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestStripDockerLogHeaders_ExactlyEightBytes(t *testing.T) {
	header := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	result := stripDockerLogHeaders(header)
	if result != "" {
		t.Errorf("expected empty for zero-length frame, got %q", result)
	}
}

func TestStripDockerLogHeaders_SingleBytePayload(t *testing.T) {
	header := []byte{1, 0, 0, 0, 0, 0, 0, 1}
	data := make([]byte, 0, len(header)+1)
	data = append(data, header...)
	data = append(data, 'X')
	result := stripDockerLogHeaders(data)
	if result != "X" {
		t.Errorf("expected 'X', got %q", result)
	}
}

func TestNewContainerSandbox_DefaultSocketPath(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	if sb.sockPath != "/var/run/docker.sock" {
		t.Errorf("default socket = %q", sb.sockPath)
	}
}

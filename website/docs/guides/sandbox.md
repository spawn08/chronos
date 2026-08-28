---
title: "Sandbox"
---

Agents call tools, and tools sometimes run code you didn't write: a model-generated shell command, a skill fetched from a registry, or a user-supplied script. The `sandbox` package isolates that execution so a misbehaving or malicious command can't take down the host process, exhaust its resources, or reach the network and filesystem it shouldn't.

Reach for a sandbox whenever a tool's `Handler` shells out, evaluates code, or otherwise executes something outside your own trusted codebase. If a tool only calls your own Go functions or a well-scoped HTTP API, you don't need one.

## The `Sandbox` Interface

Every backend implements the same interface:

```go
type Sandbox interface {
    Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error)
    Close() error
}

type Result struct {
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
    ExitCode int    `json:"exit_code"`
}
```

A non-zero program exit is reported as `Result.ExitCode` with a `nil` error — that's the program working correctly and telling you it failed. An `error` return means the sandbox itself failed to run the command (timeout at the orchestration level, daemon unreachable, invalid module, etc.).

## Choosing a Backend

| Backend | Constant | Isolation | Requires |
|---------|----------|-----------|----------|
| Process | `sandbox.BackendProcess` | OS process + timeout + own process group | nothing |
| Container | `sandbox.BackendContainer` | Docker with a hardened profile | a Docker daemon |
| WASM | `sandbox.BackendWASM` | WebAssembly (WASI) linear-memory sandbox | a `.wasm` module |
| Kubernetes Job | `sandbox.BackendK8sJob` | one-shot hardened Job per run | a reachable cluster |

Construct any backend either with its dedicated constructor (`sandbox.NewProcessSandbox`, `sandbox.NewContainerSandbox`, ...) or through the config-driven factory:

```go
import (
    "github.com/spawn08/chronos/sandbox"
)

sb, err := sandbox.NewFromConfig(sandbox.Config{Backend: sandbox.BackendProcess})
if err != nil {
    // ...
}
defer sb.Close()
```

`Config` covers every backend's required fields in one struct:

| Field | Type | Used by |
|-------|------|---------|
| `Backend` | `sandbox.Backend` | all — selects the backend; empty defaults to `BackendProcess` |
| `WorkDir` | `string` | process — working directory; empty defaults to `.` |
| `Image` | `string` | container, k8s — container image; **required** for k8s |
| `Network` | `string` | container — network mode |
| `Runtime` | `string` | container — OCI runtime (e.g. `"runsc"`, `"kata-runtime"`) |
| `Namespace` | `string` | k8s — Job namespace |
| `ServiceAcc` | `string` | k8s — pod service account |
| `WASMPath` | `string` | wasm — path to the `.wasm` module; **required** for wasm |

`sandbox.ParseBackend(s string) Backend` normalizes user-supplied strings (`"docker"` → `BackendContainer`, `"wasi"` → `BackendWASM`, `"k8s"`/`"kubernetes"`/`"job"` → `BackendK8sJob`, etc.) — handy for config files or CLI flags.

---

## Process Backend

Runs the command as a child process with a timeout. The child is placed in its own process group (on Unix) so a timeout reaps the whole tree, not just the direct child.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/spawn08/chronos/sandbox"
)

func main() {
    ctx := context.Background()

    sb := sandbox.NewProcessSandbox("/tmp/work")
    defer sb.Close()

    res, err := sb.Execute(ctx, "echo", []string{"hello"}, 5*time.Second)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Stdout, res.ExitCode)
}
```

| Field | Type | Description |
|-------|------|--------------|
| `WorkDir` | `string` | Directory the command runs in |

**Trade-offs:** zero setup, lowest latency. It applies **no** resource limits, no filesystem or network restriction, and no privilege drop — it only bounds wall-clock time. Use it for trusted or semi-trusted commands (your own scripts, deterministic build steps), never for arbitrary model-generated or third-party code in production.

---

## Container Backend

Runs each command in a Docker container with a hardened default profile applied automatically: non-root user, all Linux capabilities dropped, `no-new-privileges`, the daemon's default seccomp profile (unless overridden), a pids limit, file/process ulimits, a read-only root filesystem backed by a small writable tmpfs `/tmp`, and memory/CPU limits. It talks to the Docker Engine API directly over the daemon socket — no `docker` CLI required.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/spawn08/chronos/sandbox"
)

func main() {
    ctx := context.Background()

    sb := sandbox.NewContainerSandbox(sandbox.ContainerConfig{
        Image:       "alpine:3.19",
        MemoryBytes: 256 << 20, // 256 MiB
        NetworkMode: "none",
        Runtime:     "runsc", // optional: gVisor
    })
    defer sb.Close()

    res, err := sb.Execute(ctx, "sh", []string{"-c", "echo hi"}, 30*time.Second)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Stdout, res.ExitCode)
}
```

`ContainerConfig` fields and their hardened defaults (a zero value falls back to the default):

| Field | Type | Default | Description |
|-------|------|---------|--------------|
| `Image` | `string` | `alpine:3.19` | Container image to run |
| `SocketPath` | `string` | `/var/run/docker.sock` | Docker daemon socket |
| `MemoryBytes` | `int64` | `256 MiB` | Memory limit (swap is disabled and capped to the same value) |
| `CPUQuota` | `int64` | `50000` | Docker CPU quota per 100ms period (50000 ≈ 50% of one core) |
| `NetworkMode` | `string` | `none` | Docker network mode |
| `User` | `string` | `65534:65534` | Non-root uid:gid the workload runs as |
| `PidsLimit` | `int64` | `128` | Max processes inside the container |
| `NofileLimit` | `int64` | `1024` | `RLIMIT_NOFILE` (open file descriptors) |
| `NprocLimit` | `int64` | `128` | `RLIMIT_NPROC` (processes/threads) |
| `CapAdd` | `[]string` | none | Capabilities to re-add on top of `CapDrop: ALL` |
| `SeccompProfile` | `string` | daemon default | Custom seccomp profile JSON |
| `Runtime` | `string` | daemon default (`runc`) | Hardened OCI runtime, e.g. `"runsc"` (gVisor) or `"kata-runtime"` |
| `Tmpfs` | `map[string]string` | `{"/tmp": "rw,noexec,nosuid,nodev,size=64m"}` | Writable tmpfs mounts |
| `WritableRootfs` | `bool` | `false` | Set `true` to disable the read-only rootfs hardening |

Requires a running Docker daemon reachable at `SocketPath` (default `/var/run/docker.sock`).

**Trade-offs:** real kernel/namespace isolation with a safe-by-default profile, at the cost of container start/stop latency (typically tens to hundreds of milliseconds) and an operational dependency on Docker. Pair it with `sandbox.NewPool` (below) to amortize cold-start cost. For stronger isolation against kernel exploits, set `Runtime: "runsc"` (gVisor) or `"kata-runtime"` if installed on the host.

### Warm Pool (`sandbox.NewPool`)

`sandbox.NewPool` maintains a pool of pre-created sandboxes behind any factory function — it's most useful for the container backend, where cold start dominates latency, but it is not container-specific: the factory can return any `Sandbox` implementation.

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/spawn08/chronos/sandbox"
)

func main() {
    ctx := context.Background()

    cfg := sandbox.ContainerConfig{Image: "alpine:3.19"}
    pool, err := sandbox.NewPool(sandbox.PoolConfig{
        MaxSize:     5,
        MaxIdleTime: 5 * time.Minute, // idle sandboxes are reclaimed lazily
        Factory:     func() (sandbox.Sandbox, error) { return sandbox.NewContainerSandbox(cfg), nil },
    })
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    if err := pool.Warmup(ctx, 3); err != nil {
        log.Fatal(err)
    }

    sb, err := pool.Acquire()
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Release(sb)

    if _, err := sb.Execute(ctx, "echo", []string{"warm"}, 5*time.Second); err != nil {
        log.Fatal(err)
    }
}
```

| Field | Type | Default | Description |
|-------|------|---------|--------------|
| `MaxSize` | `int` | `5` | Maximum sandboxes kept in the pool |
| `MaxIdleTime` | `time.Duration` | `5m` | How long an idle sandbox is kept before being closed |
| `Factory` | `func() (sandbox.Sandbox, error)` | — | **Required.** Creates a new sandbox instance |

`Warmup(ctx, n)` pre-creates up to `n` (capped at `MaxSize`) sandboxes. `Acquire()` returns a warm sandbox if one is available, otherwise creates a new one on demand. `Release(sb)` returns it to the pool (or closes it if the pool is full). Idle sandboxes past `MaxIdleTime` are evicted lazily on the next `Acquire`/`Release`, so there's no background goroutine. `Size()` and `InUse()` report pool occupancy; `Close()` shuts down every pooled and in-use sandbox.

---

## WASM (WASI) Backend

Runs a WebAssembly module compiled to WASI using the pure-Go [wazero](https://wazero.io) runtime — no CGo, no external runtime, no daemon. WebAssembly isolates by construction: the module executes in its own linear memory and reaches the host only through capabilities the sandbox explicitly grants. By default it gets stdio and argv and nothing else — no filesystem, network, environment, or clock access.

The module is compiled once at construction (an invalid module is rejected immediately, not on first `Execute`) and instantiated fresh for every `Execute` call, so runs never share state.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/spawn08/chronos/sandbox"
)

func main() {
    ctx := context.Background()

    sb, err := sandbox.NewWASMSandboxWithConfig(sandbox.WASMConfig{
        WASMPath:         "prog.wasm",
        MemoryLimitPages: 2048,            // 2048 x 64 KiB = 128 MiB
        FSDir:            "/data/scratch", // mounted read-write at "/"
    })
    if err != nil {
        log.Fatal(err) // invalid module or unreadable file
    }
    defer sb.Close()

    res, err := sb.Execute(ctx, "prog", []string{"arg1"}, 10*time.Second)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Stdout, res.ExitCode)
}
```

`sandbox.NewWASMSandbox(wasmPath string)` is a shorthand for `NewWASMSandboxWithConfig(WASMConfig{WASMPath: wasmPath})` with default limits.

| Field | Type | Default | Description |
|-------|------|---------|--------------|
| `WASMPath` | `string` | — | **Required.** Path to the `.wasm` module |
| `MemoryLimitPages` | `uint32` | `4096` (256 MiB, 64 KiB/page) | Caps the module's linear memory |
| `FSDir` | `string` | `""` (no filesystem access) | Directory mounted read-write at the module's root `"/"` |

Build a WASI module from Go and try it:

```bash
# Any language that targets WASI works; Go 1.21+ does out of the box:
GOOS=wasip1 GOARCH=wasm go build -o prog.wasm ./yourprog

# examples/wasm_sandbox compiles a demo module for you if you pass no path:
go run ./examples/wasm_sandbox/
# ...or run your own:
go run ./examples/wasm_sandbox/ prog.wasm
```

**Trade-offs:** the fastest strong-isolation option — no daemon, no container runtime, sub-millisecond instantiation — but your workload must be compiled to WASI. That rules out arbitrary shell commands or existing native binaries unless you first compile them (or an interpreter for them) to WebAssembly.

---

## Kubernetes Job Backend

Runs each command as a one-shot Kubernetes `Job`. The pod uses a hardened security context — non-root (`RunAsUser: 65534`), all capabilities dropped, no privilege escalation, read-only root filesystem — with `restartPolicy: Never` and `backoffLimit: 0` (one shot only, no retries). The backend waits for the Job to finish (polling once a second), collects the pod's logs and exit code, and deletes the Job; a 60-second server-side TTL also garbage-collects it if deletion is missed.

Credentials resolve the same way `kubectl` does: the in-cluster service account when running inside a pod, otherwise the local kubeconfig (`KUBECONFIG` or `~/.kube/config`). Construction fails fast if no cluster is reachable, so a misconfigured environment errors immediately rather than at the first `Execute`.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/spawn08/chronos/sandbox"
)

func main() {
    ctx := context.Background()

    sb, err := sandbox.NewFromConfig(sandbox.Config{
        Backend:    sandbox.BackendK8sJob,
        Image:      "alpine:3.19", // must be pullable by the cluster
        Namespace:  "sandboxes",
        ServiceAcc: "sandbox-runner",
    })
    if err != nil {
        log.Fatal(err) // no reachable cluster
    }
    defer sb.Close()

    res, err := sb.Execute(ctx, "sh", []string{"-c", "echo hi"}, 2*time.Minute)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Stdout, res.ExitCode)
}
```

You can also construct it directly with `sandbox.NewK8sJobSandbox(sandbox.K8sJobConfig{...})`, which `NewFromConfig` calls internally:

| Field | Type | Default | Description |
|-------|------|---------|--------------|
| `Image` | `string` | — | **Required.** Image the Job's pod runs |
| `Namespace` | `string` | `default` | Namespace Jobs are created in |
| `ServiceAccount` | `string` | namespace default | Pod service account |

The `timeout` passed to `Execute` bounds the run both client-side (via `ctx`) and server-side (via the Job's `activeDeadlineSeconds`).

Try it against a local cluster:

```bash
kind create cluster        # or: minikube start / k3d cluster create
kubectl get nodes          # confirm connectivity
go run ./examples/k8s_sandbox/
```

**Trade-offs:** the strongest isolation and the best fit if you already run agent workloads on Kubernetes — each execution is a fully isolated pod with its own hardened security context, schedulable across your cluster's capacity. The cost is latency (Job creation, scheduling, and image pull typically add seconds, not milliseconds) and requiring a reachable cluster; it's a poor fit for short, high-frequency tool calls.

## Working Examples

Runnable, self-contained versions of every backend above live under `examples/`:

```bash
go run ./examples/sandbox_execution/   # process backend
go run ./examples/wasm_sandbox/        # WASM/WASI backend
go run ./examples/k8s_sandbox/         # Kubernetes Job backend
```

There is no dedicated `examples/` program for the container backend beyond the snippets above — copy the Container Backend example and point it at a running Docker daemon.

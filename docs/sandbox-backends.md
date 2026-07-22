# Sandbox Backends

Chronos runs untrusted tool/skill code through the `sandbox.Sandbox` interface:

```go
type Sandbox interface {
    Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error)
    Close() error
}
```

Pick a backend with `sandbox.NewFromConfig` or a backend constructor. Four backends ship today.

| Backend | Constant | Isolation | Needs |
|---------|----------|-----------|-------|
| Process | `BackendProcess` | OS process + timeout + own process group | nothing |
| Container | `BackendContainer` | Docker with a hardened profile | Docker daemon |
| WASM | `BackendWASM` | WebAssembly (WASI) linear-memory sandbox | a `.wasm` module |
| Kubernetes Job | `BackendK8sJob` | One-shot hardened Job per run | a cluster |

```go
sb, err := sandbox.NewFromConfig(sandbox.Config{Backend: sandbox.BackendProcess})
if err != nil { /* ... */ }
defer sb.Close()

res, err := sb.Execute(ctx, "echo", []string{"hello"}, 5*time.Second)
// res.Stdout, res.Stderr, res.ExitCode
```

A non-zero program exit is reported in `Result.ExitCode` with a `nil` error; only host/orchestration failures return an error.

---

## Process backend

Runs the command as a child process with a timeout. The child is placed in its own process group (on Unix) so a timeout reaps the whole tree, not just the direct child. It applies **no** resource limits — use the container or Kubernetes backend for genuinely untrusted code in production.

```go
sb := sandbox.NewProcessSandbox("/work/dir")
```

Run the example:

```bash
go run ./examples/sandbox_execution/
```

---

## Container backend

Runs each command in a Docker container with a hardened default profile: non-root user, all Linux capabilities dropped, `no-new-privileges`, the daemon's default seccomp profile, a pids limit, file/process ulimits, a read-only root filesystem backed by a small tmpfs `/tmp`, and memory/CPU limits. Optionally selects a stronger OCI runtime (`runsc` for gVisor, `kata-runtime`).

```go
sb := sandbox.NewContainerSandbox(sandbox.ContainerConfig{
    Image:       "alpine:3.19",
    MemoryBytes: 256 << 20, // 256 MiB
    NetworkMode: "none",
    Runtime:     "runsc",   // optional: gVisor
})
```

For throughput, keep a warm pool:

```go
pool, _ := sandbox.NewPool(sandbox.PoolConfig{
    MaxSize:     5,
    MaxIdleTime: 5 * time.Minute, // idle containers are reclaimed lazily
    Factory:     func() (sandbox.Sandbox, error) { return sandbox.NewContainerSandbox(cfg), nil },
})
_ = pool.Warmup(ctx, 3)
sb, _ := pool.Acquire()
defer pool.Release(sb)
```

Requires a running Docker daemon (default socket `/var/run/docker.sock`).

---

## WASM (WASI) backend

Runs a WebAssembly module compiled to WASI using the pure-Go [wazero](https://wazero.io) runtime — no CGo, no external runtime. WebAssembly isolates by construction: the module executes in its own linear memory and reaches the host only through granted capabilities. By default it gets stdio and argv and nothing else (no filesystem, network, env, or clock).

The module is compiled once at construction (an invalid module is rejected immediately) and instantiated fresh per `Execute`.

```go
sb, err := sandbox.NewWASMSandbox("prog.wasm")
if err != nil { /* invalid module or unreadable file */ }
defer sb.Close()

res, _ := sb.Execute(ctx, "prog", []string{"arg1"}, 10*time.Second)
```

Optional configuration (memory cap, a mounted directory):

```go
sb, _ := sandbox.NewWASMSandboxWithConfig(sandbox.WASMConfig{
    WASMPath:         "prog.wasm",
    MemoryLimitPages: 2048, // 2048 × 64 KiB = 128 MiB
    FSDir:            "/data/scratch", // mounted read-write at "/"
})
```

Build a WASI module from Go and run the example:

```bash
# Any language that targets WASI works; Go 1.21+ does out of the box:
GOOS=wasip1 GOARCH=wasm go build -o prog.wasm ./yourprog

# The example compiles a demo module for you if you pass no path:
go run ./examples/wasm_sandbox/
# ...or run your own:
go run ./examples/wasm_sandbox/ prog.wasm
```

---

## Kubernetes Job backend

Runs each command as a one-shot Kubernetes `Job`. The pod uses a hardened security context — non-root (`RunAsUser: 65534`), all capabilities dropped, no privilege escalation, read-only root filesystem — and `restartPolicy: Never` with `backoffLimit: 0` (one shot). The backend waits for completion, collects the pod's logs and exit code, and deletes the Job (a 60s server-side TTL also GCs it).

Credentials resolve like `kubectl`: the in-cluster service account when running inside a pod, otherwise the local kubeconfig (`KUBECONFIG` or `~/.kube/config`). Construction fails fast if no cluster is reachable.

```go
sb, err := sandbox.NewFromConfig(sandbox.Config{
    Backend:    sandbox.BackendK8sJob,
    Image:      "alpine:3.19",   // must be pullable by the cluster
    Namespace:  "sandboxes",
    ServiceAcc: "sandbox-runner",
})
if err != nil { /* no reachable cluster */ }
defer sb.Close()

res, _ := sb.Execute(ctx, "sh", []string{"-c", "echo hi"}, 2*time.Minute)
```

Run the example (start a local cluster first):

```bash
kind create cluster        # or: minikube start / k3d cluster create
kubectl get nodes          # confirm connectivity
go run ./examples/k8s_sandbox/
```

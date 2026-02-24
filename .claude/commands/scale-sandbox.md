Extend container-based sandbox isolation for Chronos (high-scalability sandbox).

## Context

Chronos has two sandbox implementations in `sandbox/`:
- **ProcessSandbox** (`sandbox.go`) — Subprocess execution with timeout (local/dev default)
- **ContainerSandbox** (`container.go`) — Docker Engine API-based isolation with memory/CPU limits

## Instructions

1. **Review existing**: Read `sandbox/container.go` to understand the current ContainerSandbox implementation (Docker Engine API, resource limits, cleanup).
2. **Extend or fix** based on $ARGUMENTS. Common tasks:
   - **Kubernetes Job backend**: Implement a sandbox that submits a K8s Job and streams logs back; poll or watch for completion. Create `sandbox/k8s.go`.
   - **Container pooling**: Add a pool of pre-warmed containers to reduce cold-start latency. Configurable pool size, idle timeout, and image.
   - **Network isolation**: Add network policy support (no-network mode, allowlist mode).
   - **Volume mounts**: Support read-only bind mounts for input data; ephemeral writable mounts for output.
   - **Image management**: Support custom runner images, image pull policies, and registry auth.
3. **Interface**: All implementations must satisfy `sandbox.Sandbox`: `Execute(ctx, command, args, timeout) (Result, error)` and `Close() error`.
4. **Integration**: Keep `ProcessSandbox` as the default. Container/K8s sandbox selected via config or constructor.
5. **Tests**: Add tests that skip if Docker/K8s is unavailable. Run `go build ./...` and `go test ./sandbox/...`.

If no specific extension is requested, review the existing ContainerSandbox for completeness and suggest improvements.

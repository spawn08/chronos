Implement or extend container-based sandbox isolation for Chronos (high-scalability sandbox).

## Context

The current sandbox in `sandbox/sandbox.go` is `ProcessSandbox` — subprocess execution with timeout. For high-scalability and security, we need container-based isolation.

## Instructions

1. **Interface**: Ensure `sandbox.Sandbox` interface remains: `Execute(ctx, req) (SandboxResult, error)` and `Close() error`. Any new implementation (e.g. `ContainerSandbox`) must satisfy this interface.
2. **Container backend**: Implement a sandbox that runs user code inside a container (Docker or containerd). Options:
   - **Docker**: Use Docker API (e.g. create container from image, attach stdin/stdout, set resource limits, remove on exit). Image can be a minimal runtime (e.g. alpine or a Chronos runner image).
   - **Kubernetes Job**: For distributed execution, implement a sandbox that submits a K8s Job and streams logs back; poll or watch for completion.
3. **Resource limits**: Support configurable CPU (cores or quota), memory (limit), and optional disk/network. Pass these to the container or Job spec.
4. **Isolation**: Do not share host filesystem by default; use volumes only where explicitly allowed. Consider read-only root or minimal capabilities.
5. **Pooling (optional)**: To reduce cold-start, maintain a small pool of pre-warmed containers that accept execution requests; document how to enable/disable and size the pool.
6. **Integration**: Keep `ProcessSandbox` as the default for local/dev. Container sandbox can be selected via config or constructor (e.g. `sandbox.NewContainerSandbox(dockerClient, opts)`). Do not break existing callers of `ProcessSandbox`.
7. **Tests**: Add tests that either use a real Docker socket (skip if unavailable) or mock the container backend. Run `go build ./...` and `go test ./sandbox/...`.

If the user specified a particular backend (e.g. "Docker only" or "Kubernetes Job"), implement that; otherwise implement Docker-based sandbox first and note K8s as a follow-up.

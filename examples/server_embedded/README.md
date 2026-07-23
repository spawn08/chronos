# Embedded ChronosOS server

Run the ChronosOS control plane as a **library** inside your own Go binary,
instead of via the `chronos serve` CLI.

`main.go` builds a `chronosos.Server` with:

- **API-key auth** (`WithAPIKeyAuth`) on `/api/*` routes — health, metrics, and
  Swagger stay public.
- **RBAC** (`WithRBAC`) — method-based roles once authenticated.
- **Swagger UI** (`WithSwagger`) at `/swagger/`.

It starts the server and shuts it down gracefully on `SIGINT`/`SIGTERM`.

## Run

```bash
go run ./examples/server_embedded/          # serves on :8420 until Ctrl-C

curl http://localhost:8420/health/ready                         # 200, public
curl -i http://localhost:8420/api/sessions                      # 401, no key
curl -H "X-Api-Key: dev-secret-key" http://localhost:8420/api/sessions  # 200
open  http://localhost:8420/swagger/                            # OpenAPI UI
```

## Test

The test exercises the exact server the example builds **in-process** via
`net/http/httptest` — no port is bound, so it is CI- and network-safe:

```bash
go test ./examples/server_embedded/
```

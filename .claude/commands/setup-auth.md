Configure authentication, authorization, and security for ChronosOS server.

The security requirements are: $ARGUMENTS

## Instructions

1. Read the auth system at `os/auth/` and the server setup at `cli/cmd/serve.go`.

2. ChronosOS supports three auth mechanisms:

   | Mechanism | Env Var | Use Case |
   |-----------|---------|----------|
   | JWT tokens | `CHRONOS_JWT_SECRET` | User-facing APIs, SSO integration |
   | API keys | `CHRONOS_API_KEYS` | Service-to-service, programmatic access |
   | RBAC | `CHRONOS_RBAC` | Role-based access control per endpoint |

3. Configure the server environment:

```bash
# === Authentication ===
# Enable auth (required for JWT/API-key to take effect)
export CHRONOS_AUTH=true

# JWT Configuration
export CHRONOS_JWT_SECRET="your-secret-key-min-32-chars-long"
export CHRONOS_JWT_ISSUER="chronos"
export CHRONOS_JWT_AUDIENCE="chronos-api"
export CHRONOS_JWT_EXPIRY="24h"

# API Key Authentication (comma-separated list)
export CHRONOS_API_KEYS="key-1-xxxxxxxx,key-2-yyyyyyyy"

# === Authorization ===
# Enable RBAC
export CHRONOS_RBAC=true

# === CORS (for browser clients) ===
export CHRONOS_CORS_ORIGINS="https://app.example.com,https://admin.example.com"

# === Other Security ===
export CHRONOS_SWAGGER=false    # disable in production

# Start the server
go run ./cli/main.go serve :8420
```

4. API key usage — clients include the key in the Authorization header:

```bash
# Using API key
curl -H "Authorization: Bearer key-1-xxxxxxxx" \
     http://localhost:8420/api/sessions

# Using JWT
curl -H "Authorization: Bearer eyJhbGci..." \
     http://localhost:8420/api/sessions
```

5. For programmatic JWT token generation in Go:

```go
package main

import (
    "fmt"
    "time"

    "github.com/golang-jwt/jwt/v5"
)

func generateToken(secret, userID, role string) (string, error) {
    claims := jwt.MapClaims{
        "sub":  userID,
        "role": role,            // admin, operator, viewer
        "iss":  "chronos",
        "aud":  "chronos-api",
        "iat":  time.Now().Unix(),
        "exp":  time.Now().Add(24 * time.Hour).Unix(),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(secret))
}

func main() {
    token, err := generateToken("your-secret-key-min-32-chars-long", "user-1", "admin")
    if err != nil { panic(err) }
    fmt.Println(token)
}
```

6. Agent permission modes control tool execution security:

```yaml
agents:
  - id: "secure-agent"
    name: "Secure Agent"
    permission_mode: "strict"   # prompt | auto | strict
    # prompt: ask user before risky tools (default)
    # auto:   allow all tools without confirmation
    # strict: deny tools not explicitly in the allow list
    tools:
      - name: "file_read"
        permission: "allow"     # explicitly allowed
      - name: "shell"
        permission: "require_approval"  # needs approval handler
      - name: "file_write"
        permission: "deny"      # blocked entirely
```

7. Set up the approval service for human-in-the-loop tool execution:

```go
import "github.com/spawn08/chronos/os/approval"

// The approval service intercepts tools with require_approval permission
approvalSvc := approval.NewService(store)

// Register an approval handler
approvalSvc.OnApprovalRequired(func(ctx context.Context, req approval.Request) (bool, error) {
    // req.ToolName, req.Arguments, req.AgentID, req.SessionID
    // Implement your approval logic: Slack notification, email, webhook, etc.
    return true, nil // approve
})
```

8. Production security checklist:

```
[ ] CHRONOS_AUTH=true
[ ] JWT secret is at least 32 characters, randomly generated
[ ] API keys are rotated regularly
[ ] CHRONOS_SWAGGER=false in production
[ ] CORS origins are explicitly listed (not "*")
[ ] Database DSN uses SSL: ?sslmode=require
[ ] Permission mode is "strict" or "prompt" (never "auto" in prod)
[ ] Tools that access external systems use "require_approval"
[ ] Audit logging is enabled (tracing: true)
[ ] Server runs behind a reverse proxy (nginx/traefik) with TLS
```

9. For a production docker-compose with auth:

```yaml
services:
  chronos:
    image: chronos-agent:latest
    ports: ["8420:8420"]
    environment:
      CHRONOS_AUTH: "true"
      CHRONOS_JWT_SECRET: "${JWT_SECRET}"
      CHRONOS_API_KEYS: "${API_KEYS}"
      CHRONOS_RBAC: "true"
      CHRONOS_CORS_ORIGINS: "https://app.example.com"
      CHRONOS_SWAGGER: "false"
      DATABASE_URL: "postgres://chronos:${DB_PASSWORD}@db:5432/chronos?sslmode=require"
    depends_on: [db]

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: chronos
      POSTGRES_USER: chronos
      POSTGRES_PASSWORD: "${DB_PASSWORD}"
    volumes: ["pgdata:/var/lib/postgresql/data"]

volumes:
  pgdata:
```

10. Verify auth is working:
```bash
# Should return 401 without auth
curl -s -o /dev/null -w "%{http_code}" http://localhost:8420/api/sessions

# Should return 200 with valid key
curl -s -o /dev/null -w "%{http_code}" \
     -H "Authorization: Bearer key-1-xxxxxxxx" \
     http://localhost:8420/api/sessions
```

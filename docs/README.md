# Chronos Docs

In-repo guides for running specific features. The full documentation site lives at
[spawn08.github.io/chronos](https://spawn08.github.io/chronos/).

| Guide | Covers |
|-------|--------|
| [**Architecture (HTML)**](architecture/index.html) | Illustrated architecture site: layer diagram, feature catalog, sequence diagrams, and internals. Open the HTML files in a browser. |
| [Sandbox backends](sandbox-backends.md) | Process, Container (Docker), WASM (WASI), and Kubernetes Job isolation — with run steps |
| [MCP transports](mcp-transports.md) | Connecting to MCP servers over stdio and HTTP+SSE |
| [Eval suites](eval-suites.md) | Declaring and running evaluation suites from YAML or Go |

Every guide is runnable end-to-end against an example under [`examples/`](../examples/).

## Architecture site

The [`architecture/`](architecture/) folder is a small, self-contained HTML site (diagrams render
client-side via Mermaid over a CDN — no build step). Open it in a browser:

```bash
open docs/architecture/index.html            # macOS
# or serve the folder:
python3 -m http.server -d docs/architecture 8000  # then visit http://localhost:8000
```

| Page | Contents |
|------|----------|
| [index.html](architecture/index.html) | Four-layer architecture, request lifecycle, package map, design principles |
| [features.html](architecture/features.html) | Feature catalog grouped by layer |
| [flows.html](architecture/flows.html) | Sequence diagrams: agent chat, StateGraph, HITL, tools, MCP, queue, sandbox, requests |
| [internals.html](architecture/internals.html) | Interfaces, patterns, adapter matrix, providers, durability notes |

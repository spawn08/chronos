# Chronos Docs

In-repo guides for running specific features. The full documentation site lives at
[spawn08.github.io/chronos](https://spawn08.github.io/chronos/).

| Guide | Covers |
|-------|--------|
| [Sandbox backends](sandbox-backends.md) | Process, Container (Docker), WASM (WASI), and Kubernetes Job isolation — with run steps |
| [MCP transports](mcp-transports.md) | Connecting to MCP servers over stdio and HTTP+SSE |
| [Eval suites](eval-suites.md) | Declaring and running evaluation suites from YAML or Go |

Every guide is runnable end-to-end against an example under [`examples/`](../examples/).

## Architecture (illustrated)

The full illustrated architecture — layer stack, request/graph/queue flows, human-in-the-loop,
MCP transports, teams, storage, deployment, sandbox, and the core interfaces — lives on the
**docs website** (Docusaurus, Mermaid-rendered):

- Source: [`website/docs/reference/architecture.md`](../website/docs/reference/architecture.md)
- Published: <https://spawn08.github.io/chronos/reference/architecture/>

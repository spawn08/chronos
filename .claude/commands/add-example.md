Scaffold a new Chronos example agent.

The example name/description is: $ARGUMENTS

## Instructions

1. Create `examples/$ARGUMENTS/main.go`
2. Follow the pattern from `examples/quickstart/main.go`:
   - Import `context`, `fmt`, `log`
   - Import from `github.com/chronos-ai/chronos/...` packages
   - Open a SQLite store (for simplicity): `sqlite.New("$ARGUMENTS.db")`
   - Call `store.Migrate(ctx)`
   - Define a `graph.StateGraph` with relevant nodes
   - Build an agent with `agent.New(...)` using the builder pattern
   - Run the agent and print results
   - Keep it under 50 lines — concise and self-contained

3. The example should demonstrate the specific feature described in $ARGUMENTS. Common patterns:
   - **Tool usage**: register tools and show tool execution in a graph node
   - **Human-in-the-loop**: use `AddInterruptNode` and show pause/resume
   - **Knowledge/RAG**: create a `VectorKnowledge`, load docs, search in a node
   - **Teams**: use `team.New()` with multiple agents
   - **Guardrails**: add input/output guardrails to an agent
   - **Memory**: use `memory.Manager` for agentic memory

4. Add a `defer store.Close()` and handle errors properly
5. Run `go build ./...` and `go run ./examples/$ARGUMENTS/main.go` to verify

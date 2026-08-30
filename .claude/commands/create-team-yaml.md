Scaffold a multi-agent team YAML configuration for Chronos.

The team description is: $ARGUMENTS

## Instructions

1. Determine the best team strategy based on the description. Available strategies:

   | Strategy | Use When |
   |----------|----------|
   | `sequential` | Agents process in order, each building on the previous output |
   | `parallel` | Agents work independently on the same input, results are merged |
   | `router` | A router selects the best agent per request (model-based or capability-based) |
   | `coordinator` | A coordinator agent delegates and synthesizes across worker agents |
   | `swarm` | Agents hand off to each other dynamically (like OpenAI Swarm) |
   | `hierarchy` | Tree of agents with a root coordinator and sub-teams |

2. Create or update an `agents.yaml` with both agent definitions and team config:

```yaml
defaults:
  model:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
  storage:
    backend: sqlite
    dsn: "team.db"

agents:
  - id: "researcher"
    name: "Research Agent"
    description: "Searches and gathers information"
    system_prompt: "You are a research specialist..."
    tools:
      - name: "search_web"
        description: "Search the web"
        parameters:
          type: object
          properties:
            query: { type: string }
          required: ["query"]
        permission: "allow"

  - id: "analyst"
    name: "Analysis Agent"
    description: "Analyzes data and draws conclusions"
    system_prompt: "You are a data analyst..."

  - id: "writer"
    name: "Writer Agent"
    description: "Produces final written output"
    system_prompt: "You are a technical writer..."

teams:
  - id: "research-team"
    name: "Research & Report Team"
    strategy: "sequential"          # see strategy table above
    agents: ["researcher", "analyst", "writer"]

    # Router-specific (strategy: router):
    # router: "model"              # model | capability
    # router_model:
    #   provider: anthropic
    #   model: claude-sonnet-4-20250514
    #   api_key: ${ANTHROPIC_API_KEY}

    # Coordinator-specific (strategy: coordinator):
    # coordinator: "coordinator-agent-id"

    # Swarm-specific (strategy: swarm):
    # initial_agent: "researcher"
    # max_handoffs: 10

    # Parallel-specific:
    # max_concurrency: 3

    # Shared:
    max_iterations: 10
    error_strategy: "fail_fast"    # fail_fast | collect | best_effort
```

3. Design the team topology based on the description:

   **Sequential pipeline** (e.g., research → analyze → write):
   - Each agent gets the previous agent's output as input
   - Order matters — list agents in execution order

   **Parallel fan-out** (e.g., multi-perspective analysis):
   - All agents receive the same input
   - Results collected and merged
   - Set `max_concurrency` to control parallelism

   **Router** (e.g., customer support triage):
   - `router: "model"` — LLM picks the best agent based on descriptions
   - `router: "capability"` — matches agent capabilities to the request
   - Set `router_model` to use a fast model for routing decisions

   **Coordinator** (e.g., project manager + specialists):
   - One coordinator agent delegates tasks to workers
   - Coordinator sees all worker outputs and synthesizes
   - The coordinator agent needs a system_prompt explaining its role

   **Swarm** (e.g., conversational handoff):
   - Agents transfer control to each other via handoff tools
   - Set `initial_agent` for the entry point
   - Set `max_handoffs` to prevent infinite loops

   **Hierarchy** (e.g., org chart):
   - Nest teams — coordinator agents can reference sub-teams
   - Use `sub_agents` on agent configs to define the tree

4. Give each agent a focused system_prompt that defines its role clearly. Agents work better when they have a single responsibility.

5. Show how to run the team:
```bash
# Run team with a task
go run ./cli/main.go team run -c agents.yaml research-team "Research the latest AI frameworks"

# List available teams
go run ./cli/main.go team list -c agents.yaml
```

6. Verify: `go run ./cli/main.go team list -c agents.yaml`

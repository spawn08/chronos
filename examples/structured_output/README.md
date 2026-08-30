# Structured (JSON) output

Ask an LLM for strict JSON and decode it into a typed Go struct.

`model.ChatRequest.ResponseFormat` controls the output format:

- `"json_object"` — the model returns valid JSON (no schema enforcement).
- `"json_schema"` — the model returns JSON conforming to the schema in
  `Metadata["json_schema"]`. Enforced as a native request parameter by
  OpenAI, Azure OpenAI, OpenAI-compatible providers (Ollama, Mistral, ...),
  Gemini, and the Responses API. Anthropic has no equivalent parameter, so
  it falls back to whatever the system prompt asks for.

Going through `sdk/agent`'s `WithOutputSchema`/`output_schema:` instead of a
bare `Provider.Chat` call (as this example does) additionally validates the
reply against the schema after the fact, on every provider — see
[Structured (JSON) Output](/guides/agents#structured-json-output) in the
Agents guide.

This example requests a `Recipe` as JSON, then decodes the reply into the
`Recipe` struct (tolerating stray Markdown code fences).

## Run

Needs a real LLM provider (one network call). Configure any provider from
`examples/internal/providers`:

```bash
OPENAI_API_KEY=sk-... go run ./examples/structured_output/
```

With no provider set, it prints the required env vars and exits.

## Test

The test covers the **pure JSON parsing helpers only** — no network, no provider:

```bash
go test ./examples/structured_output/
```

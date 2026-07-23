# Azure OpenAI — Tool Calling

Multi-round tool calling against an Azure OpenAI deployment. The model is given
two tools and Chronos drives the full tool-call loop until a final answer is
produced.

## What it demonstrates

- Wiring `tool.Registry` definitions into a provider-agnostic
  `model.ChatRequest.Tools` slice
- The tool-call loop: detect `StopReason == model.StopReasonToolCall`, execute
  each requested tool, append `RoleTool` result messages, and re-ask — bounded
  by `maxToolRounds`
- Two tools: a `calculator` (add/sub/mul/div) and a `lookup` (canned facts)

The loop is identical to the one in `examples/graph_with_llm`; only the
provider construction is Azure-specific.

## Run

```bash
export AZURE_OPENAI_API_KEY=<your-azure-api-key>
export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=<your-deployment-name>   # must support function calling
export AZURE_OPENAI_API_VERSION=2024-10-21

go run ./examples/azure_tools/main.go
```

Without `AZURE_OPENAI_API_KEY` the example prints the required variables and
exits `0` — no network call.

## Tests

`main_test.go` is fully offline: it unit-tests the pure `calculate` and
`lookup` helpers and the `tool.Registry` wiring. It never contacts Azure.

```bash
go test ./examples/azure_tools/...
```

See also: [`examples/azure`](../azure) (chat/streaming) and
[`examples/azure_rag`](../azure_rag) (RAG).

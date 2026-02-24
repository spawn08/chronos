Scaffold a new EmbeddingsProvider implementation for Chronos.

The provider name is: $ARGUMENTS

## Existing Providers
- **OpenAI** — `engine/model/openai_embeddings.go` (reference implementation)
- **Ollama** — `engine/model/ollama_embeddings.go`
- **CachedEmbeddings** — `engine/model/embeddings.go` (wrapper, not a provider)

## Instructions

1. The provider name is $ARGUMENTS (e.g. anthropic, voyage, cohere, huggingface, mistral, gemini). Normalize to lowercase; use it as the file and constructor name (e.g. voyage → `voyage_embeddings.go`, `NewVoyageEmbeddings`).
2. Check that $ARGUMENTS is not already implemented (see existing providers above). If it exists, report that and ask what to change.
3. Create `engine/model/<name>_embeddings.go` in package `model`.
4. Define a struct with fields needed for the API: at minimum API key or base URL, and optional model ID, timeout, etc.
5. Implement `Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)`:
   - Call the provider's embeddings API (HTTP client with req.Input as texts).
   - Map the response to `[][]float32` and optional `Usage`. Return `&EmbeddingResponse{Embeddings: ..., Usage: ...}`.
6. Add a constructor: `New<Name>Embeddings(apiKey string) *<Name>Embeddings`.
7. Follow the pattern from `openai_embeddings.go`. Use `context.Context` for timeouts. Wrap errors: `fmt.Errorf("<name> embeddings: %w", err)`.
8. Run `go build ./...` to verify. Note that callers can wrap with `model.NewCachedEmbeddings(provider)` for caching.

If the provider requires a separate SDK, add the minimal dependency; otherwise use `net/http` and `encoding/json`.

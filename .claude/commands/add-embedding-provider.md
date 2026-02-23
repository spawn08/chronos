Scaffold a new EmbeddingsProvider implementation for Chronos.

The provider name is: $ARGUMENTS

## Instructions

1. The provider name is $ARGUMENTS (e.g. openai, anthropic, voyage, cohere, ollama, huggingface). Normalize to lowercase; use it as the file and constructor name (e.g. openai → openai.go, NewOpenAI).
2. Create `engine/model/<name>.go` in package `model`.
3. Define a struct (e.g. `OpenAI`) with fields needed for the API: at minimum API key or base URL, and optional model ID, timeout, etc. Do not commit real API keys; use env vars or config.
4. Implement `Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)`:
   - Call the provider's embeddings API (HTTP client with req.Input as texts).
   - Map the response to `[][]float32` and optional `Usage`. Return `&EmbeddingResponse{Embeddings: ..., Usage: ...}`.
5. Add a constructor: `New<Name>(apiKey string) *<Name>` or `New<Name>(cfg *ProviderConfig) *<Name>`.
6. Follow patterns from existing providers in `engine/model/` (e.g. openai.go for HTTP). Use `context.Context` for timeouts and cancellation. Wrap errors: `fmt.Errorf("embedding provider name: %w", err)`.
7. Run `go build ./...` to verify. Optionally mention that callers can wrap with `model.NewCachedEmbeddings(provider)` for caching.

If the provider requires a separate SDK (e.g. official client library), add the minimal dependency and use it; otherwise use `net/http` and `encoding/json`.

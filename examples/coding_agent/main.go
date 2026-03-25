// Example: coding_agent — A full-featured autonomous coding agent.
//
// This example demonstrates building a Cursor/Aider-style coding agent that can:
//   - Read, write, and search files using built-in file tools
//   - Execute shell commands (git, go build, tests, etc.)
//   - Use a vector database for semantic code search (RAG)
//   - Plan and implement multi-step coding tasks autonomously
//
// What you'll learn:
//   - Wiring file tools, shell tools, and custom tools onto an agent
//   - Setting up VectorKnowledge with in-memory embeddings for code search
//   - Running an autonomous agent loop with MaxIterations
//   - Combining tools with system prompts for effective coding workflows
//
// Prerequisites:
//   - Go 1.22+
//   - Set OPENAI_API_KEY or ANTHROPIC_API_KEY for real LLM responses
//   - No API keys required to see the structure — falls back to a mock provider
//
// Run:
//
//	go run ./examples/coding_agent/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/knowledge"
	"github.com/spawn08/chronos/storage"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║       Chronos Coding Agent Example                ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")

	// ════════════════════════════════════════════════════════════════
	// Step 1: Set up the LLM provider
	//
	// The coding agent needs a capable model for reasoning about code.
	// We try real providers first, falling back to a mock for demos.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 1: Model Provider ━━━")
	provider := resolveProvider()
	fmt.Printf("  Using provider: %s (%s)\n", provider.Name(), provider.Model())

	// ════════════════════════════════════════════════════════════════
	// Step 2: Set up the knowledge base (vector store for code search)
	//
	// In a real system, you'd index your codebase into a vector store
	// (Qdrant, Pinecone, pgvector, etc.) and let the agent search it.
	// Here we use an in-memory store with mock embeddings to demonstrate
	// the pattern without external dependencies.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 2: Knowledge Base (Code Index) ━━━")

	vectorStore := newMockVectorStore()
	embedder := &mockEmbedder{}

	kb := knowledge.NewVectorKnowledge("codebase", 384, vectorStore, embedder, "mock-embed")
	kb.AddDocuments(
		knowledge.Document{
			ID:      "main.go",
			Content: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
			Metadata: map[string]any{
				"file": "main.go", "language": "go", "description": "Entry point of the application",
			},
		},
		knowledge.Document{
			ID:      "handler.go",
			Content: "package api\n\nfunc HandleRequest(w http.ResponseWriter, r *http.Request) {\n\tw.WriteHeader(200)\n\tw.Write([]byte(`{\"status\": \"ok\"}`))\n}",
			Metadata: map[string]any{
				"file": "handler.go", "language": "go", "description": "HTTP handler for API requests",
			},
		},
		knowledge.Document{
			ID:      "config.go",
			Content: "package config\n\ntype Config struct {\n\tPort int    `json:\"port\"`\n\tHost string `json:\"host\"`\n\tDB   string `json:\"db\"`\n}\n\nfunc Load(path string) (*Config, error) {\n\tdata, err := os.ReadFile(path)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tvar cfg Config\n\treturn &cfg, json.Unmarshal(data, &cfg)\n}",
			Metadata: map[string]any{
				"file": "config.go", "language": "go", "description": "Configuration loading and validation",
			},
		},
		knowledge.Document{
			ID:      "db.go",
			Content: "package store\n\ntype Store struct {\n\tdb *sql.DB\n}\n\nfunc New(dsn string) (*Store, error) {\n\tdb, err := sql.Open(\"postgres\", dsn)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"open db: %w\", err)\n\t}\n\treturn &Store{db: db}, nil\n}",
			Metadata: map[string]any{
				"file": "db.go", "language": "go", "description": "Database connection and query layer",
			},
		},
	)

	if err := kb.Load(ctx); err != nil {
		log.Fatalf("Failed to load knowledge base: %v", err)
	}
	fmt.Println("  Indexed 4 code files into vector store")

	// ════════════════════════════════════════════════════════════════
	// Step 3: Build the coding agent
	//
	// The agent is configured with:
	//   - A detailed system prompt that teaches it HOW to code
	//   - File tools (read, write, list, glob, grep) for workspace access
	//   - Shell tool (auto-approved) for running commands
	//   - A custom semantic search tool connected to the vector store
	//   - The knowledge base for automatic RAG on every query
	//   - MaxIterations=15 to allow complex multi-step tasks
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 3: Building Coding Agent ━━━")

	workDir := "."
	b := agent.New("coding-agent", "Chronos Coding Agent").
		Description("An autonomous coding agent that can read, write, search, and execute code").
		WithModel(provider).
		WithSystemPrompt(codingAgentSystemPrompt).
		WithKnowledge(kb).
		WithMaxIterations(15).
		WithDebug(false).
		AddToolkit(builtins.NewFileToolkit(workDir)).
		AddTool(builtins.NewAutoShellTool(nil, 0)).
		AddTool(newSemanticSearchTool(kb))

	codingAgent, err := b.Build()
	if err != nil {
		log.Fatalf("Failed to build coding agent: %v", err)
	}

	tools := codingAgent.Tools.List()
	fmt.Printf("  Agent: %s\n", codingAgent.Name)
	fmt.Printf("  Tools: %d registered\n", len(tools))
	for _, t := range tools {
		fmt.Printf("    - %s: %s\n", t.Name, truncate(t.Description, 60))
	}

	// ════════════════════════════════════════════════════════════════
	// Step 4: Run the coding agent on a task
	//
	// The agent receives a natural-language task and uses its tools
	// to gather context, plan, and execute. With a real LLM, it would
	// actually read files, run commands, and write code. With the mock
	// provider, we demonstrate the tool wiring and agent structure.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 4: Running Coding Agent ━━━")

	task := "List the Go files in the current directory, then read the go.mod file to understand the project structure."

	fmt.Printf("  Task: %s\n\n", task)

	resp, err := codingAgent.Chat(ctx, task)
	if err != nil {
		fmt.Printf("  Agent error: %v\n", err)
	} else {
		output := resp.Content
		if len(output) > 500 {
			output = output[:500] + "..."
		}
		fmt.Printf("  Agent response:\n%s\n", indent(output, "    "))
		if resp.Usage.PromptTokens > 0 {
			fmt.Printf("\n  [tokens: %d prompt + %d completion]\n",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		}
	}

	// ════════════════════════════════════════════════════════════════
	// Step 5: Demonstrate semantic code search
	//
	// The agent can search the indexed codebase semantically.
	// This is the same capability used automatically via the Knowledge
	// interface — here we show it as an explicit tool call.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 5: Semantic Code Search ━━━")

	searchResults, err := kb.Search(ctx, "database connection", 3)
	if err != nil {
		fmt.Printf("  Search error: %v\n", err)
	} else {
		fmt.Printf("  Query: 'database connection'\n")
		fmt.Printf("  Results: %d documents\n", len(searchResults))
		for i, doc := range searchResults {
			file, _ := doc.Metadata["file"].(string)
			desc, _ := doc.Metadata["description"].(string)
			fmt.Printf("    %d. %s (score=%.2f) — %s\n", i+1, file, doc.Score, desc)
		}
	}

	fmt.Println("\n✓ Coding agent example completed.")
}

const codingAgentSystemPrompt = `You are an expert autonomous coding agent, similar to Cursor or Aider.

Your workflow for any coding task:
1. UNDERSTAND: Use file_list, file_read, file_grep, and semantic_search to gather context
2. PLAN: Think step-by-step about what changes are needed
3. IMPLEMENT: Use file_write to create or modify files
4. VERIFY: Use shell to run tests, builds, and linters
5. ITERATE: If tests fail, read errors and fix them

Available tools:
- file_read: Read file contents
- file_write: Write content to a file
- file_list: List directory contents
- file_glob: Find files matching a glob pattern
- file_grep: Search for text patterns in files
- shell: Execute shell commands (git, go build, go test, etc.)
- semantic_search: Search the codebase by meaning, not just text

Rules:
- Always read relevant files before modifying them
- Run tests after making changes
- Use git commands to understand project history when needed
- Wrap errors with context (fmt.Errorf("context: %w", err))
- Write idiomatic Go code following project conventions
- Never hardcode secrets or credentials`

// newSemanticSearchTool creates a tool that searches the knowledge base.
func newSemanticSearchTool(kb knowledge.Knowledge) *tool.Definition {
	return &tool.Definition{
		Name:        "semantic_search",
		Description: "Search the codebase semantically by meaning. Returns relevant code snippets ranked by relevance.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural language query describing what you're looking for",
				},
				"top_k": map[string]any{
					"type":        "integer",
					"description": "Number of results to return (default: 5)",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			query, _ := args["query"].(string)
			if query == "" {
				return nil, fmt.Errorf("semantic_search: 'query' is required")
			}
			topK := 5
			if k, ok := args["top_k"].(float64); ok {
				topK = int(k)
			}

			docs, err := kb.Search(ctx, query, topK)
			if err != nil {
				return nil, fmt.Errorf("semantic_search: %w", err)
			}

			results := make([]map[string]any, len(docs))
			for i, d := range docs {
				results[i] = map[string]any{
					"id":       d.ID,
					"content":  d.Content,
					"metadata": d.Metadata,
					"score":    d.Score,
				}
			}
			return map[string]any{"results": results, "count": len(results)}, nil
		},
	}
}

func resolveProvider() model.Provider {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return model.NewOpenAI(key)
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return model.NewAnthropic(key)
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		return model.NewGemini(key)
	}
	fmt.Println("  ⚠ No API key found, using mock provider")
	fmt.Println("    Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY for real responses")
	return &mockProvider{}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

// ── Mock implementations for running without external dependencies ──

type mockProvider struct{}

func (m *mockProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1].Content

	if len(req.Tools) > 0 && !strings.Contains(last, "[Tool result") {
		return &model.ChatResponse{
			Content:    fmt.Sprintf("[Mock coding agent analyzing: %.80s]\n\nI would use the available tools (file_read, file_list, shell, semantic_search) to gather context, then implement the requested changes. With a real LLM provider, I would make actual tool calls to read files, search code, run commands, and write solutions.", last),
			Role:       "assistant",
			StopReason: model.StopReasonEnd,
		}, nil
	}

	return &model.ChatResponse{
		Content:    fmt.Sprintf("[Mock response for: %.100s]", last),
		Role:       "assistant",
		StopReason: model.StopReasonEnd,
	}, nil
}

func (m *mockProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := m.Chat(context.Background(), req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *mockProvider) Name() string  { return "mock" }
func (m *mockProvider) Model() string { return "mock-coding-v1" }

// mockVectorStore implements storage.VectorStore in-memory for this example.
type mockVectorStore struct {
	collections map[string][]storage.Embedding
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{collections: make(map[string][]storage.Embedding)}
}

func (m *mockVectorStore) CreateCollection(_ context.Context, name string, _ int) error {
	if _, ok := m.collections[name]; !ok {
		m.collections[name] = nil
	}
	return nil
}

func (m *mockVectorStore) Upsert(_ context.Context, collection string, embeddings []storage.Embedding) error {
	m.collections[collection] = append(m.collections[collection], embeddings...)
	return nil
}

func (m *mockVectorStore) Search(_ context.Context, collection string, _ []float32, topK int) ([]storage.SearchResult, error) {
	embs := m.collections[collection]
	results := make([]storage.SearchResult, 0, topK)
	for i, e := range embs {
		if i >= topK {
			break
		}
		results = append(results, storage.SearchResult{
			Embedding: e,
			Score:     1.0 - float32(i)*0.1,
		})
	}
	return results, nil
}

func (m *mockVectorStore) Delete(_ context.Context, _ string, _ []string) error { return nil }
func (m *mockVectorStore) Close() error                                         { return nil }

// mockEmbedder returns deterministic embeddings for testing.
type mockEmbedder struct{}

func (e *mockEmbedder) Embed(_ context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	embeddings := make([][]float32, len(req.Input))
	for i, text := range req.Input {
		vec := make([]float32, 384)
		for j := 0; j < len(vec) && j < len(text); j++ {
			vec[j] = float32(text[j]) / 255.0
		}
		embeddings[i] = vec
	}
	return &model.EmbeddingResponse{
		Embeddings: embeddings,
		Usage:      model.Usage{PromptTokens: len(req.Input) * 10},
	}, nil
}

//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spawn08/chronos/engine/guardrails"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/knowledge"
	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/storage/adapters/postgres"
	"github.com/spawn08/chronos/storage/adapters/qdrant"
)

func main() {
	ctx := context.Background()

	// ── 1. Durable storage (PostgreSQL) ──
	store, err := postgres.New(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// ── 2. Vector store (Qdrant) — for RAG + memory recall ──
	vectorStore, err := qdrant.New(os.Getenv("QDRANT_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer vectorStore.Close()

	// ── 3. LLM providers ──
	llm := model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))
	embedder := model.NewOpenAIEmbeddings(os.Getenv("OPENAI_API_KEY"))

	// ── 4. Knowledge / RAG pipeline ──
	kb := knowledge.NewVectorKnowledge(
		"documents", 1536, vectorStore, embedder,
		"text-embedding-3-small",
		knowledge.WithTopK(5),
		knowledge.WithScoreThreshold(0.7),
		knowledge.WithChunking(512, 64),
		knowledge.WithEmbedBatchSize(100),
		knowledge.WithQueryCache(1000, 5*60e9), // 5 min TTL
	)

	// ── 5. Memory (long-term with vector recall) ──
	memStore := memory.NewStore(store)
	memMgr := memory.NewManager("production-agent", "user-1", memStore, llm).
		WithVectorIndex(embedder, vectorStore, "text-embedding-3-small", 1536)

	// ── 6. Tracing ──
	tracer := trace.NewCollector(store)

	// ── 7. Load YAML config (gets model, tools, skills, system prompt) ──
	cfg, err := agent.LoadFile("agents.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// ── 8. Build agent with all production concerns ──
	a, err := agent.BuildAgent(ctx, &cfg.Agents[0],
		agent.WithStorageOverride(store),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Wire up programmatic concerns not expressible in YAML
	builder := agent.New(cfg.Agents[0].ID, cfg.Agents[0].Name).
		WithModel(llm).
		WithStorage(store).
		WithMemoryManager(memMgr).
		WithKnowledge(kb).
		WithTracer(tracer).
		WithStreaming(true).
		WithHistoryRuns(15).
		WithMaxIterations(15).
		WithContextConfig(agent.ContextConfig{
			MaxTokens:          16384,
			SummarizeThreshold: 12000,
			PreserveRecentTurns: 6,
		}).
		// Guardrails
		AddInputGuardrail("max-length",
			&guardrails.MaxLengthGuardrail{MaxChars: 50000}).
		AddInputGuardrail("blocklist",
			&guardrails.BlocklistGuardrail{Blocklist: []string{
				"DROP TABLE", "DELETE FROM", "rm -rf",
			}}).
		AddOutputGuardrail("max-output",
			&guardrails.MaxLengthGuardrail{MaxChars: 20000}).
		AddOutputGuardrail("no-secrets",
			&guardrails.BlocklistGuardrail{Blocklist: []string{
				"sk-ant-", "sk-", "AKIA", "password:",
			}}).
		// Knowledge search tool
		AddTool(&tool.Definition{
			Name:        "search_knowledge",
			Description: "Search the knowledge base for relevant documents",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query"},
					"top_k": map[string]any{"type": "integer", "description": "Max results"},
				},
				"required": []string{"query"},
			},
			Permission: tool.PermAllow,
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				query, _ := args["query"].(string)
				topK := 5
				if k, ok := args["top_k"].(float64); ok {
					topK = int(k)
				}
				docs, err := kb.Search(ctx, query, topK)
				if err != nil {
					return nil, fmt.Errorf("knowledge search: %w", err)
				}
				var results []map[string]any
				for _, d := range docs {
					results = append(results, map[string]any{
						"id":      d.ID,
						"content": d.Content,
						"score":   d.Score,
						"source":  d.Metadata["source"],
					})
				}
				return results, nil
			},
		})

	prodAgent, err := builder.Build()
	if err != nil {
		log.Fatal(err)
	}

	// ── 9. Connect MCP servers ──
	if err := prodAgent.ConnectMCP(ctx); err != nil {
		log.Printf("MCP connection warning: %v", err)
	}
	defer prodAgent.CloseMCP()

	// ── 10. Run ──
	_ = a // YAML-built agent (alternative approach)
	ch, err := prodAgent.ChatStream(ctx, "What do you know about our refund policy?")
	if err != nil {
		log.Fatal(err)
	}
	for resp := range ch {
		fmt.Print(resp.Content)
	}
	fmt.Println()
}

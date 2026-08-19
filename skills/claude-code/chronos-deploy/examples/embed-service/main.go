//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	store, err := sqlite.New("app.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	a, err := agent.New("api-agent", "API Agent").
		WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
		WithStorage(store).
		WithStreaming(true).
		WithSystemPrompt("You are a helpful assistant.").
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// JSON endpoint
	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		resp, err := a.Chat(r.Context(), req.Message)
		if err != nil {
			http.Error(w, fmt.Sprintf("agent error: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": resp.Content})
	})

	// SSE streaming endpoint
	http.HandleFunc("/api/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch, err := a.ChatStream(r.Context(), req.Message)
		if err != nil {
			http.Error(w, fmt.Sprintf("stream error: %v", err), http.StatusInternalServerError)
			return
		}

		for resp := range ch {
			data, _ := json.Marshal(map[string]string{"content": resp.Content})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

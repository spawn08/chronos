//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	cfg, err := agent.LoadFile("agents.yaml")
	if err != nil {
		log.Fatal(err)
	}

	agents, err := agent.BuildAll(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}

	bot := agents["chatbot"]

	ch, err := bot.ChatStream(ctx, "Hello! What can you help me with?")
	if err != nil {
		log.Fatal(err)
	}

	for resp := range ch {
		fmt.Print(resp.Content)
	}
	fmt.Println()
}

// Package cmd provides the Chronos CLI command tree.
package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	chronosos "github.com/chronos-ai/chronos/os"
	"github.com/chronos-ai/chronos/cli/repl"
	"github.com/chronos-ai/chronos/storage/adapters/sqlite"
)

// Execute runs the root CLI command.
func Execute() error {
	if len(os.Args) < 2 {
		return printUsage()
	}
	switch os.Args[1] {
	case "repl", "interactive":
		return runREPL()
	case "serve":
		return runServe()
	case "run":
		return runAgent()
	case "help", "--help", "-h":
		return printUsage()
	case "version":
		fmt.Println("chronos v0.1.0")
		return nil
	default:
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}

func printUsage() error {
	fmt.Println(`Chronos CLI — Agentic Framework

Usage:
  chronos <command>

Commands:
  repl        Start interactive REPL
  serve       Start ChronosOS control plane server
  run         Run an agent in headless mode
  version     Print version
  help        Show this help`)
	return nil
}

func runREPL() error {
	store, err := sqlite.New("chronos.db")
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	r := repl.New(store)
	return r.Start()
}

func runServe() error {
	addr := ":8420"
	if len(os.Args) > 2 {
		addr = os.Args[2]
	}
	store, err := sqlite.New("chronos.db")
	if err != nil {
		return err
	}
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	srv := chronosos.New(addr, store)
	log.Printf("Starting ChronosOS on %s", addr)
	return srv.Start(context.Background())
}

func runAgent() error {
	fmt.Println("TODO: Load agent config and run in headless mode")
	return nil
}

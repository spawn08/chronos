// Example: skills — registering, listing, and resolving skills.
//
// What you'll learn:
//   - How to build a skill.Registry and register skill.Skill definitions
//   - How to attach metadata (version, tags, tools, manifest) to a skill
//   - How to look a skill up by name and inspect what it provides
//
// A Skill is an installable capability: a named, versioned bundle of metadata
// (and the tool names it contributes) that an agent can advertise and load.
//
// This example is fully OFFLINE — it needs no API keys and no network.
//
// Run:
//
//	go run ./examples/skills/
package main

import (
	"fmt"
	"sort"

	"github.com/spawn08/chronos/sdk/skill"
)

func main() {
	fmt.Println("━━━ Chronos Skills example ━━━")

	// ════════════════════════════════════════════════════════════════
	// Step 1: Create a registry
	// ════════════════════════════════════════════════════════════════
	registry := skill.NewRegistry()

	// ════════════════════════════════════════════════════════════════
	// Step 2: Register a couple of skills
	//
	// Each skill carries a version and the tool names it provides. The
	// registry keys skills by name, so registering the same name again
	// upgrades it in place — the pattern for shipping a new version.
	// ════════════════════════════════════════════════════════════════
	registry.Register(&skill.Skill{
		Name:        "web-search",
		Version:     "1.0.0",
		Description: "Search the public web and summarize results.",
		Author:      "chronos",
		Tags:        []string{"research", "io"},
		Tools:       []string{"search", "fetch_url"},
	})

	registry.Register(&skill.Skill{
		Name:        "code-review",
		Version:     "0.3.0",
		Description: "Static review of a diff for style and correctness.",
		Author:      "chronos",
		Tags:        []string{"engineering"},
		Tools:       []string{"lint", "diff_summary"},
		Manifest:    map[string]any{"languages": []string{"go", "python"}},
	})

	// Ship a new version of web-search — same name replaces the old entry.
	registry.Register(&skill.Skill{
		Name:        "web-search",
		Version:     "1.1.0",
		Description: "Search the public web, summarize, and cite sources.",
		Author:      "chronos",
		Tags:        []string{"research", "io", "citations"},
		Tools:       []string{"search", "fetch_url", "cite"},
	})

	// ════════════════════════════════════════════════════════════════
	// Step 3: List everything installed
	// ════════════════════════════════════════════════════════════════
	skills := registry.List()
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })

	fmt.Printf("\nInstalled skills (%d):\n", len(skills))
	for _, s := range skills {
		fmt.Printf("  - %-12s v%-6s %v\n", s.Name, s.Version, s.Tags)
	}

	// ════════════════════════════════════════════════════════════════
	// Step 4: Resolve one skill by name and inspect it
	// ════════════════════════════════════════════════════════════════
	if s, ok := registry.Get("web-search"); ok {
		fmt.Printf("\nResolved %q:\n", s.Name)
		fmt.Printf("  version:     %s\n", s.Version)
		fmt.Printf("  description: %s\n", s.Description)
		fmt.Printf("  tools:       %v\n", s.Tools)
	}

	// Uninstalling removes it from the registry.
	if err := registry.Uninstall("code-review"); err != nil {
		fmt.Printf("\nuninstall failed: %v\n", err)
	}
	fmt.Printf("\nAfter uninstall, %d skill(s) remain.\n", len(registry.List()))
}

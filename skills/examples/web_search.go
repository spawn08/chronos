// Package examples provides example skill definitions.
package examples

import (
	"github.com/chronos-ai/chronos/sdk/skill"
)

// WebSearchSkill is an example skill for web search.
var WebSearchSkill = &skill.Skill{
	Name:        "web_search",
	Version:     "1.0.0",
	Description: "Search the web using a configurable search API",
	Author:      "chronos",
	Tags:        []string{"search", "web", "rag"},
	Tools:       []string{"web_search"},
	Manifest: map[string]any{
		"api_key_env": "SEARCH_API_KEY",
		"max_results": 10,
	},
}

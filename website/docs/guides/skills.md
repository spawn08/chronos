---
title: "Skills"
---


A **Skill** is an installable capability: a named, versioned bundle of metadata (description, tags, an optional Markdown manifest) plus the names of the tools it relies on. Skills don't register tools themselves — they describe *when* an agent should reach for tools it already has, and that description gets injected into the system prompt so the model knows the capability exists. The `sdk/skill` package provides the `Skill` struct, an in-memory `Registry`, and a loader that turns a directory of `SKILL.md` files into skills.

The snippets on this page assume these imports:

```go
import (
    "fmt"
    "sort"

    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/skill"
)
```

## The Skill struct

```go
type Skill struct {
    Name        string
    Version     string
    Description string
    Author      string
    Tags        []string
    Manifest    map[string]any // from SKILL.md or JSON manifest
    Tools       []string       // tool names this skill provides
}
```

`Manifest` is a free-form bag; when a skill is loaded from a `SKILL.md` file, the loader stores the Markdown body under `Manifest["body"]` so it can be appended to the agent's system prompt.

## Registry

`skill.NewRegistry()` returns an empty, concurrency-safe `*Registry` keyed by skill name.

| Method | Signature | Behavior |
|--------|-----------|----------|
| `Register` | `Register(s *Skill)` | Inserts or overwrites the entry for `s.Name` — registering the same name again upgrades it in place |
| `Uninstall` | `Uninstall(name string) error` | Removes a skill; returns an error if `name` isn't registered |
| `Get` | `Get(name string) (*Skill, bool)` | Looks up a skill by name |
| `List` | `List() []*Skill` | Returns every registered skill (unordered — sort if you need a stable order) |

### Registering and resolving skills directly

```go
registry := skill.NewRegistry()

registry.Register(&skill.Skill{
    Name:        "web-search",
    Version:     "1.0.0",
    Description: "Search the public web and summarize results.",
    Author:      "chronos",
    Tags:        []string{"research", "io"},
    Tools:       []string{"search", "fetch_url"},
})

// Registering the same name again ships a new version in place.
registry.Register(&skill.Skill{
    Name:        "web-search",
    Version:     "1.1.0",
    Description: "Search the public web, summarize, and cite sources.",
    Tags:        []string{"research", "io", "citations"},
    Tools:       []string{"search", "fetch_url", "cite"},
})

skills := registry.List()
sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
for _, s := range skills {
    fmt.Printf("%-12s v%-6s %v\n", s.Name, s.Version, s.Tags)
}

if s, ok := registry.Get("web-search"); ok {
    fmt.Println(s.Description, s.Tools)
}

if err := registry.Uninstall("web-search"); err != nil {
    fmt.Println("uninstall failed:", err)
}
```

This mirrors the runnable [`examples/skills`](https://github.com/spawn08/chronos/tree/main/examples/skills) example, which is fully offline (no API keys, no network).

## Attaching a skill to an agent

`agent.Builder` owns a `*skill.Registry` (field `Agent.Skills`) and exposes `AddSkill` to register into it:

```go
a, err := agent.New("assistant", "Assistant").
    WithModel(provider).
    AddSkill(&skill.Skill{
        Name:        "web-search",
        Version:     "1.1.0",
        Description: "Search the public web, summarize, and cite sources.",
        Tools:       []string{"search", "fetch_url", "cite"},
    }).
    Build()
if err != nil {
    // handle error
}
```

`AddSkill` only registers the skill on `a.Skills`; it does not wire tools or touch the system prompt. When building an agent from YAML (below), skills additionally get rendered into an `## Available skills` block that's appended to the system prompt at build time.

## SKILL.md — the file-based convention

`skill.LoadFromDir(root)` walks `root` for files named `SKILL.md` (case-insensitive) and parses each one into a `*Skill`. The layout convention is:

```
root/
  <skill-name>/
    SKILL.md
```

Each `SKILL.md` must begin with a YAML frontmatter block delimited by `---` lines. The frontmatter fields map directly onto `Skill`'s fields (`name`, `version`, `description`, `author`, `tags`, `tools`); everything else in the file — the Markdown body after the closing `---` — is captured into `Manifest["body"]` so callers can inject it into a system prompt. If `name` is omitted from the frontmatter, it defaults to the containing directory's basename. A file with no leading `---` fence is skipped rather than treated as an error, so unrelated Markdown in the tree doesn't accidentally become a skill. A missing `root` directory is also not an error — `LoadFromDir` returns `(nil, nil)`.

Example, adapted from `examples/yaml-configs/skills/ado-wiql/SKILL.md`:

```markdown
---
name: ado-wiql
version: 1.0.0
description: Count and filter Azure DevOps work items with WIQL.
author: chronos-examples
tags: [azure-devops, research]
tools:
  - wit_query_by_wiql
  - wit_get_work_items_batch_by_ids
---

# When to use

Use this skill for any question about ADO bugs, tasks, features, or user
stories — counts, status distributions, "what's assigned to X", "how many
are open in project Y".

# How to use

1. Call `wit_query_by_wiql` exactly once with a WIQL query.
2. Read `result.workItems` from the response — its length is the count.
```

Loading and registering it in Go:

```go
skills, err := skill.LoadFromDir("./skills")
if err != nil {
    // handle error
}
for _, s := range skills {
    registry.Register(s)
}

// Or, equivalently, in one call:
n, err := skill.LoadFromDirInto("./skills", registry)
```

## Wiring SKILL.md into YAML agents

`agent.FileConfig` has a top-level `skills_dir` field. `agent.BuildAll` loads every `SKILL.md` under that directory once into a shared catalog keyed by skill name, and individual agents opt into catalog entries by listing names under `use_skills`. Agents can also declare skills inline under `skills:` (optionally pointing `manifest_path` at a Markdown file to append to the prompt) without needing a catalog at all:

```yaml
skills_dir: ./skills

agents:
  - id: ado-assistant
    name: ADO Assistant
    model:
      provider: anthropic
      model: claude-sonnet-4-6
    use_skills:
      - ado-wiql

  - id: inline-example
    name: Inline Skill Example
    model:
      provider: anthropic
      model: claude-sonnet-4-6
    skills:
      - name: cited-answers
        version: "1.0.0"
        description: Attach sources to every factual claim.
        tools: [search, fetch_url]
```

Referencing an unknown name in `use_skills` aborts the build with a clear error, so typos fail fast instead of silently producing a skill-less agent.

## See also

- [`examples/skills`](https://github.com/spawn08/chronos/tree/main/examples/skills) — runnable, offline registry walkthrough
- [`examples/yaml-configs/skills/ado-wiql/SKILL.md`](https://github.com/spawn08/chronos/tree/main/examples/yaml-configs) — a real `SKILL.md` used by a YAML agent config
- [Agents](/guides/agents) — the `agent.Builder` API, including `AddSkill`
- [YAML Examples](/guides/yaml-examples) — full YAML agent configuration reference

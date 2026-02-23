Create a new Chronos skill with metadata manifest.

The skill name/description is: $ARGUMENTS

## Instructions

1. Create file `skills/examples/$ARGUMENTS.go` (or appropriate location)
2. Define a `*skill.Skill` variable following the pattern in `skills/examples/web_search.go`:

```go
var MySkill = &skill.Skill{
    Name:        "skill_name",
    Version:     "1.0.0",
    Description: "What this skill provides",
    Author:      "chronos",
    Tags:        []string{"relevant", "tags"},
    Tools:       []string{"tool_names_this_skill_provides"},
    Manifest: map[string]any{
        // Configuration keys
    },
}
```

3. Skills are registered on an agent via `.AddSkill(mySkill)`
4. The `Tools` field should list tool names that this skill makes available
5. The `Manifest` field holds configuration (API keys env vars, limits, etc.)
6. Skills support versioning — use semver in the `Version` field
7. Run `go build ./...` to verify

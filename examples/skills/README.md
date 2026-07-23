# Skills

Registering, listing, resolving, and versioning skills with `skill.Registry`.

A **Skill** is an installable capability: a named, versioned bundle of metadata
(description, tags, manifest) plus the tool names it contributes. This example:

1. Builds a `skill.Registry`.
2. Registers two skills, then ships a new version of one (same name replaces in place).
3. Lists all installed skills.
4. Resolves one by name and inspects it.
5. Uninstalls a skill.

Fully **offline** — no API keys, no network.

## Run

```bash
go run ./examples/skills/
```

## Test

```bash
go test ./examples/skills/
```

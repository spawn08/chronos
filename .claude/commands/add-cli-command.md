Add a new CLI command or subcommand to the Chronos CLI.

The command specification is: $ARGUMENTS

## Instructions

1. Parse $ARGUMENTS. It may be a single command name (e.g. `sessions`) or a full path (e.g. `sessions list`, `kb add`, `memory forget`). Determine the top-level command and optional subcommand(s).
2. **Entry point**: Commands are dispatched in `cli/cmd/root.go` via `os.Args[1]`. For a new top-level command (e.g. `chronos sessions`), add a new case in the switch and a `runSessions()` (or similar) that reads `os.Args[2]` for subcommands (list, resume, export). For subcommand-only (e.g. adding `sessions resume`), implement the handler in the existing branch.
3. **Structure**: Prefer one handler function per command (e.g. `runSessionsList`, `runSessionsResume`). Optionally add a subpackage under `cli/cmd/` (e.g. `cli/cmd/sessions/sessions.go`) if the command has many subcommands or shared logic.
4. **Storage**: If the command needs storage (sessions, memory, kb, db), open or receive `storage.Storage` the same way as `repl` or `serve` (e.g. SQLite with path from env or default). Call `Migrate(ctx)` if required.
5. **Output**: Use `fmt.Println` or `fmt.Printf` for simple output. Return an error so that the main CLI can exit with a non-zero code on failure.
6. **Flags**: For optional flags (e.g. `--limit`, `--output`), use `os.Args` parsing or introduce the standard `flag` package (or a minimal CLI lib) as appropriate; keep the CLI lightweight.
7. Update the help text in `printUsage()` in `root.go` to include the new command(s).
8. Run `go build ./...` and manually test the new command (e.g. `go run ./cli/main.go <command> <subcommand>`).

Implement only what was requested; do not add unrelated commands.

Generate table-driven tests for a Chronos package.

The package path or name is: $ARGUMENTS

## Instructions

1. Resolve $ARGUMENTS to a Go package path under the project (e.g. `storage`, `storage/adapters/sqlite`, `sdk/agent`, `engine/model`). If $ARGUMENTS is a single word (e.g. `sqlite`), infer the full path (e.g. `storage/adapters/sqlite`).
2. List the exported types and functions in that package. Prefer testing: constructors, interface implementations, and key business logic. Skip testing trivial getters or internal helpers unless they have complex behavior.
3. Create or update `*_test.go` in the same package. Use table-driven tests:
   - Define a struct with name, input, expected (or expectedErr), and optional setup/teardown.
   - Loop over cases and run logic; use t.Run(subtestName, fn) for each case.
4. For storage adapters: use in-memory or temp resources where possible (e.g. SQLite `:memory:`, or a test helper that creates/destroys a DB). Mock external APIs if needed.
5. For model providers: use mocks or skip tests that require API keys; document that integration tests need env vars.
6. Run `go test ./<package path>/...` and fix any failures. Ensure tests are deterministic and do not leave global state.

If the package has no testable exported surface, report that and suggest what would need to change to make it testable.

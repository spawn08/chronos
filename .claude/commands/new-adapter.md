Create a new storage adapter for Chronos.

The adapter name is: $ARGUMENTS

## Instructions

1. Create the file `storage/adapters/$ARGUMENTS/$ARGUMENTS.go`
2. The package name should be `$ARGUMENTS`
3. Define a `Store` struct with connection fields appropriate for this backend
4. Implement a `New(dsn string) (*Store, error)` constructor
5. Implement ALL methods of the `storage.Storage` interface (defined in `storage/storage.go`):
   - Sessions: CreateSession, GetSession, UpdateSession, ListSessions
   - Memory: PutMemory, GetMemory, ListMemory, DeleteMemory
   - Audit logs: AppendAuditLog, ListAuditLogs
   - Traces: InsertTrace, GetTrace, ListTraces
   - Events: AppendEvent, ListEvents
   - Checkpoints: SaveCheckpoint, GetCheckpoint, GetLatestCheckpoint, ListCheckpoints
   - Lifecycle: Migrate, Close
6. Use `encoding/json` for marshaling map/any fields
7. Follow existing patterns from `storage/adapters/sqlite/sqlite.go` and `storage/adapters/postgres/postgres.go`
8. Run `go build ./...` to verify compilation

If this is a vector store adapter instead (like qdrant, milvus, etc.), implement `storage.VectorStore` from `storage/vector.go` instead:
- Upsert, Search, Delete, CreateCollection, Close

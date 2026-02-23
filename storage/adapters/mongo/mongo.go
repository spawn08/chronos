// Package mongo provides a MongoDB-backed Storage adapter stub.
package mongo

// Store will implement storage.Storage using MongoDB.
type Store struct {
	URI      string
	Database string
}

// TODO: Implement storage.Storage interface methods.

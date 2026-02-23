// Package dynamo provides a DynamoDB-backed Storage adapter stub.
package dynamo

// Store will implement storage.Storage using AWS DynamoDB.
type Store struct {
	Region    string
	TableName string
}

// TODO: Implement storage.Storage interface methods.

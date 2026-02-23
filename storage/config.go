package storage

// Config selects which storage and vector adapters to use at runtime.
type Config struct {
	// Storage backend: "sqlite", "postgres", "mysql", "redis", "mongo", "dynamo"
	Backend string `json:"backend" yaml:"backend"`

	// DSN / connection string for the storage backend.
	DSN string `json:"dsn" yaml:"dsn"`

	// VectorBackend: "qdrant", "redisvector", "milvus", "weaviate", "pinecone"
	VectorBackend string `json:"vector_backend" yaml:"vector_backend"`

	// VectorDSN / connection for vector store.
	VectorDSN string `json:"vector_dsn" yaml:"vector_dsn"`
}

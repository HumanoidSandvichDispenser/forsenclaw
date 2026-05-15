package search

// VectorStore is the interface for storing and retrieving embedding vectors.
// Implementations can swap the backend (JSON blobs, pgvector, etc.) without
// changing callers.
type VectorStore interface {
	// Insert stores a vector with the given ID.
	Insert(id string, vec []float32) error
	// Search returns the top-K most similar vectors to the query vector.
	Search(query []float32, topK int) ([]Result, error)
}

// Result is a single search result from the vector store.
type Result struct {
	ID       string
	Score    float32 // cosine similarity, higher is better
	Distance float32 // optional: Euclidean distance, lower is better
}

package search

import "context"

// Query performs a hybrid search over an agent's indexed memory.
// It queries both the keyword index (FTS5) and the vector index,
// combining results with simple score fusion.
func (s *SQLiteIndex) Query(ctx context.Context, textQuery string, queryVec []float32, agentName string, clearance int, limit int) ([]DocumentResult, error) {
	if queryVec != nil && len(queryVec) > 0 {
		return s.HybridSearch(textQuery, queryVec, agentName, clearance, limit)
	}
	return s.KeywordSearch(textQuery, agentName, clearance, limit)
}

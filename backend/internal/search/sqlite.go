package search

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// SQLiteIndex implements a hybrid search index using SQLite with FTS5 for
// keyword search and JSON blobs for vector storage.
type SQLiteIndex struct {
	db *sql.DB
}

// NewSQLiteIndex opens (or creates) a SQLite search index at the given path.
func NewSQLiteIndex(path string) (*SQLiteIndex, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating search dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening search db: %w", err)
	}

	idx := &SQLiteIndex{db: db}
	if err := idx.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating search db: %w", err)
	}

	return idx, nil
}

// Close closes the database connection.
func (s *SQLiteIndex) Close() error {
	return s.db.Close()
}

func (s *SQLiteIndex) migrate() error {
	// Documents table: stores content chunks with metadata
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS documents (
			id          TEXT PRIMARY KEY,
			agent_name  TEXT NOT NULL,
			source_path TEXT NOT NULL,
			content     TEXT NOT NULL,
			clearance   INTEGER NOT NULL DEFAULT 1,
			chunk_type  TEXT NOT NULL DEFAULT 'memory'
		)
	`)
	if err != nil {
		return fmt.Errorf("creating documents table: %w", err)
	}

	// FTS5 virtual table for keyword search
	_, err = s.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			content,
			content='documents',
			content_rowid='rowid'
		)
	`)
	if err != nil {
		return fmt.Errorf("creating fts5 table: %w", err)
	}

	// Triggers to keep FTS5 in sync
	_, err = s.db.Exec(`
		CREATE TRIGGER IF NOT EXISTS documents_fts_insert AFTER INSERT ON documents BEGIN
			INSERT INTO documents_fts(rowid, content) VALUES (new.rowid, new.content);
		END
	`)
	if err != nil {
		return fmt.Errorf("creating fts insert trigger: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TRIGGER IF NOT EXISTS documents_fts_delete AFTER DELETE ON documents BEGIN
			INSERT INTO documents_fts(documents_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
		END
	`)
	if err != nil {
		return fmt.Errorf("creating fts delete trigger: %w", err)
	}

	// Vectors table: stores embedding vectors as JSON blobs
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS vectors (
			doc_id TEXT PRIMARY KEY,
			vector TEXT NOT NULL,
			FOREIGN KEY (doc_id) REFERENCES documents(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("creating vectors table: %w", err)
	}

	return nil
}

// AddDocument adds a document chunk to the index.
func (s *SQLiteIndex) AddDocument(id, agentName, sourcePath, content string, clearance int, chunkType string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO documents (id, agent_name, source_path, content, clearance, chunk_type)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, agentName, sourcePath, content, clearance, chunkType,
	)
	return err
}

// DeleteAgentDocuments removes all documents for a given agent.
func (s *SQLiteIndex) DeleteAgentDocuments(agentName string) error {
	_, err := s.db.Exec(`DELETE FROM documents WHERE agent_name = ?`, agentName)
	return err
}

// KeywordSearch performs full-text search over document content.
func (s *SQLiteIndex) KeywordSearch(query string, agentName string, clearance int, limit int) ([]DocumentResult, error) {
	// Query FTS5, join back to documents, filter by agent and clearance
	rows, err := s.db.Query(`
		SELECT d.id, d.agent_name, d.source_path, d.content, d.clearance, d.chunk_type
		FROM documents_fts fts
		JOIN documents d ON d.rowid = fts.rowid
		WHERE documents_fts MATCH ?
		  AND d.agent_name = ?
		  AND d.clearance <= ?
		ORDER BY rank
		LIMIT ?
	`, query, agentName, clearance, limit)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	defer rows.Close()

	return scanDocumentResults(rows)
}

// VectorSearch performs similarity search using stored vectors.
func (s *SQLiteIndex) VectorSearch(query []float32, agentName string, clearance int, limit int) ([]DocumentResult, error) {
	rows, err := s.db.Query(`
		SELECT d.id, d.agent_name, d.source_path, d.content, d.clearance, d.chunk_type, v.vector
		FROM vectors v
		JOIN documents d ON d.id = v.doc_id
		WHERE d.agent_name = ?
		  AND d.clearance <= ?
	`, agentName, clearance)
	if err != nil {
		return nil, fmt.Errorf("vector search query: %w", err)
	}
	defer rows.Close()

	var results []struct {
		doc   DocumentResult
		vec   []float32
		score float32
	}

	for rows.Next() {
		var id, agent, sourcePath, content, chunkType string
		var clearanceTier int
		var vecJSON string
		if err := rows.Scan(&id, &agent, &sourcePath, &content, &clearanceTier, &chunkType, &vecJSON); err != nil {
			return nil, fmt.Errorf("scanning vector result: %w", err)
		}

		var vec []float32
		if err := json.Unmarshal([]byte(vecJSON), &vec); err != nil {
			continue // skip malformed vectors
		}

		score := cosineSimilarity(query, vec)
		results = append(results, struct {
			doc   DocumentResult
			vec   []float32
			score float32
		}{
			doc: DocumentResult{
				ID:         id,
				AgentName:  agent,
				SourcePath: sourcePath,
				Content:    content,
				Clearance:  clearanceTier,
				ChunkType:  chunkType,
			},
			vec:   vec,
			score: score,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending and take top K
	// Simple insertion sort for small result sets
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	docResults := make([]DocumentResult, len(results))
	for i, r := range results {
		docResults[i] = r.doc
	}
	return docResults, nil
}

// HybridSearch combines keyword and vector search, deduplicating results.
// Each result gets a combined score.
func (s *SQLiteIndex) HybridSearch(keywordQuery string, queryVec []float32, agentName string, clearance int, limit int) ([]DocumentResult, error) {
	// Run both searches in parallel (both are fast, sequential is fine for v1)
	kwResults, err := s.KeywordSearch(keywordQuery, agentName, clearance, limit*2)
	if err != nil {
		return nil, err
	}

	vecResults, err := s.VectorSearch(queryVec, agentName, clearance, limit*2)
	if err != nil {
		return nil, err
	}

	// Merge and deduplicate, boosting items present in both
	seen := make(map[string]DocumentResult)
	scores := make(map[string]float32)

	for _, r := range kwResults {
		seen[r.ID] = r
		scores[r.ID] = 0.5 // keyword match base score
	}
	for _, r := range vecResults {
		if existing, ok := seen[r.ID]; ok {
			// Boost for appearing in both
			scores[r.ID] = 1.0
			_ = existing
		} else {
			seen[r.ID] = r
			scores[r.ID] = 0.5 // vector match base score
		}
	}

	// Sort by score
	type scored struct {
		id    string
		score float32
	}
	var scoredResults []scored
	for id, score := range scores {
		scoredResults = append(scoredResults, scored{id: id, score: score})
	}
	for i := 1; i < len(scoredResults); i++ {
		for j := i; j > 0 && scoredResults[j].score > scoredResults[j-1].score; j-- {
			scoredResults[j], scoredResults[j-1] = scoredResults[j-1], scoredResults[j]
		}
	}

	if len(scoredResults) > limit {
		scoredResults = scoredResults[:limit]
	}

	results := make([]DocumentResult, len(scoredResults))
	for i, s := range scoredResults {
		results[i] = seen[s.id]
	}
	return results, nil
}

// DocumentResult is a single search result.
type DocumentResult struct {
	ID         string
	AgentName  string
	SourcePath string
	Content    string
	Clearance  int
	ChunkType  string
}

// VectorStore methods (interface implementation)

func (s *SQLiteIndex) Insert(id string, vec []float32) error {
	jsonVec, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("marshaling vector: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO vectors (doc_id, vector) VALUES (?, ?)`,
		id, string(jsonVec),
	)
	return err
}

func (s *SQLiteIndex) Search(query []float32, topK int) ([]Result, error) {
	rows, err := s.db.Query(`SELECT doc_id, vector FROM vectors`)
	if err != nil {
		return nil, fmt.Errorf("querying vectors: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var id string
		var vecJSON string
		if err := rows.Scan(&id, &vecJSON); err != nil {
			return nil, fmt.Errorf("scanning vector: %w", err)
		}

		var vec []float32
		if err := json.Unmarshal([]byte(vecJSON), &vec); err != nil {
			continue
		}

		score := cosineSimilarity(query, vec)
		results = append(results, Result{ID: id, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func scanDocumentResults(rows *sql.Rows) ([]DocumentResult, error) {
	var results []DocumentResult
	for rows.Next() {
		var r DocumentResult
		if err := rows.Scan(&r.ID, &r.AgentName, &r.SourcePath, &r.Content, &r.Clearance, &r.ChunkType); err != nil {
			return nil, fmt.Errorf("scanning document: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

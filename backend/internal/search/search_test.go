package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

func TestSQLiteIndexDocumentCRUD(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSQLiteIndex(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndex failed: %v", err)
	}
	defer idx.Close()

	if err := idx.AddDocument("doc1", "housewife", "/data/housewife/MEMORY.md", "The user likes coffee.", 5, "memory"); err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	if err := idx.AddDocument("doc2", "housewife", "/data/housewife/MEMORY.md", "The user has a dog named Max.", 5, "memory"); err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	if err := idx.AddDocument("doc3", "scout", "/data/scout/MEMORY.md", "Web search results.", 2, "memory"); err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	results, err := idx.KeywordSearch("coffee", "housewife", 5, 10)
	if err != nil {
		t.Fatalf("KeywordSearch failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "doc1" {
		t.Fatalf("expected doc1, got %s", results[0].ID)
	}

	results, err = idx.KeywordSearch("coffee", "housewife", 2, 10)
	if err != nil {
		t.Fatalf("KeywordSearch failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results (clearance too low), got %d", len(results))
	}

	if err := idx.DeleteAgentDocuments("housewife"); err != nil {
		t.Fatalf("DeleteAgentDocuments failed: %v", err)
	}

	results, err = idx.KeywordSearch("coffee", "housewife", 5, 10)
	if err != nil {
		t.Fatalf("KeywordSearch failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}
}

func TestVectorStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSQLiteIndex(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndex failed: %v", err)
	}
	defer idx.Close()

	vec1 := []float32{1.0, 0.0, 0.0}
	vec2 := []float32{0.0, 1.0, 0.0}
	vec3 := []float32{0.9, 0.1, 0.0}

	if err := idx.Insert("v1", vec1); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if err := idx.Insert("v2", vec2); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if err := idx.Insert("v3", vec3); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	query := []float32{1.0, 0.0, 0.0}
	results, err := idx.Search(query, 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "v1" {
		t.Fatalf("expected v1 first, got %s", results[0].ID)
	}
	if results[0].Score < 0.99 {
		t.Fatalf("expected score ~1.0, got %f", results[0].Score)
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float32
	}{
		{[]float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{[]float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{[]float32{1, 1, 0}, []float32{1, 1, 0}, 1.0},
		{[]float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{[]float32{1, 0, 0}, []float32{0, 0, 0}, 0.0},
	}

	for _, tt := range tests {
		got := cosineSimilarity(tt.a, tt.b)
		diff := got - tt.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.001 {
			t.Errorf("cosineSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestChunkText(t *testing.T) {
	text := "Para 1.\n\nPara 2.\n\nPara 3.\n\nPara 4."

	chunks := chunkText(text, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for small text, got %d", len(chunks))
	}

	chunks = chunkText(text, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

type mockEmbedder struct {
	vectors [][]float32
	err     error
	callIdx int
}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := make([][]float32, len(texts))
	for i := range texts {
		if m.callIdx < len(m.vectors) {
			result[i] = m.vectors[m.callIdx]
			m.callIdx++
		}
	}
	return result, nil
}

func TestRebuilder(t *testing.T) {
	tmpDir := t.TempDir()
	p := paths.NewPathsFromRoots(tmpDir+"/config", tmpDir+"/data", tmpDir+"/cache")

	agentDir := filepath.Join(p.AgentsDataDir(), "housewife")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("creating agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "MEMORY.md"), []byte("The user likes coffee.\n\nThe user has a dog."), 0644); err != nil {
		t.Fatalf("writing MEMORY.md: %v", err)
	}

	dbPath := p.SearchCacheDir() + "/search.db"
	idx, err := NewSQLiteIndex(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndex failed: %v", err)
	}
	defer idx.Close()

	embedder := &mockEmbedder{
		vectors: [][]float32{
			{1.0, 0.0},
			{0.0, 1.0},
		},
	}

	rebuilder := NewRebuilder(idx, embedder, p)
	if err := rebuilder.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	results, err := idx.KeywordSearch("coffee", "housewife", 5, 10)
	if err != nil {
		t.Fatalf("KeywordSearch failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	vecResults, err := idx.Search([]float32{1.0, 0.0}, 10)
	if err != nil {
		t.Fatalf("Vector search failed: %v", err)
	}
	if len(vecResults) != 1 {
		t.Fatalf("expected 1 vector (single chunk), got %d", len(vecResults))
	}
}

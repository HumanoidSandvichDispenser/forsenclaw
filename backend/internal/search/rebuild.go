package search

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

// Rebuilder rebuilds the search index from agent memory files on disk.
type Rebuilder struct {
	index    *SQLiteIndex
	embedder Embedder
	paths    *paths.Paths
}

// NewRebuilder creates a rebuilder with the given index, embedder, and paths.
func NewRebuilder(index *SQLiteIndex, embedder Embedder, p *paths.Paths) *Rebuilder {
	return &Rebuilder{
		index:    index,
		embedder: embedder,
		paths:    p,
	}
}

// Rebuild walks all agent data directories and re-indexes all memory files.
func (r *Rebuilder) Rebuild(ctx context.Context) error {
	tx, err := r.index.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning rebuild transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear existing documents
	if _, err := tx.Exec(`DELETE FROM documents`); err != nil {
		return fmt.Errorf("clearing documents: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM vectors`); err != nil {
		return fmt.Errorf("clearing vectors: %w", err)
	}

	agentsDir := r.paths.AgentsDataDir()
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading agents data dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentName := entry.Name()
		agentDir := filepath.Join(agentsDir, agentName)

		if err := r.indexAgent(ctx, tx, agentName, agentDir); err != nil {
			return fmt.Errorf("indexing agent %q: %w", agentName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing rebuild transaction: %w", err)
	}
	return nil
}

func (r *Rebuilder) indexAgent(ctx context.Context, tx *sql.Tx, agentName, agentDir string) error {
	// Index MEMORY.md
	memPath := filepath.Join(agentDir, memory.MemoryFileName)
	if data, err := os.ReadFile(memPath); err == nil {
		chunks := chunkText(string(data), 1000) // ~1000 char chunks
		for i, chunk := range chunks {
			id := fmt.Sprintf("%s:memory:%d", agentName, i)
			if err := r.index.addDocumentExec(tx, id, agentName, memPath, chunk, 5, "memory"); err != nil {
				return fmt.Errorf("adding memory chunk: %w", err)
			}
			if err := r.embedAndStoreTx(ctx, tx, id, chunk); err != nil {
				return fmt.Errorf("embedding memory chunk: %w", err)
			}
		}
	}

	// Index daily notes
	notesDir := filepath.Join(agentDir, "memory")
	entries, err := os.ReadDir(notesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading daily notes dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(notesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		chunks := chunkText(string(data), 1000)
		for i, chunk := range chunks {
			id := fmt.Sprintf("%s:daily:%s:%d", agentName, entry.Name(), i)
			if err := r.index.addDocumentExec(tx, id, agentName, path, chunk, 5, "daily"); err != nil {
				return fmt.Errorf("adding daily chunk: %w", err)
			}
			if err := r.embedAndStoreTx(ctx, tx, id, chunk); err != nil {
				return fmt.Errorf("embedding daily chunk: %w", err)
			}
		}
	}

	return nil
}

func (r *Rebuilder) embedAndStoreTx(ctx context.Context, tx *sql.Tx, id, text string) error {
	vectors, err := r.embedder.Embed(ctx, []string{text})
	if err != nil {
		return err
	}
	if len(vectors) == 0 {
		return fmt.Errorf("no embedding returned")
	}
	return r.index.insertExec(tx, id, vectors[0])
}

// chunkText splits text into chunks of approximately targetSize characters,
// breaking at paragraph boundaries when possible.
func chunkText(text string, targetSize int) []string {
	if len(text) <= targetSize {
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var current strings.Builder

	for _, para := range paragraphs {
		if current.Len()+len(para)+2 > targetSize && current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

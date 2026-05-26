package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements RoomRepository and MessageRepository using SQLite
// for room metadata and compaction cursors, and JSONL transcript files for
// messages.
type SQLiteStore struct {
	db       *sql.DB
	roomsDir string
}

// Ensure SQLiteStore implements both repositories.
var _ RoomRepository = (*SQLiteStore)(nil)
var _ MessageRepository = (*SQLiteStore)(nil)

// NewSQLiteStore opens (or creates) the rooms database at dbPath and runs
// any pending migrations. roomsDir is the directory where JSONL transcript
// files are stored.
func NewSQLiteStore(dbPath, roomsDir string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &SQLiteStore{db: db, roomsDir: roomsDir}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// migrate runs the schema migrations. Migrations are idempotent.
func (s *SQLiteStore) migrate() error {
	const schemaV1 = `
	CREATE TABLE IF NOT EXISTS rooms (
		id           TEXT PRIMARY KEY,
		name         TEXT,
		participants TEXT NOT NULL,
		clearance    INTEGER NOT NULL DEFAULT 5,
		created_at   DATETIME NOT NULL,
		updated_at   DATETIME NOT NULL
	);
	`
	if _, err := s.db.Exec(schemaV1); err != nil {
		return fmt.Errorf("schema v1: %w", err)
	}

	const schemaV2 = `
	CREATE TABLE IF NOT EXISTS compaction_cursors (
		agent_name       TEXT NOT NULL,
		room_id          TEXT NOT NULL,
		compacted_offset INTEGER NOT NULL DEFAULT 0,
		updated_at       DATETIME NOT NULL,
		PRIMARY KEY (agent_name, room_id)
	);
	`
	if _, err := s.db.Exec(schemaV2); err != nil {
		return fmt.Errorf("schema v2: %w", err)
	}

	// Schema v3: add name column to existing databases.
	var hasName bool
	rows, err := s.db.Query(`SELECT 1 FROM pragma_table_info('rooms') WHERE name = 'name'`)
	if err != nil {
		return fmt.Errorf("schema v3 check: %w", err)
	}
	if rows.Next() {
		hasName = true
	}
	rows.Close()

	if !hasName {
		if _, err := s.db.Exec(`ALTER TABLE rooms ADD COLUMN name TEXT DEFAULT ''`); err != nil {
			return fmt.Errorf("schema v3: %w", err)
		}
	}

	// Schema v4: drop protocol columns from existing databases.
	var hasProtocolType bool
	protRows, err := s.db.Query(`SELECT 1 FROM pragma_table_info('rooms') WHERE name = 'protocol_type'`)
	if err != nil {
		return fmt.Errorf("schema v4 check: %w", err)
	}
	if protRows.Next() {
		hasProtocolType = true
	}
	protRows.Close()

	if hasProtocolType {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("schema v4 begin: %w", err)
		}
		v4stmts := []string{
			`CREATE TABLE rooms_v4 (
				id           TEXT PRIMARY KEY,
				name         TEXT DEFAULT '',
				participants TEXT NOT NULL,
				clearance    INTEGER NOT NULL DEFAULT 5,
				created_at   DATETIME NOT NULL,
				updated_at   DATETIME NOT NULL
			)`,
			`INSERT INTO rooms_v4 (id, name, participants, clearance, created_at, updated_at)
				SELECT id, name, participants, clearance_ceiling, created_at, updated_at FROM rooms`,
			`DROP TABLE rooms`,
			`ALTER TABLE rooms_v4 RENAME TO rooms`,
		}
		for _, stmt := range v4stmts {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("schema v4: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("schema v4 commit: %w", err)
		}
	}

	// Schema v5: rename clearance_ceiling to clearance for existing databases.
	var hasOldCol bool
	oldRows, err := s.db.Query(`SELECT 1 FROM pragma_table_info('rooms') WHERE name = 'clearance_ceiling'`)
	if err != nil {
		return fmt.Errorf("schema v5 check: %w", err)
	}
	if oldRows.Next() {
		hasOldCol = true
	}
	if err := oldRows.Close(); err != nil {
		return fmt.Errorf("schema v5 check close: %w", err)
	}

	if hasOldCol {
		if _, err := s.db.Exec(`ALTER TABLE rooms RENAME COLUMN clearance_ceiling TO clearance`); err != nil {
			return fmt.Errorf("schema v5: %w", err)
		}
	}

	return nil
}

// scanRoom reads a single room row from the given scannable.
func (s *SQLiteStore) scanRoom(sc interface {
	Scan(dest ...any) error
}) (*room.Room, error) {
	var r room.Room
	var participantsJSON string
	var createdAtStr, updatedAtStr string

	err := sc.Scan(
		&r.ID,
		&r.Name,
		&participantsJSON,
		&r.Clearance,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(participantsJSON), &r.Participants); err != nil {
		return nil, fmt.Errorf("unmarshal participants: %w", err)
	}

	r.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	return &r, nil
}

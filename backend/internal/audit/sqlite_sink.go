package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

// SQLiteSink writes audit events to a SQLite database.
type SQLiteSink struct {
	db *sql.DB
}

// NewSQLiteSink opens (or creates) the audit database at path and ensures the
// schema exists.
func NewSQLiteSink(path string) (*SQLiteSink, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS audit_events (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT    NOT NULL,
		level     TEXT    NOT NULL,
		kind      TEXT    NOT NULL,
		agent_id  TEXT,
		fields    TEXT
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create audit_events table: %w", err)
	}

	return &SQLiteSink{db: db}, nil
}

func (s *SQLiteSink) Write(e Event) error {
	fieldsJSON, err := json.Marshal(e.Fields)
	if err != nil {
		return fmt.Errorf("marshal fields: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO audit_events (timestamp, level, kind, agent_id, fields) VALUES (?, ?, ?, ?, ?)`,
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		e.Level.String(),
		string(e.Kind),
		e.AgentID,
		string(fieldsJSON),
	)
	return err
}

func (s *SQLiteSink) Close() error {
	return s.db.Close()
}

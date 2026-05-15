// Package room provides room storage, transcript I/O, and protocol implementations.
// The Store interface abstracts persistence so tests can use in-memory backends.
package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store defines the persistence contract for room metadata.
type Store interface {
	CreateRoom(ctx context.Context, room *Room) error
	GetRoom(ctx context.Context, id string) (*Room, error)
	ListRooms(ctx context.Context, opts ListOpts) ([]Room, error)
	UpdateRoom(ctx context.Context, room *Room) error
	DeleteRoom(ctx context.Context, id string) error
}

// Ensure SQLiteStore implements Store.
var _ Store = (*SQLiteStore)(nil)

// SQLiteStore persists room metadata in a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the rooms database at dbPath and runs
// any pending migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// migrate runs the schema migrations. Migrations are idempotent.
func (s *SQLiteStore) migrate() error {
	const schemaV1 = `
	CREATE TABLE IF NOT EXISTS rooms (
		id                TEXT PRIMARY KEY,
		participants      TEXT NOT NULL,
		clearance_ceiling INTEGER NOT NULL DEFAULT 5,
		protocol_type     TEXT NOT NULL DEFAULT 'freeform',
		protocol_config   TEXT,
		protocol_state    TEXT,
		created_at        DATETIME NOT NULL,
		updated_at        DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_rooms_protocol ON rooms(protocol_type);
	`

	if _, err := s.db.Exec(schemaV1); err != nil {
		return fmt.Errorf("schema v1: %w", err)
	}

	return nil
}

// CreateRoom persists a new room. The room ID must be set by the caller.
func (s *SQLiteStore) CreateRoom(ctx context.Context, room *Room) error {
	if room.ID == "" {
		return fmt.Errorf("room ID is required")
	}
	if err := room.Validate(); err != nil {
		return fmt.Errorf("invalid room: %w", err)
	}

	participantsJSON, err := json.Marshal(room.Participants)
	if err != nil {
		return fmt.Errorf("marshal participants: %w", err)
	}

	now := time.Now().UTC()
	if room.CreatedAt.IsZero() {
		room.CreatedAt = now
	}
	if room.UpdatedAt.IsZero() {
		room.UpdatedAt = now
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO rooms (id, participants, clearance_ceiling, protocol_type, protocol_config, protocol_state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		room.ID,
		participantsJSON,
		room.ClearanceCeiling,
		room.ProtocolType,
		room.ProtocolConfig,
		room.ProtocolState,
		room.CreatedAt.Format(time.RFC3339Nano),
		room.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert room: %w", err)
	}

	return nil
}

// GetRoom retrieves a room by ID. Returns an error if the room does not exist.
func (s *SQLiteStore) GetRoom(ctx context.Context, id string) (*Room, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, participants, clearance_ceiling, protocol_type, protocol_config, protocol_state, created_at, updated_at
		FROM rooms
		WHERE id = ?
	`, id)

	room, err := s.scanRoom(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("room %q not found", id)
		}
		return nil, fmt.Errorf("get room: %w", err)
	}

	return room, nil
}

// ListRooms returns rooms matching the given options, ordered by updated_at DESC.
// Participant filtering is done in Go rather than SQL for correctness with JSON
// serialization; the expected room count (hundreds) makes this acceptable.
// When a participant filter is active, LIMIT/OFFSET are applied after Go-side
// filtering to ensure the caller always receives up to opts.Limit results.
func (s *SQLiteStore) ListRooms(ctx context.Context, opts ListOpts) ([]Room, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 200 {
		opts.Limit = 200
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	query := `
		SELECT id, participants, clearance_ceiling, protocol_type, protocol_config, protocol_state, created_at, updated_at
		FROM rooms
		WHERE 1=1
	`
	args := []any{}

	if opts.Protocol != "" {
		query += ` AND protocol_type = ?`
		args = append(args, opts.Protocol)
	}

	query += ` ORDER BY updated_at DESC`

	// When participant filtering is active, omit LIMIT/OFFSET from SQL so that
	// Go-side filtering sees all candidates before pagination is applied.
	// Otherwise push pagination down to the DB for efficiency.
	if opts.Participant == "" {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, opts.Limit, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		room, err := s.scanRoom(rows)
		if err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		// Filter by participant in Go for correctness.
		if opts.Participant != "" && !room.hasParticipant(opts.Participant) {
			continue
		}
		rooms = append(rooms, *room)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	// Apply pagination in Go when a participant filter is active.
	if opts.Participant != "" {
		if opts.Offset >= len(rooms) {
			return []Room{}, nil
		}
		rooms = rooms[opts.Offset:]
		if len(rooms) > opts.Limit {
			rooms = rooms[:opts.Limit]
		}
	}

	return rooms, nil
}

// hasParticipant returns true if the room contains a participant with the given ID.
func (r *Room) hasParticipant(id string) bool {
	for _, p := range r.Participants {
		if p.ID == id {
			return true
		}
	}
	return false
}

// UpdateRoom updates the mutable fields of an existing room. The room must
// already exist in the database.
func (s *SQLiteStore) UpdateRoom(ctx context.Context, room *Room) error {
	if room.ID == "" {
		return fmt.Errorf("room ID is required")
	}
	if err := room.Validate(); err != nil {
		return fmt.Errorf("invalid room: %w", err)
	}

	participantsJSON, err := json.Marshal(room.Participants)
	if err != nil {
		return fmt.Errorf("marshal participants: %w", err)
	}

	room.UpdatedAt = time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
		UPDATE rooms
		SET participants = ?,
		    clearance_ceiling = ?,
		    protocol_type = ?,
		    protocol_config = ?,
		    protocol_state = ?,
		    updated_at = ?
		WHERE id = ?
	`,
		participantsJSON,
		room.ClearanceCeiling,
		room.ProtocolType,
		room.ProtocolConfig,
		room.ProtocolState,
		room.UpdatedAt.Format(time.RFC3339Nano),
		room.ID,
	)
	if err != nil {
		return fmt.Errorf("update room: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("room %q not found", room.ID)
	}

	return nil
}

// DeleteRoom removes a room and its metadata from the database. The JSONL
// transcript file is NOT deleted by this method; callers must handle that
// separately.
func (s *SQLiteStore) DeleteRoom(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM rooms WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete room: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("room %q not found", id)
	}

	return nil
}

// scanRoom reads a single room row from the given scannable.
func (s *SQLiteStore) scanRoom(sc interface {
	Scan(dest ...any) error
}) (*Room, error) {
	var room Room
	var participantsJSON string
	var protocolConfigNull, protocolStateNull sql.NullString
	var createdAtStr, updatedAtStr string

	err := sc.Scan(
		&room.ID,
		&participantsJSON,
		&room.ClearanceCeiling,
		&room.ProtocolType,
		&protocolConfigNull,
		&protocolStateNull,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(participantsJSON), &room.Participants); err != nil {
		return nil, fmt.Errorf("unmarshal participants: %w", err)
	}

	if protocolConfigNull.Valid {
		room.ProtocolConfig = json.RawMessage(protocolConfigNull.String)
	}
	if protocolStateNull.Valid {
		room.ProtocolState = json.RawMessage(protocolStateNull.String)
	}

	room.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	room.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	return &room, nil
}

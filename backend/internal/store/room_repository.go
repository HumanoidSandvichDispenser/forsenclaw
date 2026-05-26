package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// CreateRoom persists a new room. The room ID must be set by the caller.
func (s *SQLiteStore) CreateRoom(ctx context.Context, r *room.Room) error {
	if r.ID == "" {
		return fmt.Errorf("room ID is required")
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("invalid room: %w", err)
	}

	participantsJSON, err := json.Marshal(r.Participants)
	if err != nil {
		return fmt.Errorf("marshal participants: %w", err)
	}

	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO rooms (id, name, participants, clearance_ceiling, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.Name, participantsJSON, r.ClearanceCeiling,
		r.CreatedAt.Format(time.RFC3339Nano),
		r.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert room: %w", err)
	}
	return nil
}

// GetRoom retrieves a room by ID. Returns an error if the room does not exist.
func (s *SQLiteStore) GetRoom(ctx context.Context, id string) (*room.Room, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, participants, clearance_ceiling, created_at, updated_at
		FROM rooms WHERE id = ?
	`, id)

	r, err := s.scanRoom(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("room %q not found", id)
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	return r, nil
}

// ListRooms returns rooms matching the given options, ordered by updated_at DESC.
// Participant filtering is done in Go rather than SQL for correctness with JSON
// serialization; the expected room count (hundreds) makes this acceptable.
func (s *SQLiteStore) ListRooms(ctx context.Context, opts ListOpts) ([]room.Room, error) {
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
		SELECT id, name, participants, clearance_ceiling, created_at, updated_at
		FROM rooms ORDER BY updated_at DESC
	`
	args := []any{}

	// When participant filtering is active, omit LIMIT/OFFSET from SQL so
	// Go-side filtering sees all candidates before pagination is applied.
	if opts.Participant == "" {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, opts.Limit, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()

	var rooms []room.Room
	for rows.Next() {
		r, err := s.scanRoom(rows)
		if err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		if opts.Participant != "" && r.ParticipantByID(opts.Participant) == nil {
			continue
		}
		rooms = append(rooms, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	if opts.Participant != "" {
		if opts.Offset >= len(rooms) {
			return []room.Room{}, nil
		}
		rooms = rooms[opts.Offset:]
		if len(rooms) > opts.Limit {
			rooms = rooms[:opts.Limit]
		}
	}

	return rooms, nil
}

// UpdateRoom updates the mutable fields of an existing room.
func (s *SQLiteStore) UpdateRoom(ctx context.Context, r *room.Room) error {
	if r.ID == "" {
		return fmt.Errorf("room ID is required")
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("invalid room: %w", err)
	}

	participantsJSON, err := json.Marshal(r.Participants)
	if err != nil {
		return fmt.Errorf("marshal participants: %w", err)
	}

	r.UpdatedAt = time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
		UPDATE rooms SET name = ?, participants = ?, clearance_ceiling = ?, updated_at = ?
		WHERE id = ?
	`,
		r.Name, participantsJSON, r.ClearanceCeiling,
		r.UpdatedAt.Format(time.RFC3339Nano), r.ID,
	)
	if err != nil {
		return fmt.Errorf("update room: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("room %q not found", r.ID)
	}
	return nil
}

// DeleteRoom removes a room's metadata from the database. The JSONL transcript
// file is NOT deleted; callers must handle that separately.
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

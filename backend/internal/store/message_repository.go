package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// AppendMessage writes a message to the room's JSONL transcript file.
func (s *SQLiteStore) AppendMessage(ctx context.Context, roomID string, msg room.Message) error {
	w, err := NewTranscriptWriter(s.roomsDir, roomID)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer w.Close()
	return w.Append(ctx, msg)
}

// GetMessages returns messages for a room, applying the given options.
// If opts.Offset > 0, messages before that line index are skipped (compaction
// cursor boundary). If opts.Limit > 0, the last N messages are returned (tail
// behaviour). Time filters are applied before limit.
func (s *SQLiteStore) GetMessages(ctx context.Context, roomID string, opts ReadOpts) ([]room.Message, error) {
	msgs, err := ReadMessages(ctx, s.roomsDir, roomID, ReadOpts{
		After:  opts.After,
		Before: opts.Before,
	})
	if err != nil {
		return nil, err
	}

	if opts.Offset > 0 {
		if opts.Offset >= len(msgs) {
			return []room.Message{}, nil
		}
		msgs = msgs[opts.Offset:]
	}

	if opts.Limit > 0 && len(msgs) > opts.Limit {
		msgs = msgs[len(msgs)-opts.Limit:]
	}

	return msgs, nil
}

// GetCompactionOffset returns the number of messages already compacted for
// the given agent+room pair. Returns 0 if no record exists yet.
func (s *SQLiteStore) GetCompactionOffset(ctx context.Context, agentName, roomID string) (int, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT compacted_offset FROM compaction_cursors
		WHERE agent_name = ? AND room_id = ?
	`, agentName, roomID)

	var offset int
	err := row.Scan(&offset)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("get compaction offset: %w", err)
	}
	return offset, nil
}

// SetCompactionOffset upserts the compaction offset for an agent+room pair.
func (s *SQLiteStore) SetCompactionOffset(ctx context.Context, agentName, roomID string, offset int) error {
	if agentName == "" {
		return fmt.Errorf("agentName is required")
	}
	if roomID == "" {
		return fmt.Errorf("roomID is required")
	}
	if offset < 0 {
		return fmt.Errorf("offset must be non-negative")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO compaction_cursors (agent_name, room_id, compacted_offset, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_name, room_id) DO UPDATE SET
			compacted_offset = excluded.compacted_offset,
			updated_at = excluded.updated_at
	`, agentName, roomID, offset, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("set compaction offset: %w", err)
	}
	return nil
}

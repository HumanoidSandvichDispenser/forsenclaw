package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// AppendMessage writes a message to the room's message log, assigning the
// next per-room sequence number. Returns the assigned number or an error.
func (s *SQLiteStore) AppendMessage(ctx context.Context, roomID int64, msg room.Message) (int64, error) {
	if err := msg.Validate(); err != nil {
		return 0, fmt.Errorf("invalid message: %w", err)
	}

	var nextNumber int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(
			"SELECT COALESCE(MAX(number), 0) + 1 FROM messages WHERE room_id = ?",
			roomID,
		).Scan(&nextNumber).Error; err != nil {
			return fmt.Errorf("get next number: %w", err)
		}
		msg.RoomID = roomID
		msg.Number = nextNumber
		if err := tx.Create(&msg).Error; err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return nextNumber, nil
}

// GetMessages returns messages for a room, applying the given options.
// Messages are returned in chronological order (by number).
func (s *SQLiteStore) GetMessages(ctx context.Context, roomID int64, opts ReadOpts) ([]room.Message, error) {
	query := s.db.WithContext(ctx).Model(&room.Message{}).Where("room_id = ?", roomID)

	if opts.Offset > 0 {
		query = query.Where("number > ?", opts.Offset)
	}
	if opts.After != nil {
		query = query.Where("timestamp > ?", *opts.After)
	}
	if opts.Before != nil {
		query = query.Where("timestamp < ?", *opts.Before)
	}

	if opts.Limit > 0 {
		// Tail behaviour: get last N in chronological order
		var rev []room.Message
		err := query.Order("number DESC").Limit(opts.Limit).Find(&rev).Error
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		// Reverse to chronological order (TODO: can be done with a subquery)
		for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
			rev[i], rev[j] = rev[j], rev[i]
		}
		return rev, nil
	}

	var msgs []room.Message
	err := query.Order("number ASC").Find(&msgs).Error
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return msgs, nil
}

// GetCompactionOffset returns the last compacted message number for the
// given agent+room pair. Returns 0 if no record exists yet.
func (s *SQLiteStore) GetCompactionOffset(ctx context.Context, agentName string, roomID int64) (int, error) {
	var cursor CompactionCursor
	err := s.db.WithContext(ctx).
		Where("agent_name = ? AND room_id = ?", agentName, roomID).
		First(&cursor).Error
	if err != nil {
		// gorm.ErrRecordNotFound is expected for new pairs
		return 0, nil
	}
	return cursor.CompactedNumber, nil
}

// SetCompactionOffset upserts the compaction number for an agent+room pair.
func (s *SQLiteStore) SetCompactionOffset(ctx context.Context, agentName string, roomID int64, offset int) error {
	if agentName == "" {
		return fmt.Errorf("agentName is required")
	}
	if roomID <= 0 {
		return fmt.Errorf("roomID must be positive")
	}
	if offset < 0 {
		return fmt.Errorf("offset must be non-negative")
	}

	cursor := CompactionCursor{
		AgentName:       agentName,
		RoomID:          roomID,
		CompactedNumber: offset,
		UpdatedAt:       time.Now().UTC(),
	}

	return s.db.WithContext(ctx).Save(&cursor).Error
}

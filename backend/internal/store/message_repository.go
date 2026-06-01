package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// AppendMessage writes a message to the room's conversation tree, attaching it
// as a child of the current room head. Returns the assigned message ID.
func (s *SQLiteStore) AppendMessage(ctx context.Context, roomID int64, msg room.Message) (int64, error) {
	if err := msg.Validate(); err != nil {
		return 0, fmt.Errorf("invalid message: %w", err)
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Read the current head to use as parent.
		var head *int64
		if err := tx.Raw("SELECT head FROM rooms WHERE id = ?", roomID).Scan(&head).Error; err != nil {
			return fmt.Errorf("get room head: %w", err)
		}

		msg.RoomID = roomID
		msg.ParentID = head

		if err := tx.Create(&msg).Error; err != nil {
			return fmt.Errorf("insert message: %w", err)
		}

		// Upsert branch cursor: head → new message.
		if head != nil {
			if err := upsertBranchCursor(tx, *head, msg.ID); err != nil {
				return fmt.Errorf("upsert branch cursor: %w", err)
			}
		}

		// Advance the room's head.
		if err := tx.Exec("UPDATE rooms SET head = ? WHERE id = ?", msg.ID, roomID).Error; err != nil {
			return fmt.Errorf("update room head: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

// GetMessages returns the messages on the active branch of a room by walking
// up the parent chain from the head. Messages are returned in chronological
// order (oldest first).
//
// opts.Head overrides the room's stored head (useful for viewing a specific
// branch). opts.Limit caps the walk depth. opts.CompactionID stops the walk
// before any ancestor with id <= CompactionID.
func (s *SQLiteStore) GetMessages(ctx context.Context, roomID int64, opts ReadOpts) ([]room.Message, error) {
	headID := opts.Head
	if headID == 0 {
		var head *int64
		if err := s.db.WithContext(ctx).Raw("SELECT head FROM rooms WHERE id = ?", roomID).Scan(&head).Error; err != nil {
			return nil, fmt.Errorf("get room head: %w", err)
		}
		if head == nil {
			return nil, nil
		}
		headID = *head
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10000
	}

	// Walk up from head collecting IDs, then fetch full rows via GORM
	// (so serializers for Sender and ToolCalls are applied correctly).
	const cteQuery = `
		WITH RECURSIVE chain(id, parent_id, depth) AS (
			SELECT id, parent_id, 1
			FROM messages
			WHERE id = ?
			UNION ALL
			SELECT m.id, m.parent_id, c.depth + 1
			FROM messages m
			INNER JOIN chain c ON m.id = c.parent_id
			WHERE c.depth < ?
			  AND (? = 0 OR m.id > ?)
		)
		SELECT id FROM chain ORDER BY id ASC
	`
	var ids []int64
	if err := s.db.WithContext(ctx).Raw(cteQuery, headID, limit, opts.CompactionID, opts.CompactionID).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("get message ids: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	var msgs []room.Message
	q := s.db.WithContext(ctx).Where("id IN ?", ids).Order("id ASC")
	if opts.After != nil {
		q = q.Where("timestamp > ?", *opts.After)
	}
	if opts.Before != nil {
		q = q.Where("timestamp < ?", *opts.Before)
	}
	if err := q.Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return msgs, nil
}

// GetCompactionOffset returns the last compacted message ID for the given
// agent+room pair. Returns 0 if no record exists yet.
func (s *SQLiteStore) GetCompactionOffset(ctx context.Context, agentName string, roomID int64) (int64, error) {
	var cursor CompactionCursor
	err := s.db.WithContext(ctx).
		Where("agent_name = ? AND room_id = ?", agentName, roomID).
		First(&cursor).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return cursor.CompactedID, nil
}

// SetCompactionOffset upserts the compaction message ID for an agent+room pair.
func (s *SQLiteStore) SetCompactionOffset(ctx context.Context, agentName string, roomID int64, offset int64) error {
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
		AgentName:   agentName,
		RoomID:      roomID,
		CompactedID: offset,
		UpdatedAt:   time.Now().UTC(),
	}

	return s.db.WithContext(ctx).Save(&cursor).Error
}

// SwitchBranch sets the active branch to the subtree rooted at messageID.
// It updates the branch cursor for messageID's parent to point at messageID,
// then sets the room's head to the tip of messageID's subtree.
func (s *SQLiteStore) SwitchBranch(ctx context.Context, roomID int64, messageID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get the parent of the target message.
		var parentID *int64
		if err := tx.Raw("SELECT parent_id FROM messages WHERE id = ?", messageID).Scan(&parentID).Error; err != nil {
			return fmt.Errorf("get message parent: %w", err)
		}

		// Update the branch cursor for the parent to point at the target.
		if parentID != nil {
			if err := upsertBranchCursor(tx, *parentID, messageID); err != nil {
				return fmt.Errorf("upsert branch cursor: %w", err)
			}
		}

		// Walk down from messageID following branch cursors to find the tip.
		const tipQuery = `
			WITH RECURSIVE descent(id) AS (
				SELECT ?
				UNION ALL
				SELECT mbc.child_id
				FROM message_branch_cursors mbc
				INNER JOIN descent d ON mbc.parent_id = d.id
			)
			SELECT id FROM descent ORDER BY id DESC LIMIT 1
		`
		var tip int64
		if err := tx.Raw(tipQuery, messageID).Scan(&tip).Error; err != nil || tip == 0 {
			tip = messageID
		}

		// Set the room head to the tip.
		return tx.Exec("UPDATE rooms SET head = ? WHERE id = ?", tip, roomID).Error
	})
}

// GetSiblings returns all messages that share the same parent as messageID,
// including messageID itself, ordered by id.
func (s *SQLiteStore) GetSiblings(ctx context.Context, messageID int64) ([]room.Message, error) {
	var anchor struct {
		ParentID *int64
		RoomID   int64
	}
	if err := s.db.WithContext(ctx).Raw("SELECT parent_id, room_id FROM messages WHERE id = ?", messageID).Scan(&anchor).Error; err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}

	var msgs []room.Message
	var err error
	if anchor.ParentID == nil {
		// Root message — siblings are other root messages in the same room.
		err = s.db.WithContext(ctx).Where("room_id = ? AND parent_id IS NULL", anchor.RoomID).Order("id ASC").Find(&msgs).Error
	} else {
		err = s.db.WithContext(ctx).Where("parent_id = ?", *anchor.ParentID).Order("id ASC").Find(&msgs).Error
	}
	if err != nil {
		return nil, fmt.Errorf("get siblings: %w", err)
	}
	return msgs, nil
}

// upsertBranchCursor sets the active child for parentID in the branch cursor
// table, creating or replacing any existing entry.
func upsertBranchCursor(tx *gorm.DB, parentID, childID int64) error {
	return tx.Exec(
		"INSERT OR REPLACE INTO message_branch_cursors (parent_id, child_id) VALUES (?, ?)",
		parentID, childID,
	).Error
}

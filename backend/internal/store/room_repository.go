package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// CreateRoom persists a new room. The room ID is assigned by the database
// via autoincrement and populated on the passed-in room struct.
func (s *SQLiteStore) CreateRoom(ctx context.Context, r *room.Room) error {
	if err := r.Validate(); err != nil {
		return fmt.Errorf("invalid room: %w", err)
	}

	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(r).Error; err != nil {
			return fmt.Errorf("insert room: %w", err)
		}
		return saveParticipants(ctx, tx, r.ID, r.Participants)
	})
}

// GetRoom retrieves a room by ID. Returns an error if the room does not exist.
func (s *SQLiteStore) GetRoom(ctx context.Context, id int64) (*room.Room, error) {
	var r room.Room
	if err := s.db.WithContext(ctx).First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("room %d not found", id)
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	var err error
	r.Participants, err = loadParticipants(ctx, s.db, r.ID)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRooms returns rooms matching the given options, ordered by updated_at DESC.
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

	query := s.db.WithContext(ctx).Order("updated_at DESC").Limit(opts.Limit).Offset(opts.Offset)
	if opts.Participant != "" {
		sub := s.db.Model(&RoomParticipant{}).Select("room_id").Where("actor_id = ?", opts.Participant)
		query = query.Where("id IN (?)", sub)
	}

	var rooms []room.Room
	if err := query.Find(&rooms).Error; err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}

	if err := loadParticipantsForRooms(ctx, s.db, rooms); err != nil {
		return nil, err
	}
	return rooms, nil
}

// UpdateRoom updates the mutable fields of an existing room.
func (s *SQLiteStore) UpdateRoom(ctx context.Context, r *room.Room) error {
	if r.ID <= 0 {
		return fmt.Errorf("room ID is required")
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("invalid room: %w", err)
	}

	r.UpdatedAt = time.Now().UTC()

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Select("name", "clearance", "updated_at").Updates(r)
		if result.Error != nil {
			return fmt.Errorf("update room: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("room %d not found", r.ID)
		}
		if err := deleteParticipants(ctx, tx, r.ID); err != nil {
			return err
		}
		return saveParticipants(ctx, tx, r.ID, r.Participants)
	})
}

// DeleteRoom removes a room and all associated data (participants, messages,
// compaction cursors) from the database.
func (s *SQLiteStore) DeleteRoom(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deleteParticipants(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.Where("room_id = ?", id).Delete(&room.Message{}).Error; err != nil {
			return fmt.Errorf("delete messages for room %d: %w", id, err)
		}
		if err := tx.Where("room_id = ?", id).Delete(&CompactionCursor{}).Error; err != nil {
			return fmt.Errorf("delete compaction cursors for room %d: %w", id, err)
		}
		result := tx.Delete(&room.Room{}, id)
		if result.Error != nil {
			return fmt.Errorf("delete room: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("room %d not found", id)
		}
		return nil
	})
}

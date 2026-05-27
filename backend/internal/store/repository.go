// Package store provides the persistence layer for Hearth: repository
// interfaces, query types, and the SQLite implementation.
package store

import (
	"context"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// ---------------------------------------------------------------------------
// Query options
// ---------------------------------------------------------------------------

// ListOpts controls pagination and filtering for ListRooms.
type ListOpts struct {
	// Participant filters to rooms containing this actor ID.
	Participant string

	// Limit is the maximum number of rooms to return.
	Limit int

	// Offset is the number of rooms to skip.
	Offset int
}

// ReadOpts controls filtering and pagination for GetMessages.
type ReadOpts struct {
	// Limit is the maximum number of messages to return (0 = all).
	Limit int

	// Offset is the compaction cursor: only messages with number > Offset
	// are returned.
	Offset int

	// After returns only messages strictly after this time.
	After *time.Time

	// Before returns only messages strictly before this time.
	Before *time.Time
}

// ---------------------------------------------------------------------------
// Repository interfaces
// ---------------------------------------------------------------------------

// RoomRepository is the persistence contract for room metadata.
type RoomRepository interface {
	CreateRoom(ctx context.Context, r *room.Room) error
	GetRoom(ctx context.Context, id int64) (*room.Room, error)
	ListRooms(ctx context.Context, opts ListOpts) ([]room.Room, error)
	UpdateRoom(ctx context.Context, r *room.Room) error
	DeleteRoom(ctx context.Context, id int64) error
}

// MessageRepository is the persistence contract for room messages and
// per-agent compaction state.
type MessageRepository interface {
	AppendMessage(ctx context.Context, roomID int64, msg room.Message) (int64, error)
	GetMessages(ctx context.Context, roomID int64, opts ReadOpts) ([]room.Message, error)
	GetCompactionOffset(ctx context.Context, agentName string, roomID int64) (int, error)
	SetCompactionOffset(ctx context.Context, agentName string, roomID int64, offset int) error
}

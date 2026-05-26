// Package store provides the persistence layer for Hearth: repository
// interfaces, query types, and the SQLite implementation.
package store

import (
	"context"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

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

	// Offset is the number of messages to skip from the start (compaction
	// cursor line index). 0 means no offset.
	Offset int

	// After returns only messages strictly after this time.
	After *time.Time

	// Before returns only messages strictly before this time.
	Before *time.Time
}

// RoomRepository is the persistence contract for room metadata.
type RoomRepository interface {
	CreateRoom(ctx context.Context, r *room.Room) error
	GetRoom(ctx context.Context, id string) (*room.Room, error)
	ListRooms(ctx context.Context, opts ListOpts) ([]room.Room, error)
	UpdateRoom(ctx context.Context, r *room.Room) error
	DeleteRoom(ctx context.Context, id string) error
}

// MessageRepository is the persistence contract for room messages and
// per-agent compaction state.
type MessageRepository interface {
	AppendMessage(ctx context.Context, roomID string, msg room.Message) error
	GetMessages(ctx context.Context, roomID string, opts ReadOpts) ([]room.Message, error)
	GetCompactionOffset(ctx context.Context, agentName, roomID string) (int, error)
	SetCompactionOffset(ctx context.Context, agentName, roomID string, offset int) error
}

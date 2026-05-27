package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// RoomParticipant is the normalized persistence model for room participants.
// Each actor in a room gets one row, keyed by (room_id, actor_id).
type RoomParticipant struct {
	RoomID    int64          `gorm:"primaryKey;not null"`
	ActorID   string         `gorm:"primaryKey;column:actor_id;not null"`
	ActorType room.ActorType `gorm:"column:actor_type;not null"`
	Clearance int            `gorm:"not null;default:0"`
	Name      string         `gorm:"default:''"`
}

func (rp RoomParticipant) toActor() room.Actor {
	return room.Actor{
		ID:        rp.ActorID,
		Type:      rp.ActorType,
		Clearance: rp.Clearance,
		Name:      rp.Name,
	}
}

func participantFromActor(roomID int64, a room.Actor) RoomParticipant {
	return RoomParticipant{
		RoomID:    roomID,
		ActorID:   a.ID,
		ActorType: a.Type,
		Clearance: a.Clearance,
		Name:      a.Name,
	}
}

func loadParticipants(ctx context.Context, db *gorm.DB, roomID int64) ([]room.Actor, error) {
	var rows []RoomParticipant
	if err := db.WithContext(ctx).Where("room_id = ?", roomID).Order("actor_id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load participants for room %d: %w", roomID, err)
	}
	actors := make([]room.Actor, len(rows))
	for i, rp := range rows {
		actors[i] = rp.toActor()
	}
	return actors, nil
}

// loadParticipantsForRooms loads all participants for a slice of rooms in a
// single query and assigns them. Modifies rooms in place.
func loadParticipantsForRooms(ctx context.Context, db *gorm.DB, rooms []room.Room) error {
	if len(rooms) == 0 {
		return nil
	}
	ids := make([]int64, len(rooms))
	for i, r := range rooms {
		ids[i] = r.ID
	}
	var rows []RoomParticipant
	if err := db.WithContext(ctx).Where("room_id IN ?", ids).Find(&rows).Error; err != nil {
		return fmt.Errorf("load participants: %w", err)
	}
	byRoom := make(map[int64][]room.Actor, len(rooms))
	for _, rp := range rows {
		byRoom[rp.RoomID] = append(byRoom[rp.RoomID], rp.toActor())
	}
	for i := range rooms {
		rooms[i].Participants = byRoom[rooms[i].ID]
	}
	return nil
}

func saveParticipants(ctx context.Context, db *gorm.DB, roomID int64, actors []room.Actor) error {
	rows := make([]RoomParticipant, len(actors))
	for i, a := range actors {
		rows[i] = participantFromActor(roomID, a)
	}
	if err := db.WithContext(ctx).Create(&rows).Error; err != nil {
		return fmt.Errorf("save participants for room %d: %w", roomID, err)
	}
	return nil
}

func deleteParticipants(ctx context.Context, db *gorm.DB, roomID int64) error {
	if err := db.WithContext(ctx).Where("room_id = ?", roomID).Delete(&RoomParticipant{}).Error; err != nil {
		return fmt.Errorf("delete participants for room %d: %w", roomID, err)
	}
	return nil
}

package dispatch

import (
	"context"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// loadCrossRoomFeed reads recent messages from all other rooms the agent
// participates in, excluding the current room.
func (d *Dispatcher) loadCrossRoomFeed(ctx context.Context, ag *agent.Agent, currentRoomID string) ([]memory.CrossRoomMessage, error) {
	rooms, err := d.store.ListRooms(ctx, room.ListOpts{
		Participant: "agent:" + ag.Name(),
		Limit:       200,
	})
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}

	var feed []memory.CrossRoomMessage
	for _, r := range rooms {
		if r.ID == currentRoomID {
			continue
		}

		cursor, err := d.store.GetCompactionCursor(ctx, ag.Name(), r.ID)
		if err != nil {
			return nil, fmt.Errorf("get cursor for room %s: %w", r.ID, err)
		}

		msgs, err := room.ReadMessagesTail(d.paths.RoomsDir(), r.ID, cursor.Offset, d.ctxConfig.OtherRoomWindow)
		if err != nil {
			return nil, fmt.Errorf("read tail for room %s: %w", r.ID, err)
		}

		for _, m := range msgs {
			if m.ClearanceTag > ag.Definition.Clearance {
				continue
			}
			feed = append(feed, memory.CrossRoomMessage{Message: m, RoomID: r.ID})
		}
	}

	return feed, nil
}

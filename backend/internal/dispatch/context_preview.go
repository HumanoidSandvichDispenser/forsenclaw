package dispatch

import (
	"context"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// PreviewOptions controls what inputs are included in context preview.
type PreviewOptions struct {
	IncludeCrossRoom   bool
	IncludeInterjections bool
}

// PreviewContext assembles the context window for a room/agent pair without
// mutating compaction state or writing daily notes.
func (d *Dispatcher) PreviewContext(ctx context.Context, ag *agent.Agent, roomID string, opts PreviewOptions) (*memory.AssembledContext, *room.CompactionCursor, error) {
	if ag == nil {
		return nil, nil, fmt.Errorf("agent is nil")
	}

	cursor, err := d.store.GetCompactionCursor(ctx, ag.Name(), roomID)
	if err != nil {
		return nil, nil, fmt.Errorf("get compaction cursor: %w", err)
	}

	currentHistory, err := room.ReadMessagesTail(d.paths.RoomsDir(), roomID, cursor.Offset, d.ctxConfig.CurrentRoomWindow)
	if err != nil {
		return nil, nil, fmt.Errorf("read current room tail: %w", err)
	}

	var crossRoomFeed []memory.CrossRoomMessage
	if opts.IncludeCrossRoom {
		crossRoomFeed, err = d.loadCrossRoomFeed(ctx, ag, roomID)
		if err != nil {
			return nil, nil, fmt.Errorf("load cross-room feed: %w", err)
		}
	}

	var interjections []room.Message
	if opts.IncludeInterjections {
		interjections = d.interjectionsSnapshot(roomID)
	}

	assembled, err := d.assembler.Assemble(ctx, ag, memory.AssembleRequest{
		RoomID:             roomID,
		CrossRoomFeed:      crossRoomFeed,
		CurrentRoomHistory: currentHistory,
		Interjections:      interjections,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("assemble context: %w", err)
	}

	return assembled, cursor, nil
}

func (d *Dispatcher) interjectionsSnapshot(roomID string) []room.Message {
	d.mu.RLock()
	defer d.mu.RUnlock()
	interjections := d.interjections[roomID]
	if len(interjections) == 0 {
		return nil
	}
	snapshot := make([]room.Message, len(interjections))
	copy(snapshot, interjections)
	return snapshot
}

package dispatch

import (
	"context"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// appendToTranscript writes a message to the room's JSONL transcript file.
func (d *Dispatcher) appendToTranscript(ctx context.Context, roomID string, msg room.Message) error {
	d.mu.Lock()
	writer, ok := d.transcripts[roomID]
	if !ok {
		var err error
		writer, err = room.NewTranscriptWriter(d.paths.RoomsDir(), roomID)
		if err != nil {
			d.mu.Unlock()
			return fmt.Errorf("create transcript writer: %w", err)
		}
		d.transcripts[roomID] = writer
	}
	d.mu.Unlock()

	return writer.Append(ctx, msg)
}

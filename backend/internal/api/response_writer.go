package api

import (
	"context"
	"fmt"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
)

// AgentResponseWriter writes completed agent responses to the room transcript
// and broadcasts a message.created event to connected WebSocket clients.
// It implements agent.ResponseWriter.
type AgentResponseWriter struct {
	rooms    store.RoomRepository
	messages store.MessageRepository
	hub      *Hub
}

// NewAgentResponseWriter creates an AgentResponseWriter backed by the given
// store and hub.
func NewAgentResponseWriter(rooms store.RoomRepository, messages store.MessageRepository, hub *Hub) *AgentResponseWriter {
	return &AgentResponseWriter{rooms: rooms, messages: messages, hub: hub}
}

// WriteAgentResponse looks up the room, appends the agent message, and
// broadcasts a message.created event to subscribers.
func (w *AgentResponseWriter) WriteAgentResponse(ctx context.Context, roomID int64, agentName string, content string) error {
	r, err := w.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return fmt.Errorf("looking up room %d: %w", roomID, err)
	}

	actorID := "agent:" + agentName
	sender := r.ParticipantByID(actorID)
	if sender == nil {
		return fmt.Errorf("agent %q not found in room %d", actorID, roomID)
	}

	msg := room.Message{
		Timestamp:    time.Now().UTC(),
		RoomID:       roomID,
		Sender:       *sender,
		ClearanceTag: min(sender.Clearance, r.Clearance),
		Type:         room.MessageText,
		Content:      content,
	}

	number, err := w.messages.AppendMessage(ctx, roomID, msg)
	if err != nil {
		return fmt.Errorf("appending message: %w", err)
	}
	msg.Number = number

	w.hub.Broadcast(roomID, dispatch.StreamEvent{
		Type:    "message.created",
		Payload: msg,
	})
	return nil
}

package api

import (
	"context"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

type AgentStreamWriter struct {
	hub *Hub
}

func NewAgentStreamWriter(hub *Hub) *AgentStreamWriter {
	return &AgentStreamWriter{hub: hub}
}

func (w *AgentStreamWriter) StreamAgentDelta(
	ctx context.Context,
	roomID int64,
	agentName string,
	delta string,
) error {
	w.hub.Broadcast(roomID, dispatch.StreamEvent{
		Type: "message.delta",
		Payload: room.MessageDelta{
			RoomID: roomID,
			Actor: room.Actor{
				Type: "agent",
				ID:   "agent:" + agentName,
				Name: agentName,
			},
			Delta: delta,
		},
	})
	return nil
}

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
				Type: room.ActorAgent,
				ID:   room.AgentID(agentName),
				Name: agentName,
			},
			Delta: delta,
		},
	})
	return nil
}

// AgentError is the payload for an agent.error stream event: a failed inference
// turn for an agent in a room.
type AgentError struct {
	RoomID    int64  `json:"room_id"`
	AgentName string `json:"agent_name"`
	Message   string `json:"message"`
}

func (w *AgentStreamWriter) StreamAgentError(
	ctx context.Context,
	roomID int64,
	agentName string,
	message string,
) error {
	w.hub.Broadcast(roomID, dispatch.StreamEvent{
		Type: "agent.error",
		Payload: AgentError{
			RoomID:    roomID,
			AgentName: agentName,
			Message:   message,
		},
	})
	return nil
}

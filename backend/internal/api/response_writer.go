package api

import (
	"context"
	"fmt"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
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
func (w *AgentResponseWriter) WriteAgentResponse(ctx context.Context, roomID int64, agentName string, content string, toolCalls []inference.ToolCallWire, inputTokens, outputTokens int) error {
	r, err := w.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return fmt.Errorf("looking up room %d: %w", roomID, err)
	}

	actorID := room.AgentID(agentName)
	sender := r.ParticipantByID(actorID)
	if sender == nil {
		return fmt.Errorf("agent %q not found in room %d", actorID, roomID)
	}

	msgType := room.MessageText
	var records []room.ToolCallRecord
	if len(toolCalls) > 0 {
		msgType = room.MessageToolCall
		for _, tc := range toolCalls {
			records = append(records, room.ToolCallRecord{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}

	msg := room.Message{
		Timestamp:         time.Now().UTC(),
		RoomID:            roomID,
		Sender:            *sender,
		ClearanceTag:      min(sender.Clearance, r.Clearance),
		Type:              msgType,
		Content:           content,
		ToolCalls:         records,
		UsageInputTokens:  inputTokens,
		UsageOutputTokens: outputTokens,
	}

	id, err := w.messages.AppendMessage(ctx, roomID, msg)
	if err != nil {
		return fmt.Errorf("appending message: %w", err)
	}
	msg.ID = id

	w.hub.Broadcast(roomID, dispatch.StreamEvent{
		Type:    "message.created",
		Payload: msg,
	})
	return nil
}

// WriteToolResult appends a tool result message and broadcasts it.
func (w *AgentResponseWriter) WriteToolResult(ctx context.Context, roomID int64, agentName string, toolCallID string, toolName string, result string) error {
	r, err := w.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return fmt.Errorf("looking up room %d: %w", roomID, err)
	}

	actorID := room.AgentID(agentName)
	sender := r.ParticipantByID(actorID)
	if sender == nil {
		return fmt.Errorf("agent %q not found in room %d", actorID, roomID)
	}

	msg := room.Message{
		Timestamp:    time.Now().UTC(),
		RoomID:       roomID,
		Sender:       *sender,
		ClearanceTag: min(sender.Clearance, r.Clearance),
		Type:         room.MessageToolResult,
		Content:      result,
		ToolCallID:   toolCallID,
		ToolName:     toolName,
	}

	id, err := w.messages.AppendMessage(ctx, roomID, msg)
	if err != nil {
		return fmt.Errorf("appending tool result: %w", err)
	}
	msg.ID = id

	w.hub.Broadcast(roomID, dispatch.StreamEvent{
		Type:    "message.created",
		Payload: msg,
	})
	return nil
}

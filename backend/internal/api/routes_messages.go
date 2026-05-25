package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// ---------------------------------------------------------------------------
// Message routes
// ---------------------------------------------------------------------------

func registerMessageRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "send-message",
		Method:      http.MethodPost,
		Path:        "/api/rooms/{room_id}/messages",
		Summary:     "Send a message to a room",
		Tags:        []string{"Messages"},
	}, func(ctx context.Context, input *SendMessageRequest) (*SendMessageResponse, error) {
		return svc.sendMessage(ctx, input)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-messages",
		Method:      http.MethodGet,
		Path:        "/api/rooms/{room_id}/messages",
		Summary:     "List messages in a room",
		Tags:        []string{"Messages"},
	}, func(ctx context.Context, input *ListMessagesRequest) (*ListMessagesResponse, error) {
		return svc.listMessages(ctx, input)
	})
}

// ---------------------------------------------------------------------------
// Message handlers
// ---------------------------------------------------------------------------

func (svc *Service) sendMessage(ctx context.Context, input *SendMessageRequest) (*SendMessageResponse, error) {
	// Look up room
	r, err := svc.store.GetRoom(ctx, input.RoomID)
	if err != nil {
		return nil, huma.Error404NotFound("room not found")
	}

	// Resolve sender actor from participant list
	sender := r.ParticipantByID(input.Body.Sender)
	if sender == nil {
		return nil, huma.Error400BadRequest(fmt.Sprintf("sender %q is not a participant in this room", input.Body.Sender))
	}

	// Build message
	msg := room.Message{
		ID:           uuid.New().String(),
		Timestamp:    time.Now().UTC(),
		RoomID:       r.ID,
		Sender:       *sender,
		ClearanceTag: sender.Clearance,
		Type:         room.MessageText,
		Content:      input.Body.Content,
	}

	// Write message to transcript
	writer, err := room.NewTranscriptWriter(svc.paths.RoomsDir(), r.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to open transcript: " + err.Error())
	}
	if err := writer.Append(ctx, msg); err != nil {
		writer.Close()
		return nil, huma.Error500InternalServerError("failed to write message: " + err.Error())
	}
	writer.Close()

	// Submit to dispatcher for each agent participant
	for _, p := range r.Participants {
		if !p.IsAgent() {
			continue
		}
		agentName := strings.TrimPrefix(p.ID, "agent:")
		svc.dispatcher.Submit(agent.Request{
			ID:     uuid.New().String(),
			Target: agentName,
			Source: agent.SourceRoom,
			Payload: agent.RequestPayload{
				RoomID: r.ID,
				Messages: []agent.Message{{
					Sender:    msg.Sender.Name,
					Content:   msg.Content,
					Timestamp: msg.Timestamp,
					Type:      agent.MessageText,
				}},
			},
		})
	}

	resp := &SendMessageResponse{}
	resp.Body = toMessageResponse(msg)
	return resp, nil
}

func (svc *Service) listMessages(ctx context.Context, input *ListMessagesRequest) (*ListMessagesResponse, error) {
	// Verify room exists
	if _, err := svc.store.GetRoom(ctx, input.RoomID); err != nil {
		return nil, huma.Error404NotFound("room not found")
	}

	// Parse optional time filter
	var before *time.Time
	if input.Before != "" {
		t, err := time.Parse(time.RFC3339, input.Before)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid 'before' timestamp: must be RFC3339")
		}
		before = &t
	}

	opts := room.ReadOpts{
		Limit:  input.Limit,
		Before: before,
	}

	msgs, err := room.ReadMessages(ctx, svc.paths.RoomsDir(), input.RoomID, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read messages: "+err.Error())
	}

	resp := &ListMessagesResponse{}
	resp.Body.Messages = make([]MessageResponse, len(msgs))
	for i, m := range msgs {
		resp.Body.Messages[i] = toMessageResponse(m)
	}
	return resp, nil
}

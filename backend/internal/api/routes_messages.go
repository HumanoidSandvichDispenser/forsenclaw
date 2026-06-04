package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
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
	r, err := svc.rooms.GetRoom(ctx, input.RoomID)
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
		Timestamp:    time.Now().UTC(),
		RoomID:       r.ID,
		Sender:       *sender,
		ClearanceTag: min(sender.Clearance, r.Clearance),
		Type:         room.MessageText,
		Content:      input.Body.Content,
	}

	// Write message (number assigned by DB)
	_, err = svc.messages.AppendMessage(ctx, r.ID, msg)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to write message: " + err.Error())
	}

	// Submit to dispatcher for each agent participant
	svc.dispatchToAgents(r, msg)

	resp := &SendMessageResponse{}
	resp.Body = toMessageResponse(msg)
	return resp, nil
}

func (svc *Service) listMessages(ctx context.Context, input *ListMessagesRequest) (*ListMessagesResponse, error) {
	// Verify room exists
	if _, err := svc.rooms.GetRoom(ctx, input.RoomID); err != nil {
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

	msgs, err := svc.messages.GetMessages(ctx, input.RoomID, store.ReadOpts{
		Limit:  input.Limit,
		Before: before,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read messages: " + err.Error())
	}

	resp := &ListMessagesResponse{}
	resp.Body.Messages = make([]MessageResponse, len(msgs))
	for i, m := range msgs {
		mr := toMessageResponse(m)
		// Annotate with sibling IDs so the client can render and navigate
		// alternative branches. Only populated at a real fork (>1 sibling).
		// Active-branch messages each have a distinct parent, so this is one
		// lookup per message with no redundancy.
		if siblings, err := svc.messages.GetSiblings(ctx, m.ID); err == nil && len(siblings) > 1 {
			ids := make([]int64, len(siblings))
			for j, s := range siblings {
				ids[j] = s.ID
			}
			mr.SiblingIDs = ids
		}
		resp.Body.Messages[i] = mr
	}
	return resp, nil
}

// dispatchToAgents submits a room request to every agent participant — a fan-out
// turn, as when a user message is sent or edited.
func (svc *Service) dispatchToAgents(r *room.Room, msg room.Message) {
	for _, p := range r.Participants {
		if !p.IsAgent() {
			continue
		}
		_, agentName, _ := room.SplitActorID(p.ID)
		svc.dispatchToAgent(agentName, r.ID, msg)
	}
}

// dispatchToAgent submits a room request to a single agent — used by retry and
// tool-result edits, where only the turn's authoring agent should re-run.
func (svc *Service) dispatchToAgent(agentName string, roomID int64, msg room.Message) {
	svc.dispatcher.Submit(agent.Request{
		Target: agentName,
		Source: agent.SourceRoom,
		Payload: agent.RequestPayload{
			RoomID: roomID,
			Messages: []agent.Message{{
				Sender:    msg.Sender.Name,
				Content:   msg.Content,
				Timestamp: msg.Timestamp,
				Type:      agent.MessageText,
			}},
		},
	})
}

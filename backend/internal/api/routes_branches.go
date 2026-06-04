package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// ---------------------------------------------------------------------------
// Branching routes
//
// A fork is "rewind the room head to a fork point, then continue." Editing a
// message forks a new sibling with changed content; retrying re-runs the
// authoring agent from the same input; switching navigates between existing
// siblings.
// ---------------------------------------------------------------------------

func registerBranchRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "switch-branch",
		Method:      http.MethodPost,
		Path:        "/api/rooms/{room_id}/messages/{message_id}/switch",
		Summary:     "Switch the active branch to a sibling message",
		Tags:        []string{"Messages"},
	}, func(ctx context.Context, input *SwitchBranchRequest) (*SwitchBranchResponse, error) {
		return svc.switchBranch(ctx, input)
	})

	huma.Register(api, huma.Operation{
		OperationID: "edit-message",
		Method:      http.MethodPost,
		Path:        "/api/rooms/{room_id}/messages/{message_id}/edit",
		Summary:     "Fork a sibling of a message with edited content",
		Tags:        []string{"Messages"},
	}, func(ctx context.Context, input *EditMessageRequest) (*EditMessageResponse, error) {
		return svc.editMessage(ctx, input)
	})

	huma.Register(api, huma.Operation{
		OperationID: "retry-message",
		Method:      http.MethodPost,
		Path:        "/api/rooms/{room_id}/messages/{message_id}/retry",
		Summary:     "Regenerate an assistant message as a new sibling",
		Tags:        []string{"Messages"},
	}, func(ctx context.Context, input *RetryMessageRequest) (*RetryMessageResponse, error) {
		return svc.retryMessage(ctx, input)
	})
}

func (svc *Service) switchBranch(ctx context.Context, input *SwitchBranchRequest) (*SwitchBranchResponse, error) {
	if _, err := svc.messages.GetMessage(ctx, input.MessageID); err != nil {
		return nil, huma.Error404NotFound("message not found")
	}
	if err := svc.messages.SwitchBranch(ctx, input.RoomID, input.MessageID); err != nil {
		return nil, huma.Error500InternalServerError("failed to switch branch: " + err.Error())
	}
	return &SwitchBranchResponse{}, nil
}

func (svc *Service) editMessage(ctx context.Context, input *EditMessageRequest) (*EditMessageResponse, error) {
	r, err := svc.rooms.GetRoom(ctx, input.RoomID)
	if err != nil {
		return nil, huma.Error404NotFound("room not found")
	}
	orig, err := svc.messages.GetMessage(ctx, input.MessageID)
	if err != nil {
		return nil, huma.Error404NotFound("message not found")
	}
	if orig.RoomID != r.ID {
		return nil, huma.Error404NotFound("message not found in this room")
	}

	// Rewind the head to the fork point (the original's parent) so the edited
	// message lands as a new sibling rather than extending the current branch.
	if err := svc.messages.SetHead(ctx, r.ID, orig.ParentID); err != nil {
		return nil, huma.Error500InternalServerError("failed to set head: " + err.Error())
	}

	// Clone the original's metadata (sender, type, tool correlation) and replace
	// only the content. Zero the ID so the DB assigns a fresh one; AppendMessage
	// sets the parent from the head we just rewound to.
	sibling := orig
	sibling.ID = 0
	sibling.Content = input.Body.Content
	sibling.Timestamp = time.Now().UTC()
	newID, err := svc.messages.AppendMessage(ctx, r.ID, sibling)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to write edited message: " + err.Error())
	}
	sibling.ID = newID
	sibling.ParentID = orig.ParentID
	sibling.RoomID = r.ID

	// Re-run the agent only when the edited message is an input to a turn: a
	// user message (the agent should respond) or a tool result (the authoring
	// agent should resume its turn). Editing an agent's own text is a manual
	// correction and triggers nothing.
	switch {
	case orig.Type == room.MessageToolResult && orig.Sender.IsAgent():
		_, agentName, _ := room.SplitActorID(orig.Sender.ID)
		svc.dispatchToAgent(agentName, r.ID, sibling)
	case orig.Sender.IsUser():
		svc.dispatchToAgents(r, sibling)
	}

	resp := &EditMessageResponse{}
	resp.Body = toMessageResponse(sibling)
	return resp, nil
}

func (svc *Service) retryMessage(ctx context.Context, input *RetryMessageRequest) (*RetryMessageResponse, error) {
	r, err := svc.rooms.GetRoom(ctx, input.RoomID)
	if err != nil {
		return nil, huma.Error404NotFound("room not found")
	}
	orig, err := svc.messages.GetMessage(ctx, input.MessageID)
	if err != nil {
		return nil, huma.Error404NotFound("message not found")
	}
	if orig.RoomID != r.ID {
		return nil, huma.Error404NotFound("message not found in this room")
	}
	if !orig.Sender.IsAgent() {
		return nil, huma.Error400BadRequest("only agent messages can be retried")
	}

	// Rewind to the fork point so the regenerated response becomes a new sibling
	// of the original, then re-run the authoring agent. It re-assembles from the
	// rewound head, so no payload reconstruction is needed.
	if err := svc.messages.SetHead(ctx, r.ID, orig.ParentID); err != nil {
		return nil, huma.Error500InternalServerError("failed to set head: " + err.Error())
	}
	_, agentName, _ := room.SplitActorID(orig.Sender.ID)
	svc.dispatchToAgent(agentName, r.ID, orig)

	return &RetryMessageResponse{}, nil
}

package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
)

// ---------------------------------------------------------------------------
// Context preview routes
// ---------------------------------------------------------------------------

func registerContextPreviewRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-context",
		Method:      http.MethodGet,
		Path:        "/api/rooms/{room_id}/agents/{agent_name}/context-preview",
		Summary:     "Preview agent context window",
		Tags:        []string{"Rooms", "Agents"},
	}, func(ctx context.Context, input *GetContextPreviewRequest) (*GetContextPreviewResponse, error) {
		return svc.previewContext(ctx, input)
	})
}

// ---------------------------------------------------------------------------
// Context preview handlers
// ---------------------------------------------------------------------------

func (svc *Service) previewContext(ctx context.Context, input *GetContextPreviewRequest) (*GetContextPreviewResponse, error) {
	r, err := svc.store.GetRoom(ctx, input.RoomID)
	if err != nil {
		return nil, huma.Error404NotFound("room not found")
	}

	ag := svc.agentMgr.Get(input.AgentName)
	if ag == nil {
		return nil, huma.Error404NotFound("agent not found")
	}

	if r.ParticipantByID("agent:"+input.AgentName) == nil {
		return nil, huma.Error400BadRequest("agent is not a participant in this room")
	}

	opts := dispatch.PreviewOptions{
		IncludeCrossRoom:     input.IncludeCrossRoom,
		IncludeInterjections: input.IncludeInterjections,
	}

	assembled, cursor, err := svc.dispatcher.PreviewContext(ctx, ag, input.RoomID, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to preview context: "+err.Error())
	}

	resp := &GetContextPreviewResponse{}
	resp.Body.Messages = make([]ContextMessageResponse, len(assembled.Messages))
	for i, msg := range assembled.Messages {
		resp.Body.Messages[i] = ContextMessageResponse{
			Role:    string(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		}
	}
	resp.Body.CompactionOffset = cursor.Offset
	resp.Body.AssembledBytes = dispatch.AssembledContextSize(assembled)
	return resp, nil
}

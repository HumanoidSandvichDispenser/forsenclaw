package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
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
		return nil, huma.Error500InternalServerError("failed to preview context: " + err.Error())
	}

	payload := assembled.ToContextPayload("")

	// Build a flat message list for display, mirroring how providers render the context.
	var messages []ContextMessageResponse

	// System message: role description + memory + daily notes + RAG + tools
	var sysContent strings.Builder
	sysContent.WriteString(payload.SystemPrompt)
	if payload.Memory != "" {
		sysContent.WriteString("\n\n## Memory\n\n")
		sysContent.WriteString(payload.Memory)
	}
	if len(payload.DailyNotes) > 0 {
		sysContent.WriteString("\n\n## Daily Notes\n\n")
		for i, note := range payload.DailyNotes {
			if i > 0 {
				sysContent.WriteString("\n\n")
			}
			sysContent.WriteString(note)
		}
	}
	if len(payload.RAGResults) > 0 {
		sysContent.WriteString("\n\n## Relevant Context\n\n")
		for i, rag := range payload.RAGResults {
			if i > 0 {
				sysContent.WriteString("\n\n")
			}
			sysContent.WriteString(rag)
		}
	}
	if len(payload.ToolSchemas) > 0 {
		sysContent.WriteString("\n\n## Available Tools\n\n")
		for i, tool := range payload.ToolSchemas {
			if i > 0 {
				sysContent.WriteString("\n\n")
			}
			sysContent.WriteString(tool)
		}
	}
	messages = append(messages, ContextMessageResponse{Role: "system", Content: sysContent.String()})

	// Cross-room feed as a user message.
	if len(payload.CrossRoomFeed) > 0 {
		var sb strings.Builder
		sb.WriteString("## Cross-room activity\n\n")
		for _, line := range payload.CrossRoomFeed {
			sb.WriteString(line)
			sb.WriteString("\n\n")
		}
		messages = append(messages, ContextMessageResponse{Role: "user", Content: sb.String()})
	}

	// History messages.
	for _, h := range payload.History {
		messages = append(messages, ContextMessageResponse{
			Role:    string(h.Role),
			Content: h.Content,
			Name:    h.Name,
		})
	}

	// RFC as final user message.
	if payload.RFC != "" {
		messages = append(messages, ContextMessageResponse{
			Role:    string(inference.RoleUser),
			Content: payload.RFC,
		})
	}

	resp := &GetContextPreviewResponse{}
	resp.Body.Messages = messages
	resp.Body.CompactionOffset = cursor.Offset
	resp.Body.AssembledBytes = dispatch.AssembledContextSize(assembled)
	return resp, nil
}

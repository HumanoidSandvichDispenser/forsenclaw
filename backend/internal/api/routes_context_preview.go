package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
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

// previewContext assembles the context window for an agent in a room exactly
// as it would be built for a real invocation, then renders it through the same
// provider helpers (BuildSystemPrompt/BuildMessageSequence) used at inference
// time so the preview faithfully reflects what the model would receive —
// without running inference.
//
// Fidelity caveats vs. a live turn: tools are shown unfiltered (clearance-based
// tool filtering happens in the inference handler, not the assembler), the
// system prompt is rendered in "native" tool mode (the Anthropic default), and
// there is no current-turn tool-exchange state since none exists pre-inference.
func (svc *Service) previewContext(
	ctx context.Context,
	input *GetContextPreviewRequest,
) (*GetContextPreviewResponse, error) {
	if _, err := svc.rooms.GetRoom(ctx, input.RoomID); err != nil {
		return nil, huma.Error404NotFound("room not found")
	}

	ag := svc.agentMgr.Get(input.AgentName)
	if ag == nil {
		return nil, huma.Error404NotFound("agent not found")
	}

	// Feed the agent's tool definitions into assembly so the preview reflects
	// the same payload the inference path builds. executor is nil in tests.
	var tools []inference.ToolDefinition
	if svc.executor != nil {
		tools = svc.executor.AllDefinitions()
	}

	// Assemble against a room-sourced request with no current-turn tool
	// exchanges — this mirrors the first inference turn for the room.
	req := agent.Request{
		Target: ag.Name(),
		Source: agent.SourceRoom,
		Payload: agent.RequestPayload{
			RoomID: input.RoomID,
		},
	}
	payload, err := svc.assembler.Assemble(ctx, ag, req, tools)
	if err != nil {
		return nil, huma.Error500InternalServerError("assemble context", err)
	}

	offset, err := svc.messages.GetCompactionOffset(ctx, ag.Name(), input.RoomID)
	if err != nil {
		return nil, huma.Error500InternalServerError("get compaction offset", err)
	}

	// Render through the shared provider helpers so headers and ordering match
	// what an adapter would emit. "native" mirrors the Anthropic adapter.
	systemPrompt := inference.BuildSystemPrompt(payload, "native")
	msgSeq := inference.BuildMessageSequence(payload)

	msgs := make([]ContextMessageResponse, 0, len(msgSeq)+1)
	bytes := len(systemPrompt)
	msgs = append(msgs, ContextMessageResponse{
		Role:    string(inference.RoleSystem),
		Content: systemPrompt,
	})
	for _, m := range msgSeq {
		var toolCalls []ContextToolCallResponse
		for _, tc := range m.ToolCalls {
			toolCalls = append(toolCalls, ContextToolCallResponse{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
			bytes += len(tc.Function.Arguments)
		}
		msgs = append(msgs, ContextMessageResponse{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCalls:  toolCalls,
			ToolCallID: m.ToolCallID,
		})
		bytes += len(m.Content)
	}

	resp := &GetContextPreviewResponse{}
	resp.Body.Messages = msgs
	resp.Body.Tools = toContextToolResponses(payload.ToolDefinitions)
	resp.Body.CompactionOffset = int(offset)
	resp.Body.AssembledBytes = bytes
	return resp, nil
}

// toContextToolResponses maps assembled tool definitions to the preview DTO.
func toContextToolResponses(defs []inference.ToolDefinition) []ContextToolResponse {
	out := make([]ContextToolResponse, 0, len(defs))
	for _, d := range defs {
		out = append(out, ContextToolResponse{
			Name:      d.Name,
			Resource:  d.Resource,
			Clearance: d.Clearance,
		})
	}
	return out
}

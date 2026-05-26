package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
)

func registerConfirmationRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "list-confirmations",
		Method:      http.MethodGet,
		Path:        "/api/rooms/{room_id}/confirmations",
		Summary:     "List pending tool-call confirmations for a room",
		Tags:        []string{"Confirmations"},
	}, func(ctx context.Context, input *ListConfirmationsRequest) (*ListConfirmationsResponse, error) {
		return svc.listConfirmations(ctx, input)
	})

	huma.Register(api, huma.Operation{
		OperationID: "respond-confirmation",
		Method:      http.MethodPost,
		Path:        "/api/rooms/{room_id}/confirmations/{node_id}",
		Summary:     "Respond to a pending tool-call confirmation",
		Tags:        []string{"Confirmations"},
	}, func(ctx context.Context, input *RespondConfirmationRequest) (*RespondConfirmationResponse, error) {
		return svc.respondConfirmation(ctx, input)
	})
}

func (svc *Service) listConfirmations(_ context.Context, input *ListConfirmationsRequest) (*ListConfirmationsResponse, error) {
	pending := svc.agentMgr.ConfirmationRegistry().List(input.RoomID)

	resp := &ListConfirmationsResponse{}
	resp.Body.Confirmations = make([]ConfirmationResponse, len(pending))
	for i, pc := range pending {
		resp.Body.Confirmations[i] = toConfirmationResponse(pc)
	}
	return resp, nil
}

func (svc *Service) respondConfirmation(_ context.Context, input *RespondConfirmationRequest) (*RespondConfirmationResponse, error) {
	if input.Body.Action == "allow" && input.Body.Args != "" {
		if !json.Valid([]byte(input.Body.Args)) {
			return nil, huma.Error400BadRequest("args must be valid JSON")
		}
	}

	// Take atomically removes the confirmation — a second concurrent request gets 404.
	pc := svc.agentMgr.ConfirmationRegistry().Take(input.RoomID, input.NodeID)
	if pc == nil {
		return nil, huma.Error404NotFound("confirmation not found")
	}

	var result dag.Result
	switch input.Body.Action {
	case "allow":
		result = dag.Result{Status: dag.StatusAllowed, EditedArgs: input.Body.Args}
	case "deny":
		result = dag.Result{Status: dag.StatusDenied}
	case "revise":
		result = dag.Result{Status: dag.StatusRevise, Content: input.Body.Feedback}
	default:
		return nil, huma.Error400BadRequest("action must be one of: allow, deny, revise")
	}

	rt := svc.agentMgr.Runtime(pc.AgentName)
	if rt == nil {
		return nil, huma.Error404NotFound("agent runtime not found")
	}
	if !rt.Respond(input.NodeID, result) {
		return nil, huma.Error404NotFound("confirmation node not found in runtime")
	}

	return &RespondConfirmationResponse{}, nil
}

func toConfirmationResponse(pc agent.PendingConfirmation) ConfirmationResponse {
	return ConfirmationResponse{
		NodeID:    pc.NodeID,
		AgentName: pc.AgentName,
		RoomID:    pc.RoomID,
		ToolName:  pc.ToolName,
		Args:      pc.Args,
	}
}

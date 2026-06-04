package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

func registerDAGRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "get-agent-dag",
		Method:      http.MethodGet,
		Path:        "/api/agents/{agent_name}/dag",
		Summary:     "Get an agent's current request DAG",
		Tags:        []string{"Agents"},
	}, func(ctx context.Context, input *GetAgentDAGRequest) (*GetAgentDAGResponse, error) {
		return svc.getAgentDAG(ctx, input)
	})
}

// getAgentDAG returns a snapshot of the agent's live request DAG. Settled nodes
// persist until the agent's next request (the retention model the runtime
// enforces on Enqueue), so an idle agent still shows the DAG it last ran.
//
// The DAG is the agent's internal state, classified at the agent's configured
// clearance — a provable upper bound on every node's origin (each node's
// clearance is min(agent, room) ≤ agent.Clearance). A viewer below that clearance
// must not learn even the shape or timing of the agent's work, so the whole
// snapshot is denied rather than filtered.
func (svc *Service) getAgentDAG(_ context.Context, input *GetAgentDAGRequest) (*GetAgentDAGResponse, error) {
	ag := svc.agentMgr.Get(input.AgentName)
	rt := svc.agentMgr.Runtime(input.AgentName)
	if ag == nil || rt == nil {
		return nil, huma.Error404NotFound("agent has no runtime")
	}

	if !clearedToReadDAG(svc.user.Clearance, ag.Clearance()) {
		return nil, huma.Error403Forbidden("insufficient clearance to view this agent's DAG")
	}

	resp := &GetAgentDAGResponse{}
	resp.Body.Nodes = rt.Snapshot()
	return resp, nil
}

// clearedToReadDAG applies the Bell-LaPadula read rule (no read-up) for the DAG:
// a viewer may read it only at or above the agent's configured clearance. Shared
// by the REST snapshot and the WebSocket stream so both gates agree.
func clearedToReadDAG(viewerClearance, agentClearance int) bool {
	return viewerClearance >= agentClearance
}

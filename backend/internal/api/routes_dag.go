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
func (svc *Service) getAgentDAG(_ context.Context, input *GetAgentDAGRequest) (*GetAgentDAGResponse, error) {
	rt := svc.agentMgr.Runtime(input.AgentName)
	if rt == nil {
		return nil, huma.Error404NotFound("agent has no runtime")
	}

	resp := &GetAgentDAGResponse{}
	resp.Body.Nodes = rt.Snapshot()
	return resp, nil
}

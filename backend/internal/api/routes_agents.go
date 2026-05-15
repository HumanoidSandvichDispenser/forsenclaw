package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
)

// ---------------------------------------------------------------------------
// Agent routes
// ---------------------------------------------------------------------------

func registerAgentRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "list-agents",
		Method:      http.MethodGet,
		Path:        "/api/agents",
		Summary:     "List loaded agents",
		Tags:        []string{"Agents"},
	}, func(ctx context.Context, input *struct{}) (*ListAgentsResponse, error) {
		return svc.listAgents(ctx)
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-agent",
		Method:      http.MethodGet,
		Path:        "/api/agents/{name}",
		Summary:     "Get agent details",
		Tags:        []string{"Agents"},
	}, func(ctx context.Context, input *GetAgentRequest) (*GetAgentResponse, error) {
		return svc.getAgent(ctx, input)
	})
}

// ---------------------------------------------------------------------------
// Agent handlers
// ---------------------------------------------------------------------------

func (svc *Service) listAgents(ctx context.Context) (*ListAgentsResponse, error) {
	agents := svc.agentMgr.All()

	resp := &ListAgentsResponse{}
	resp.Body.Agents = make([]AgentResponse, 0, len(agents))
	for _, ag := range agents {
		resp.Body.Agents = append(resp.Body.Agents, toAgentResponse(ag))
	}
	return resp, nil
}

func (svc *Service) getAgent(ctx context.Context, input *GetAgentRequest) (*GetAgentResponse, error) {
	ag := svc.agentMgr.Get(input.Name)
	if ag == nil {
		return nil, huma.Error404NotFound("agent not found")
	}

	resp := &GetAgentResponse{}
	resp.Body = toAgentResponse(ag)
	return resp, nil
}

// toAgentResponse converts an internal agent.Agent to the API response type.
func toAgentResponse(ag *agent.Agent) AgentResponse {
	def := ag.Definition
	return AgentResponse{
		Name:            def.Name,
		RoleDescription: def.RoleDescription,
		PrimaryModel:    def.Models.Primary,
		RoutineModel:    def.Models.Routine,
		SensitiveModel:  def.Models.Sensitive,
		Clearance:       def.Clearance,
		Active:          ag.IsActive(),
	}
}

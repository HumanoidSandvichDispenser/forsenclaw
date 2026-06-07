package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// GetAgentMemoryRequest selects the agent whose memory to inspect.
type GetAgentMemoryRequest struct {
	AgentName string `path:"agent_name" validate:"required" doc:"Agent name"`
}

// AgentDailyNoteResponse is one day's notes at a clearance level.
type AgentDailyNoteResponse struct {
	Date    string `json:"date"`
	Content string `json:"content"`
}

// AgentMemoryLevelResponse is one clearance band of the agent's memory.
// Clearance 0 is the legacy flat (unleveled) baseline.
type AgentMemoryLevelResponse struct {
	Clearance int                      `json:"clearance"`
	Memory    string                   `json:"memory"`
	Notes     []AgentDailyNoteResponse `json:"notes"`
}

// GetAgentMemoryResponse is the agent's memory broken out by clearance level,
// scoped to what the viewer is cleared to read.
type GetAgentMemoryResponse struct {
	Body struct {
		AgentName string                     `json:"agent_name"`
		Levels    []AgentMemoryLevelResponse `json:"levels"`
	}
}

func registerAgentMemoryRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "get-agent-memory",
		Method:      http.MethodGet,
		Path:        "/api/agents/{agent_name}/memory",
		Summary:     "Inspect an agent's memory and daily notes by clearance",
		Tags:        []string{"Agents"},
	}, func(ctx context.Context, input *GetAgentMemoryRequest) (*GetAgentMemoryResponse, error) {
		return svc.getAgentMemory(ctx, input)
	})
}

// getAgentMemory returns the agent's per-clearance memory and recent daily
// notes, scoped by Bell-LaPadula read: the viewer sees only levels at or below
// its own clearance, capped at the agent's configured clearance. The legacy flat
// baseline may hold content up to the configured clearance, so it is shown only
// to a viewer cleared to that ceiling.
func (svc *Service) getAgentMemory(_ context.Context, input *GetAgentMemoryRequest) (*GetAgentMemoryResponse, error) {
	if svc.memory == nil {
		return nil, huma.Error501NotImplemented("memory inspection is not available")
	}
	ag := svc.agentMgr.Get(input.AgentName)
	if ag == nil {
		return nil, huma.Error404NotFound("agent not found")
	}

	maxClearance := ag.Clearance()
	if svc.user.Clearance < maxClearance {
		maxClearance = svc.user.Clearance
	}
	includeFlat := svc.user.Clearance >= ag.Clearance()

	levels, err := svc.memory.Inspect(ag.Name(), maxClearance, includeFlat)
	if err != nil {
		return nil, huma.Error500InternalServerError("read agent memory", err)
	}

	resp := &GetAgentMemoryResponse{}
	resp.Body.AgentName = ag.Name()
	resp.Body.Levels = make([]AgentMemoryLevelResponse, 0, len(levels))
	for _, lvl := range levels {
		notes := make([]AgentDailyNoteResponse, 0, len(lvl.Notes))
		for _, n := range lvl.Notes {
			notes = append(notes, AgentDailyNoteResponse{
				Date:    n.Date.Format("2006-01-02"),
				Content: n.Content,
			})
		}
		resp.Body.Levels = append(resp.Body.Levels, AgentMemoryLevelResponse{
			Clearance: lvl.Clearance,
			Memory:    lvl.Memory,
			Notes:     notes,
		})
	}
	return resp, nil
}

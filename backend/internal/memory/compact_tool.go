package memory

import (
	"context"
	"fmt"
	"strconv"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/tool"
)

// AgentResolver looks up a live agent by name. Tools that need an agent's
// definition (model tiers, feature flags) — which is not principal data and so
// is absent from tool.Invocation — use it to recover the agent.
type AgentResolver func(name string) *agent.Agent

// compactTool lets an agent compact its own transcript in the current room down
// to a target size on demand, independent of the automatic post-turn trigger.
type compactTool struct {
	compactor *Compactor
	resolve   AgentResolver
}

// NewCompactTool builds the native "compact" tool. SetResolver must be called
// before use, once the agent manager exists (mirrors the create_room wiring).
func NewCompactTool(c *Compactor) *compactTool {
	return &compactTool{compactor: c}
}

// SetResolver wires the agent lookup after manager construction.
func (t *compactTool) SetResolver(r AgentResolver) { t.resolve = r }

func (t *compactTool) Definition() inference.ToolDefinition {
	return inference.ToolDefinition{
		Name: "compact",
		Description: "Compact your conversation history in this room, summarizing the oldest " +
			"messages into your daily notes and dropping them from the live window. Optionally " +
			"give a target size in bytes; omit it to use the configured default.",
		Resource:    "frsn:agent/compact",
		DataActions: []string{"agent:compact"},
		SelfLeveled: true,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": map[string]interface{}{
					"type":        "string",
					"description": "Optional target size in bytes, as an integer (e.g. \"4000\").",
				},
			},
		},
	}
}

func (t *compactTool) Invoke(ctx context.Context, inv tool.Invocation, params map[string]string) (string, error) {
	if inv.RoomID == 0 {
		return "", fmt.Errorf("compact requires a room context")
	}
	if t.resolve == nil {
		return "", fmt.Errorf("compact tool is not wired with an agent resolver")
	}
	ag := t.resolve(inv.AgentName)
	if ag == nil {
		return "", fmt.Errorf("agent %q not found", inv.AgentName)
	}

	target := 0
	if s := params["target"]; s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			return "", fmt.Errorf("invalid target %q: %w", s, err)
		}
		target = v
	}

	if err := t.compactor.Compact(ctx, ag, inv.RoomID, target); err != nil {
		return "", err
	}
	return "Compacted.", nil
}

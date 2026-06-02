package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/audit"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// Executor executes tool calls against the MCP registry and logs each
// invocation via the audit logger. Permission gating and confirmation are
// handled upstream by the caller (InferenceHandler / ConfirmationHandler).
type Executor struct {
	registry Registry
	audit    *audit.Logger
}

// NewExecutor creates an Executor backed by the given registry and audit logger.
func NewExecutor(registry Registry, auditLogger *audit.Logger) *Executor {
	return &Executor{registry: registry, audit: auditLogger}
}

// AllDefinitions returns all tool definitions from the registry.
// Satisfies the agent.ToolExecutor interface.
func (e *Executor) AllDefinitions() []inference.ToolDefinition {
	return e.registry.AllDefinitions()
}

// DynamicClearance is implemented by tools whose resource clearance depends on
// their call arguments rather than a static per-tool value (e.g. create_room,
// whose target ceiling comes from an argument). ok is false when the args carry
// no clearance, in which case the static tool clearance applies.
type DynamicClearance interface {
	ResourceClearance(params map[string]string) (int, bool)
}

// ResolveResourceClearance derives a per-call resource clearance from the call
// arguments when the resolved tool implements DynamicClearance. Satisfies the
// agent.ResourceClearanceResolver interface.
func (e *Executor) ResolveResourceClearance(call inference.ToolCallWire) (int, bool) {
	client, err := e.registry.Resolve(call.Function.Name)
	if err != nil {
		return 0, false
	}
	dc, ok := client.(DynamicClearance)
	if !ok {
		return 0, false
	}
	var params map[string]string
	if call.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &params); err != nil {
			return 0, false
		}
	}
	return dc.ResourceClearance(params)
}

// Execute resolves the tool, converts JSON arguments, calls the MCP client,
// and logs the outcome. Satisfies the agent.ToolExecutor interface.
func (e *Executor) Execute(ctx context.Context, call inference.ToolCallWire) (string, error) {
	client, err := e.registry.Resolve(call.Function.Name)
	if err != nil {
		return "", fmt.Errorf("resolving tool %q: %w", call.Function.Name, err)
	}

	var params map[string]string
	if call.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &params); err != nil {
			return "", fmt.Errorf("parsing arguments for tool %q: %w", call.Function.Name, err)
		}
	}

	result, err := client.Call(ctx, call.Function.Name, params)

	agentID := audit.AgentIDFromContext(ctx)
	if err != nil {
		e.audit.Log(audit.Event{
			Level:   audit.LevelWarn,
			Kind:    audit.KindToolFailed,
			AgentID: agentID,
			Fields: map[string]any{
				"tool_id": call.Function.Name,
				"call_id": call.ID,
				"args":    params,
				"error":   err.Error(),
			},
		})
		return "", fmt.Errorf("calling tool %q: %w", call.Function.Name, err)
	}

	e.audit.Log(audit.Event{
		Level:   audit.LevelInfo,
		Kind:    audit.KindToolInvoked,
		AgentID: agentID,
		Fields: map[string]any{
			"tool_id": call.Function.Name,
			"call_id": call.ID,
			"args":    params,
		},
	})

	return result, nil
}

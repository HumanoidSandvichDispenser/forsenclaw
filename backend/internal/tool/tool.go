// Package tool defines the unified tool-execution boundary: a single Tool
// abstraction that both MCP-backed and in-process ("native") tools implement,
// and an Executor that dispatches calls to them.
//
// The principal for a call travels as an explicit, typed Invocation value
// rather than through context values, so authorization-relevant data
// (operating clearance, room) is visible in signatures and serializable across
// a future host/worker boundary.
package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/audit"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// Invocation is the principal and authority for a single tool call: who is
// calling, at what operating clearance, and in which room. It carries data, not
// the agent object, so it is decoupled from the agent type and can cross an IPC
// boundary unchanged.
type Invocation struct {
	// AgentName is the calling agent.
	AgentName string
	// ConfiguredClearance is the agent's static ceiling.
	ConfiguredClearance int
	// OperatingClearance is the clearance the agent is acting at for this call
	// (min(configured, room)). Native tools that write data write at this level.
	OperatingClearance int
	// RoomID is the room the call originates from, or 0 if none.
	RoomID int64
}

// Tool is a single invocable capability. MCP-backed tools and native in-process
// tools (note, compact, …) both satisfy it, so the Executor needs no
// native-vs-MCP branch.
type Tool interface {
	// Definition is the schema injected into the model's context and used for
	// clearance/permission gating.
	Definition() inference.ToolDefinition
	// Invoke runs the tool on behalf of inv with the decoded call parameters.
	Invoke(ctx context.Context, inv Invocation, params map[string]string) (string, error)
}

// DynamicClearance is optionally implemented by a Tool whose resource clearance
// depends on its call arguments rather than its static definition (e.g.
// create_room, whose ceiling comes from an argument). ok is false when the args
// carry no clearance, in which case the static value applies.
type DynamicClearance interface {
	ResourceClearance(params map[string]string) (int, bool)
}

// Executor dispatches tool calls to registered Tools and audits each one.
// Permission gating and confirmation are handled upstream by the caller; the
// Executor only resolves, decodes, invokes, and logs.
type Executor struct {
	tools map[string]Tool
	audit *audit.Logger
}

// NewExecutor builds an Executor over the given tools, keyed by definition name.
func NewExecutor(auditLogger *audit.Logger, tools ...Tool) *Executor {
	m := make(map[string]Tool, len(tools))
	for _, t := range tools {
		m[t.Definition().Name] = t
	}
	return &Executor{tools: m, audit: auditLogger}
}

// AllDefinitions returns the definitions of every registered tool.
func (e *Executor) AllDefinitions() []inference.ToolDefinition {
	defs := make([]inference.ToolDefinition, 0, len(e.tools))
	for _, t := range e.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

// ResolveResourceClearance derives a per-call resource clearance when the
// resolved tool implements DynamicClearance.
func (e *Executor) ResolveResourceClearance(call inference.ToolCallWire) (int, bool) {
	t, ok := e.tools[call.Function.Name]
	if !ok {
		return 0, false
	}
	dc, ok := t.(DynamicClearance)
	if !ok {
		return 0, false
	}
	params, err := decodeParams(call)
	if err != nil {
		return 0, false
	}
	return dc.ResourceClearance(params)
}

// Execute resolves the tool named by the call, decodes its arguments, invokes
// it on behalf of inv, and audits the outcome.
func (e *Executor) Execute(ctx context.Context, inv Invocation, call inference.ToolCallWire) (string, error) {
	t, ok := e.tools[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("resolving tool %q: not registered", call.Function.Name)
	}

	params, err := decodeParams(call)
	if err != nil {
		return "", fmt.Errorf("parsing arguments for tool %q: %w", call.Function.Name, err)
	}

	result, err := t.Invoke(ctx, inv, params)
	if err != nil {
		e.log(audit.LevelWarn, audit.KindToolFailed, inv.AgentName, map[string]any{
			"tool_id": call.Function.Name,
			"call_id": call.ID,
			"args":    params,
			"error":   err.Error(),
		})
		return "", fmt.Errorf("calling tool %q: %w", call.Function.Name, err)
	}

	e.log(audit.LevelInfo, audit.KindToolInvoked, inv.AgentName, map[string]any{
		"tool_id": call.Function.Name,
		"call_id": call.ID,
		"args":    params,
	})
	return result, nil
}

func (e *Executor) log(level audit.Level, kind audit.Kind, agentID string, fields map[string]any) {
	if e.audit == nil {
		return
	}
	e.audit.Log(audit.Event{Level: level, Kind: kind, AgentID: agentID, Fields: fields})
}

// decodeParams unmarshals a call's JSON arguments into a flat string map.
func decodeParams(call inference.ToolCallWire) (map[string]string, error) {
	if call.Function.Arguments == "" {
		return map[string]string{}, nil
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(call.Function.Arguments), &params); err != nil {
		return nil, err
	}
	return params, nil
}

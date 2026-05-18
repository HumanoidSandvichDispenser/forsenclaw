package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// runToolLoop drives the agentic inference loop for a single RFC.
// It calls the model, parses tool calls, executes them via the executor,
// injects results back as history, and repeats until the model produces
// a response with no tool calls, the iteration cap is hit, or the context
// is cancelled.
//
// Returns the final prose response, cumulative usage, and any error.
func (d *Dispatcher) runToolLoop(
	ctx context.Context,
	ag *agent.Agent,
	rfc room.RFC,
	provider inference.Provider,
	modelID string,
	assembled *memory.AssembledContext,
	executor *mcp.Executor,
) (finalProse string, usage inference.Usage, err error) {
	iterationCap := executor.Cfg.MaxIterations

	// Copy history so we don't mutate assembled.History.
	history := make([]inference.HistoryMessage, len(assembled.History))
	copy(history, assembled.History)

	// Broadcast typing indicator once at the start.
	if d.hub != nil {
		d.hub.Broadcast(rfc.RoomID, StreamEvent{Type: "typing", RoomID: rfc.RoomID})
	}

	var prose string

	for iteration := 0; iteration < iterationCap; iteration++ {
		payload := assembled.ToContextPayload(modelID)
		payload.History = history

		rawResponse, iterUsage, streamErr := d.streamPayload(ctx, rfc, provider, payload)
		if streamErr != nil {
			return "", inference.Usage{}, streamErr
		}

		usage = mergeUsage(usage, iterUsage)

		calls, iterProse, parseErr := mcp.ParseToolCalls(rawResponse)
		if parseErr != nil {
			log.Printf("dispatcher: warning: failed to parse tool calls in iteration %d for RFC %s: %v", iteration, rfc.ID, parseErr)
		}
		prose = iterProse

		if len(calls) == 0 {
			// No tool calls — this is the final response.
			return prose, usage, nil
		}

		// Execute all calls in this iteration (serial for v1).
		assistantCall := inference.HistoryMessage{Role: inference.RoleAssistant}
		assistantCall.ToolCalls = make([]inference.ToolCallWire, 0, len(calls))
		toolResults := make([]inference.HistoryMessage, 0, len(calls))
		for _, call := range calls {
			args, err := json.Marshal(call.Parameters)
			if err != nil {
				args = []byte("{}")
			}
			assistantCall.ToolCalls = append(assistantCall.ToolCalls, inference.ToolCallWire{
				ID:   call.ID,
				Type: "function",
				Function: inference.ToolFunctionWire{
					Name:      call.ToolID,
					Arguments: string(args),
				},
			})

			result := executor.Execute(ctx, ag.Name(), call)

			toolResults = append(toolResults, inference.HistoryMessage{
				Role:       inference.RoleTool,
				Name:       call.ToolID,
				ToolCallID: call.ID,
				Content:    toolResultContent(result),
			})
		}
		history = append(history, assistantCall)
		history = append(history, toolResults...)
	}

	// Hard iteration cap hit.
	log.Printf("dispatcher: tool loop hit iteration cap (%d) for RFC %s", iterationCap, rfc.ID)
	return prose, usage, nil
}

// buildExecutor constructs a permission-gated MCP executor for the given agent.
func (d *Dispatcher) buildExecutor(ag *agent.Agent) *mcp.Executor {
	perms, _ := ag.Definition.ParsedPermissions()
	maxIter := d.maxToolIterations
	if maxIter <= 0 {
		maxIter = 10
	}
	return mcp.NewExecutor(d.mcpRegistry, perms, d.confirmations, d.auditLogger, mcp.ExecutorConfig{
		MaxIterations: maxIter,
	})
}

// mergeUsage accumulates token counts across loop iterations.
func mergeUsage(acc, iter inference.Usage) inference.Usage {
	return inference.Usage{
		PromptTokens:     acc.PromptTokens + iter.PromptTokens,
		CompletionTokens: acc.CompletionTokens + iter.CompletionTokens,
		TotalTokens:      acc.TotalTokens + iter.TotalTokens,
	}
}

// toolResultContent returns the text content stored for a tool-role history message.
func toolResultContent(r mcp.ToolResult) string {
	if r.IsError {
		return fmt.Sprintf("error: %s", r.Error)
	}
	return r.Content
}

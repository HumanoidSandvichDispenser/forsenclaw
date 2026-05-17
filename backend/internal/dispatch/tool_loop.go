package dispatch

import (
	"context"
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
		for _, call := range calls {
			// Append assistant's tool-call turn.
			history = append(history, inference.HistoryMessage{
				Role:    inference.RoleAssistant,
				Content: call.RawXML,
			})

			result := executor.Execute(ctx, ag.Name(), call)

			// Append tool result turn.
			history = append(history, inference.HistoryMessage{
				Role:    inference.RoleTool,
				Name:    call.ToolID,
				Content: formatToolResult(result),
			})
		}
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

// formatToolResult serialises a ToolResult into the XML wire format injected
// into the conversation history.
func formatToolResult(r mcp.ToolResult) string {
	if r.IsError {
		return fmt.Sprintf(`<tool_result id=%q error="true"><error><![CDATA[%s]]></error></tool_result>`,
			r.CallID, r.Error)
	}
	return fmt.Sprintf(`<tool_result id=%q><content><![CDATA[%s]]></content></tool_result>`,
		r.CallID, r.Content)
}

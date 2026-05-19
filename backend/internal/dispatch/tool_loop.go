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

	// turnHistory starts from the assembled room transcript and grows with each
	// tool round-trip during this RFC.
	turnHistory := make([]inference.HistoryMessage, len(assembled.History))
	copy(turnHistory, assembled.History)

	// Broadcast typing indicator once at the start.
	if d.hub != nil {
		d.hub.Broadcast(rfc.RoomID, StreamEvent{Type: "typing", RoomID: rfc.RoomID})
	}

	var prose string

	for iteration := 0; iteration < iterationCap; iteration++ {
		payload := assembled.ToContextPayload(modelID)
		payload.History = turnHistory

		rawResponse, finalChunk, streamErr := d.broadcastAndCollect(ctx, rfc, provider, payload)
		if streamErr != nil {
			return "", inference.Usage{}, streamErr
		}

		usage = mergeUsage(usage, finalChunk.Usage)

		if len(finalChunk.ToolCalls) > 0 {
			// Native branch: the adapter already surfaced structured tool calls.
			assistantMsg, resultMsgs := buildNativeToolTurn(ctx, rawResponse, finalChunk.ToolCalls, executor, ag.Name())
			turnHistory = append(turnHistory, assistantMsg)
			turnHistory = append(turnHistory, resultMsgs...)
			continue
		}

		// XML branch: parse tool calls from the response text.
		calls, iterProse, parseErr := mcp.ParseToolCalls(rawResponse)
		if parseErr != nil {
			log.Printf("dispatcher: warning: failed to parse tool calls in iteration %d for RFC %s: %v", iteration, rfc.ID, parseErr)
		}
		prose = iterProse

		if len(calls) == 0 {
			// No tool calls — this is the final response.
			return prose, usage, nil
		}

		assistantMsg, resultMsgs := buildXMLToolTurn(ctx, rawResponse, calls, executor, ag.Name())
		turnHistory = append(turnHistory, assistantMsg)
		turnHistory = append(turnHistory, resultMsgs...)
	}

	// Hard iteration cap hit.
	log.Printf("dispatcher: tool loop hit iteration cap (%d) for RFC %s", iterationCap, rfc.ID)
	return prose, usage, nil
}

// buildNativeToolTurn executes a set of native tool calls and returns the
// assistant message and result messages to append to turnHistory.
func buildNativeToolTurn(
	ctx context.Context,
	prose string,
	toolCalls []inference.ToolCallWire,
	executor *mcp.Executor,
	agentName string,
) (assistantMsg inference.HistoryMessage, resultMsgs []inference.HistoryMessage) {
	assistantMsg = inference.HistoryMessage{
		Role:      inference.RoleAssistant,
		Content:   prose,
		ToolCalls: toolCalls,
	}

	for _, call := range toolCalls {
		mcpCall := mcp.ToolCall{
			ID:         call.ID,
			ToolID:     call.Function.Name,
			Parameters: make(map[string]string),
		}
		_ = json.Unmarshal([]byte(call.Function.Arguments), &mcpCall.Parameters)

		result := executor.Execute(ctx, agentName, mcpCall)

		resultMsgs = append(resultMsgs, inference.HistoryMessage{
			Role:       inference.RoleTool,
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    toolResultContent(result),
		})
	}

	return assistantMsg, resultMsgs
}

// buildXMLToolTurn executes a set of XML-parsed tool calls and returns the
// assistant message (containing the raw response so the model sees its own
// call) and result messages to append to turnHistory.
func buildXMLToolTurn(
	ctx context.Context,
	rawResponse string,
	calls []mcp.ToolCall,
	executor *mcp.Executor,
	agentName string,
) (assistantMsg inference.HistoryMessage, resultMsgs []inference.HistoryMessage) {
	assistantMsg = inference.HistoryMessage{
		Role:    inference.RoleAssistant,
		Content: rawResponse,
	}

	for _, call := range calls {
		result := executor.Execute(ctx, agentName, call)

		resultMsgs = append(resultMsgs, inference.HistoryMessage{
			Role:    inference.RoleUser,
			Name:    call.ToolID,
			Content: fmt.Sprintf("<tool_response tool_id=\"%s\">%s</tool_response>", call.ToolID, toolResultContent(result)),
		})
	}

	return assistantMsg, resultMsgs
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

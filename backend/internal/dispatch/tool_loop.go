package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// toolCallLogEntry records a single executed tool call for context and display.
type toolCallLogEntry struct {
	name   string
	args   map[string]interface{} // decoded parameter map for JSON embedding
	result string                 // truncated result for display
}

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
	var toolCallLog []toolCallLogEntry // per-call records for context and display

	agentActor := room.Actor{
		ID:       "agent:" + ag.Name(),
		Type:     room.ActorAgent,
		Name:     ag.Definition.Name,
		Clearance: ag.Definition.Clearance,
	}

	for iteration := 0; iteration < iterationCap; iteration++ {
		payload := assembled.ToContextPayload(modelID)
		payload.History = turnHistory

		log.Printf("dispatcher: tool loop iteration %d for RFC %s", iteration, rfc.ID)

		rawResponse, finalChunk, streamErr := d.broadcastAndCollect(ctx, rfc, provider, payload)
		if streamErr != nil {
			return "", inference.Usage{}, streamErr
		}

		usage = mergeUsage(usage, finalChunk.Usage)
		log.Printf("dispatcher: model response: finish_reason=%q content_len=%d native_tool_calls=%d",
			finalChunk.FinishReason, len(rawResponse), len(finalChunk.ToolCalls))

		if len(finalChunk.ToolCalls) > 0 {
			// Native branch: the adapter already surfaced structured tool calls.
			names := make([]string, 0, len(finalChunk.ToolCalls))
			for _, tc := range finalChunk.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			log.Printf("dispatcher: native tool calls in iteration %d for RFC %s: %s", iteration, rfc.ID, strings.Join(names, ", "))
			if d.hub != nil {
				d.hub.Broadcast(rfc.RoomID, StreamEvent{
					Type:    "tool_call",
					RoomID:  rfc.RoomID,
					Content: strings.Join(names, ", "),
				})
			}
			assistantMsg, resultMsgs, results := buildNativeToolTurn(ctx, rawResponse, finalChunk.ToolCalls, executor, ag.Name())

			// Persist tool call turn to transcript.
			toolCallRecords := make([]room.ToolCallRecord, len(finalChunk.ToolCalls))
			for i, tc := range finalChunk.ToolCalls {
				toolCallRecords[i] = room.ToolCallRecord{
					ID:        tc.ID,
					ToolName:  tc.Function.Name,
					Arguments: tc.Function.Arguments,
				}
			}
			toolCallTranscriptMsg := room.Message{
				ID:           uuid.New().String(),
				Timestamp:    time.Now().UTC(),
				RoomID:       rfc.RoomID,
				Sender:       agentActor,
				ClearanceTag: ag.Definition.Clearance,
				Type:         room.MessageToolCall,
				Content:      rawResponse,
				ToolCalls:    toolCallRecords,
			}
			if err := d.appendToTranscript(ctx, rfc.RoomID, toolCallTranscriptMsg); err != nil {
				log.Printf("dispatcher: warning: failed to persist tool call message: %v", err)
			}

			for i, tc := range finalChunk.ToolCalls {
				result := results[i]
				resultContent := toolResultContent(result)
				resultTranscriptMsg := room.Message{
					ID:           uuid.New().String(),
					Timestamp:    time.Now().UTC(),
					RoomID:       rfc.RoomID,
					Sender:       agentActor,
					ClearanceTag: ag.Definition.Clearance,
					Type:         room.MessageToolResult,
					Content:      resultContent,
					ToolCallID:   tc.ID,
					ToolName:     tc.Function.Name,
				}
				if err := d.appendToTranscript(ctx, rfc.RoomID, resultTranscriptMsg); err != nil {
					log.Printf("dispatcher: warning: failed to persist tool result message: %v", err)
				}

				var args map[string]interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				toolCallLog = append(toolCallLog, toolCallLogEntry{
					name:   tc.Function.Name,
					args:   args,
					result: truncateResult(resultContent),
				})
			}

			if d.hub != nil {
				d.hub.Broadcast(rfc.RoomID, StreamEvent{
					Type:   "tool_result",
					RoomID: rfc.RoomID,
				})
			}
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
			log.Printf("dispatcher: no tool calls in iteration %d for RFC %s, returning prose (%d bytes)", iteration, rfc.ID, len(prose))
			return prependToolUse(toolCallLog, prose), usage, nil
		}

		names := make([]string, 0, len(calls))
		for _, tc := range calls {
			names = append(names, tc.ToolID)
		}
		log.Printf("dispatcher: XML tool calls in iteration %d for RFC %s: %s", iteration, rfc.ID, strings.Join(names, ", "))
		if d.hub != nil {
			d.hub.Broadcast(rfc.RoomID, StreamEvent{
				Type:    "tool_call",
				RoomID:  rfc.RoomID,
				Content: strings.Join(names, ", "),
			})
		}
		assistantMsg, resultMsgs, results := buildXMLToolTurn(ctx, rawResponse, calls, executor, ag.Name())

		// Persist tool call turn to transcript.
		toolCallTranscriptMsg := room.Message{
			ID:           uuid.New().String(),
			Timestamp:    time.Now().UTC(),
			RoomID:       rfc.RoomID,
			Sender:       agentActor,
			ClearanceTag: ag.Definition.Clearance,
			Type:         room.MessageToolCall,
			Content:      rawResponse, // includes XML tool call blocks
		}
		if err := d.appendToTranscript(ctx, rfc.RoomID, toolCallTranscriptMsg); err != nil {
			log.Printf("dispatcher: warning: failed to persist tool call message: %v", err)
		}

		for i, call := range calls {
			result := results[i]
			resultContent := toolResultContent(result)
			resultXML := fmt.Sprintf("<tool_response tool_id=%q>%s</tool_response>", call.ToolID, resultContent)
			resultTranscriptMsg := room.Message{
				ID:           uuid.New().String(),
				Timestamp:    time.Now().UTC(),
				RoomID:       rfc.RoomID,
				Sender:       agentActor,
				ClearanceTag: ag.Definition.Clearance,
				Type:         room.MessageToolResult,
				Content:      resultXML,
				ToolName:     call.ToolID,
			}
			if err := d.appendToTranscript(ctx, rfc.RoomID, resultTranscriptMsg); err != nil {
				log.Printf("dispatcher: warning: failed to persist tool result message: %v", err)
			}

			args := make(map[string]interface{}, len(call.Parameters))
			for k, v := range call.Parameters {
				args[k] = v
			}
			toolCallLog = append(toolCallLog, toolCallLogEntry{
				name:   call.ToolID,
				args:   args,
				result: truncateResult(resultContent),
			})
		}

		if d.hub != nil {
			d.hub.Broadcast(rfc.RoomID, StreamEvent{
				Type:   "tool_result",
				RoomID: rfc.RoomID,
			})
		}
		turnHistory = append(turnHistory, assistantMsg)
		turnHistory = append(turnHistory, resultMsgs...)
	}

	// Hard iteration cap hit.
	log.Printf("dispatcher: tool loop hit iteration cap (%d) for RFC %s", iterationCap, rfc.ID)
	return prependToolUse(toolCallLog, prose), usage, nil
}

// buildNativeToolTurn executes a set of native tool calls and returns the
// assistant message, result messages to append to turnHistory, and the raw results.
func buildNativeToolTurn(
	ctx context.Context,
	prose string,
	toolCalls []inference.ToolCallWire,
	executor *mcp.Executor,
	agentName string,
) (assistantMsg inference.HistoryMessage, resultMsgs []inference.HistoryMessage, results []mcp.ToolResult) {
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
		results = append(results, result)

		resultMsgs = append(resultMsgs, inference.HistoryMessage{
			Role:       inference.RoleTool,
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    toolResultContent(result),
		})
	}

	return assistantMsg, resultMsgs, results
}

// buildXMLToolTurn executes a set of XML-parsed tool calls and returns the
// assistant message (containing the raw response so the model sees its own
// call), result messages to append to turnHistory, and the raw results.
func buildXMLToolTurn(
	ctx context.Context,
	rawResponse string,
	calls []mcp.ToolCall,
	executor *mcp.Executor,
	agentName string,
) (assistantMsg inference.HistoryMessage, resultMsgs []inference.HistoryMessage, results []mcp.ToolResult) {
	assistantMsg = inference.HistoryMessage{
		Role:    inference.RoleAssistant,
		Content: rawResponse,
	}

	for _, call := range calls {
		result := executor.Execute(ctx, agentName, call)
		results = append(results, result)

		resultMsgs = append(resultMsgs, inference.HistoryMessage{
			Role:    inference.RoleUser,
			Name:    call.ToolID,
			Content: fmt.Sprintf("<tool_response tool_id=\"%s\">%s</tool_response>", call.ToolID, toolResultContent(result)),
		})
	}

	return assistantMsg, resultMsgs, results
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

// prependToolUse prepends <tool_use> tags for each tool call to the prose so
// that the frontend can render them as persistent collapsed blocks, matching
// how <thought> blocks are displayed. Each tag's content is a JSON object
// with name, args, and a truncated result.
func prependToolUse(toolCallLog []toolCallLogEntry, prose string) string {
	if len(toolCallLog) == 0 {
		return prose
	}
	var sb strings.Builder
	for _, entry := range toolCallLog {
		data, _ := json.Marshal(map[string]interface{}{
			"name":   entry.name,
			"args":   entry.args,
			"result": entry.result,
		})
		sb.WriteString("<tool_use>")
		sb.Write(data)
		sb.WriteString("</tool_use>\n")
	}
	if prose != "" {
		sb.WriteString("\n")
		sb.WriteString(prose)
	}
	return sb.String()
}

// truncateResult truncates a tool result string for display in <tool_use> tags.
const truncateResultLen = 500

func truncateResult(s string) string {
	if len(s) <= truncateResultLen {
		return s
	}
	return s[:truncateResultLen] + "…"
}

// toolResultContent returns the text content stored for a tool-role history message.
func toolResultContent(r mcp.ToolResult) string {
	if r.IsError {
		return fmt.Sprintf("error: %s", r.Error)
	}
	return r.Content
}

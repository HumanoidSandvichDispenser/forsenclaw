package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// confirmationEntry tracks a tool call that was yielded for user confirmation.
type confirmationEntry struct {
	call  inference.ToolCallWire
	depID string
}

// InferenceHandler handles a request node by running the inference loop.
// It accumulates history across handler invocations and yields ConfirmationHandler
// deps for tool calls that require user approval.
type InferenceHandler struct {
	req       Request
	agent     *Agent
	registry  *inference.Registry
	assembler Assembler
	executor  ToolExecutor

	history              []inference.HistoryMessage
	pendingConfirmations []confirmationEntry
	turnCount            int // for generating unique dep IDs
}

func (h *InferenceHandler) Handle(ctx context.Context, childResults map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	// If we have pending confirmations, apply their results before continuing.
	if len(h.pendingConfirmations) > 0 {
		if err := h.applyConfirmations(ctx, childResults); err != nil {
			return nil, nil, err
		}
		h.pendingConfirmations = nil
	}

	return h.inferenceLoop(ctx)
}

// applyConfirmations processes the results of outstanding confirmation deps.
func (h *InferenceHandler) applyConfirmations(ctx context.Context, childResults map[string]dag.Result) error {
	for _, entry := range h.pendingConfirmations {
		result, ok := childResults[entry.depID]
		if !ok {
			continue // shouldn't happen — DAG ensures all deps resolve before re-calling
		}
		if result.Status == dag.StatusDenied {
			h.history = append(h.history, inference.HistoryMessage{
				Role:       inference.RoleTool,
				Content:    "Action denied by user.",
				Name:       entry.call.Function.Name,
				ToolCallID: entry.call.ID,
			})
			continue
		}
		toolResult, err := h.executor.Execute(ctx, entry.call)
		if err != nil {
			return fmt.Errorf("executing tool %q: %w", entry.call.Function.Name, err)
		}
		h.history = append(h.history, inference.HistoryMessage{
			Role:       inference.RoleTool,
			Content:    toolResult,
			Name:       entry.call.Function.Name,
			ToolCallID: entry.call.ID,
		})
	}
	return nil
}

// inferenceLoop runs inference, processes tool calls, and loops until the agent
// produces a final response or must yield confirmation deps.
func (h *InferenceHandler) inferenceLoop(ctx context.Context) ([]dag.Dep, *dag.Result, error) {
	for {
		payload, err := h.assembler.Assemble(ctx, h.agent, h.req, h.history)
		if err != nil {
			return nil, nil, err
		}

		provider, modelID, err := h.registry.ResolveTier(h.agent.Definition, inference.TierPrimary)
		if err != nil {
			return nil, nil, err
		}
		payload.Model = modelID

		ch, err := provider.Infer(ctx, payload)
		if err != nil {
			return nil, nil, err
		}

		var content strings.Builder
		var toolCalls []inference.ToolCallWire
		for chunk := range ch {
			content.WriteString(chunk.Content)
			toolCalls = append(toolCalls, chunk.ToolCalls...)
		}
		response := content.String()

		if len(toolCalls) == 0 {
			return nil, &dag.Result{Status: dag.StatusAllowed, Content: response}, nil
		}

		h.history = append(h.history, inference.HistoryMessage{
			Role:      inference.RoleAssistant,
			Content:   response,
			ToolCalls: toolCalls,
		})

		var deps []dag.Dep
		for i, tc := range toolCalls {
			switch h.toolEffect(tc.Function.Name) {
			case "allow":
				toolResult, err := h.executor.Execute(ctx, tc)
				if err != nil {
					return nil, nil, fmt.Errorf("executing tool %q: %w", tc.Function.Name, err)
				}
				h.history = append(h.history, inference.HistoryMessage{
					Role:       inference.RoleTool,
					Content:    toolResult,
					Name:       tc.Function.Name,
					ToolCallID: tc.ID,
				})

			case "deny":
				h.history = append(h.history, inference.HistoryMessage{
					Role:       inference.RoleTool,
					Content:    "Action not permitted.",
					Name:       tc.Function.Name,
					ToolCallID: tc.ID,
				})

			case "require_confirmation":
				h.turnCount++
				depID := fmt.Sprintf("confirm_%s_%d_%d", tc.Function.Name, h.turnCount, i)
				h.pendingConfirmations = append(h.pendingConfirmations, confirmationEntry{
					call:  tc,
					depID: depID,
				})
				deps = append(deps, dag.Dep{
					ID:      depID,
					Handler: NewConfirmationHandler(tc),
				})
			}
		}

		if len(deps) > 0 {
			return deps, nil, nil
		}
		// All tool calls handled inline — loop for next inference turn.
	}
}

// toolEffect returns the permission effect for a tool name.
// Falls back to "deny" if no matching permission is found.
func (h *InferenceHandler) toolEffect(toolName string) string {
	for _, perm := range h.agent.Permissions() {
		if perm.Action == toolName || perm.Scope == "*" {
			return perm.Effect
		}
	}
	return "deny"
}

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

	confirmationRegistry *ConfirmationRegistry
	notifier             ConfirmationNotifier

	turnHistory          []inference.HistoryMessage // tool exchanges accumulated this turn
	pendingConfirmations []confirmationEntry
	turnCount            int // for generating unique dep IDs

	// BLP state — computed at the start of each inference loop turn.
	effectiveClearance int
	toolClearances     map[string]int
	toolResources      map[string]string
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

		switch result.Status {
		case dag.StatusDenied:
			h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
				Role:       inference.RoleTool,
				Content:    "Action denied by user.",
				Name:       entry.call.Function.Name,
				ToolCallID: entry.call.ID,
			})

		case dag.StatusRevise:
			h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
				Role:       inference.RoleTool,
				Content:    "User asked you to revise this action: " + result.Content,
				Name:       entry.call.Function.Name,
				ToolCallID: entry.call.ID,
			})

		default: // StatusAllowed
			call := entry.call
			if result.EditedArgs != "" {
				call.Function.Arguments = result.EditedArgs
			}
			toolResult, err := h.executor.Execute(ctx, call)
			if err != nil {
				return fmt.Errorf("executing tool %q: %w", call.Function.Name, err)
			}
			h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
				Role:       inference.RoleTool,
				Content:    toolResult,
				Name:       call.Function.Name,
				ToolCallID: call.ID,
			})
		}
	}
	return nil
}

// inferenceLoop runs inference, processes tool calls, and loops until the agent
// produces a final response or must yield confirmation deps.
func (h *InferenceHandler) inferenceLoop(ctx context.Context) ([]dag.Dep, *dag.Result, error) {
	for {
		// Compute effective clearance for BLP tool gating.
		var err error
		h.effectiveClearance, err = h.assembler.EffectiveClearance(ctx, h.agent, h.req.Payload.RoomID)
		if err != nil {
			return nil, nil, fmt.Errorf("effective clearance: %w", err)
		}

		// Build tool clearance/resource maps and filter/annotate tools for BLP.
		allTools := h.executor.AllDefinitions()
		var tools []inference.ToolDefinition
		tools, h.toolClearances = filterToolsByClearance(allTools, h.effectiveClearance)
		h.toolResources = make(map[string]string, len(allTools))
		for _, t := range allTools {
			h.toolResources[t.Name] = h.executor.ToolResource(t.Name)
		}

		payload, err := h.assembler.Assemble(ctx, h.agent, h.req, tools)
		if err != nil {
			return nil, nil, err
		}
		payload.CurrentTurnHistory = h.turnHistory

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

		h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
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
				h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
					Role:       inference.RoleTool,
					Content:    toolResult,
					Name:       tc.Function.Name,
					ToolCallID: tc.ID,
				})

			case "deny":
				h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
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
					Handler: NewConfirmationHandler(
					tc,
					depID,
					h.agent.Name(),
					h.req.Payload.RoomID,
					h.confirmationRegistry,
					h.notifier,
				),
				})
			}
		}

		if len(deps) > 0 {
			return deps, nil, nil
		}
		// All tool calls handled inline — loop for next inference turn.
	}
}

// toolEffect returns the combined BLP + permission effect for a tool name.
// BLP is checked first (structural, not overridable by permissions), then
// the agent's permission statements are evaluated with deny > require_confirmation > allow
// precedence. Falls back to "deny" if no statement matches.
func (h *InferenceHandler) toolEffect(toolName string) string {
	// BLP pre-check: skip when effectiveClearance has not been computed yet.
	if h.effectiveClearance > 0 {
		tc := h.toolClearances[toolName]
		if tc > h.effectiveClearance {
			return "deny" // no read-up
		}
		if tc < h.effectiveClearance {
			return "require_confirmation" // no write-down without approval
		}
	}

	resource := h.toolResources[toolName]

	// Collect the highest-priority effect across all matching statements.
	// Priority: deny > require_confirmation > allow.
	effect := ""
	for _, stmt := range h.agent.Permissions() {
		if !stmt.Matches("tool:invoke", resource) {
			continue
		}
		switch stmt.Effect {
		case "deny":
			return "deny" // explicit deny short-circuits
		case "require_confirmation":
			effect = "require_confirmation"
		case "allow":
			if effect == "" {
				effect = "allow"
			}
		}
	}
	if effect == "" {
		return "deny" // default deny
	}
	return effect
}


// filterToolsByClearance applies BLP rules to a set of tool definitions.
// Tools with clearance > effectiveClearance are dropped (no read-up).
// Tools with clearance < effectiveClearance are annotated with a write-down
// warning in their description (no write-down without approval).
// Returns the filtered list and a map of tool name → clearance.
func filterToolsByClearance(tools []inference.ToolDefinition, effectiveClearance int) ([]inference.ToolDefinition, map[string]int) {
	clearances := make(map[string]int, len(tools))
	var filtered []inference.ToolDefinition
	for _, t := range tools {
		clearances[t.Name] = t.Clearance
		if t.Clearance > effectiveClearance {
			// No read-up: tool above effective clearance is not injected.
			continue
		}
		if t.Clearance < effectiveClearance {
			// No write-down without approval: annotate with warning.
			t.Description = fmt.Sprintf(
				"[Clearance %d — requires confirmation in this clearance-%d room due to write-down risk. Minimize sensitive content or use propose_handoff for deliberate transfer.]\n\n%s",
				t.Clearance, effectiveClearance, t.Description,
			)
		}
		filtered = append(filtered, t)
	}
	return filtered, clearances
}

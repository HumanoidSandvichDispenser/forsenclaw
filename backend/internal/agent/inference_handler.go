package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/policy"
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
	req          Request
	agent        *Agent
	registry     *inference.Registry
	assembler    Assembler
	executor     ToolExecutor
	streamWriter StreamWriter

	responseWriter       ResponseWriter
	confirmationRegistry *ConfirmationRegistry
	notifier             ConfirmationNotifier

	turnHistory          []inference.HistoryMessage // tool exchanges accumulated this turn
	pendingConfirmations []confirmationEntry
	turnCount            int // for generating unique dep IDs

	// basePayload is the assembled context from the first inference turn.
	// Cached so that subsequent turns don't re-read room history and re-append
	// the RFC, which would make the user's message appear twice in context.
	basePayload *inference.ContextPayload

	// BLP state — computed at the start of each inference loop turn.
	effectiveClearance int
	toolClearances     map[string]int
	toolResources      map[string]string

	// policy evaluates per-call authorization (permissions + BLP). Built from
	// the agent's permission statements at the start of the inference loop.
	policy *policy.Engine
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
			const deniedMsg = "Action denied by user."
			h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
				Role:       inference.RoleTool,
				Content:    deniedMsg,
				Name:       entry.call.Function.Name,
				ToolCallID: entry.call.ID,
			})
			if h.responseWriter != nil {
				if werr := h.responseWriter.WriteToolResult(ctx, h.req.Payload.RoomID, h.agent.Name(), entry.call.ID, entry.call.Function.Name, deniedMsg); werr != nil {
					return fmt.Errorf("writing denied tool result: %w", werr)
				}
			}

		case dag.StatusRevise:
			const revisedMsg = "User asked you to revise this action."
			h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
				Role:       inference.RoleTool,
				Content:    "User asked you to revise this action: " + result.Content,
				Name:       entry.call.Function.Name,
				ToolCallID: entry.call.ID,
			})
			if h.responseWriter != nil {
				if werr := h.responseWriter.WriteToolResult(ctx, h.req.Payload.RoomID, h.agent.Name(), entry.call.ID, entry.call.Function.Name, revisedMsg); werr != nil {
					return fmt.Errorf("writing revised tool result: %w", werr)
				}
			}

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
			h.toolResources[t.Name] = t.Resource
		}
		h.policy = policy.NewEngine(h.agent.Permissions())

		if h.basePayload == nil {
			assembled, err := h.assembler.Assemble(ctx, h.agent, h.req, tools)
			if err != nil {
				return nil, nil, err
			}
			h.basePayload = &assembled
		}
		payload := *h.basePayload
		payload.ToolDefinitions = tools
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
		var usage inference.Usage
		for chunk := range ch {
			content.WriteString(chunk.Content)
			toolCalls = append(toolCalls, chunk.ToolCalls...)
			if chunk.Usage.TotalTokens > 0 {
				usage = chunk.Usage
			}

			// stream delta to clients as it arrives
			if h.streamWriter != nil {
				h.streamWriter.StreamAgentDelta(
					ctx,
					h.req.Payload.RoomID,
					h.agent.Name(),
					chunk.Content,
				)
			}
		}
		response := content.String()

		if len(toolCalls) == 0 {
			return nil, &dag.Result{
				Status:       dag.StatusAllowed,
				Content:      response,
				InputTokens:  usage.PromptTokens,
				OutputTokens: usage.CompletionTokens,
			}, nil
		}

		h.turnHistory = append(h.turnHistory, inference.HistoryMessage{
			Role:      inference.RoleAssistant,
			Content:   response,
			ToolCalls: toolCalls,
		})

		// Persist the assistant's tool call message immediately so the
		// frontend sees the invocation before execution completes.
		if h.responseWriter != nil {
			werr := h.responseWriter.WriteAgentResponse(
				ctx,
				h.req.Payload.RoomID,
				h.agent.Name(),
				response,
				toolCalls,
				usage.PromptTokens,
				usage.CompletionTokens,
			)
			if werr != nil {
				return nil, nil, fmt.Errorf("writing tool call message: %w", werr)
			}
		}

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
				if h.responseWriter != nil {
					if werr := h.responseWriter.WriteToolResult(ctx, h.req.Payload.RoomID, h.agent.Name(), tc.ID, tc.Function.Name, toolResult); werr != nil {
						return nil, nil, fmt.Errorf("writing tool result: %w", werr)
					}
				}

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
					ID: depID,
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

// toolEffect returns the combined BLP + permission effect for a tool name by
// delegating to the policy engine. BLP is checked first (structural, not
// overridable by permissions), then permission statements are evaluated with
// deny > require_confirmation > allow precedence, defaulting to deny.
func (h *InferenceHandler) toolEffect(toolName string) string {
	// Lazily build the engine so direct-constructed handlers (e.g. in tests)
	// that never enter the inference loop still evaluate correctly.
	if h.policy == nil {
		h.policy = policy.NewEngine(h.agent.Permissions())
	}
	d := h.policy.Evaluate(policy.Query{
		Action:            "tool:invoke",
		Resource:          h.toolResources[toolName],
		SubjectClearance:  h.effectiveClearance,
		ResourceClearance: h.toolClearances[toolName],
	})
	return string(d.Effect)
}

// filterToolsByClearance applies BLP rules to a set of tool definitions to
// decide which tools are injected into the model's context. Tools the subject
// cannot read up to are dropped; tools that would be a write-down are annotated
// with a warning in their description. The BLP rule itself lives in
// policy.ClearanceCheck — only the prompt-facing annotation lives here.
// Returns the filtered list and a map of tool name → clearance.
func filterToolsByClearance(tools []inference.ToolDefinition, effectiveClearance int) ([]inference.ToolDefinition, map[string]int) {
	clearances := make(map[string]int, len(tools))
	var filtered []inference.ToolDefinition
	for _, t := range tools {
		clearances[t.Name] = t.Clearance
		if d, ok := policy.ClearanceCheck(effectiveClearance, t.Clearance); ok {
			switch d.Reason {
			case policy.ReasonReadUp:
				// No read-up: tool above effective clearance is not injected.
				continue
			case policy.ReasonWriteDown:
				// No write-down without approval: annotate with warning.
				t.Description = fmt.Sprintf(
					"[Clearance %d — requires confirmation in this clearance-%d room due to write-down risk. Minimize sensitive content or use propose_handoff for deliberate transfer.]\n\n%s",
					t.Clearance, effectiveClearance, t.Description,
				)
			}
		}
		filtered = append(filtered, t)
	}
	return filtered, clearances
}

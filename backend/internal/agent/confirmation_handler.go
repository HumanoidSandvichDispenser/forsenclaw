package agent

import (
	"context"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// ConfirmationHandler manages the user confirmation flow for a single tool call.
// On Handle, it notifies the room of the pending confirmation and blocks until
// the user responds via Respond. The runtime calls Respond via AgentRuntime.Respond.
type ConfirmationHandler struct {
	call     inference.ToolCallWire
	response chan dag.Result
}

// NewConfirmationHandler creates a ConfirmationHandler for the given tool call.
func NewConfirmationHandler(call inference.ToolCallWire) *ConfirmationHandler {
	return &ConfirmationHandler{
		call:     call,
		response: make(chan dag.Result, 1),
	}
}

func (h *ConfirmationHandler) Handle(ctx context.Context, _ map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	// TODO: notify room that this tool call is awaiting confirmation.
	// The frontend renders the call details and allow/deny/edit/revise controls.
	panic("not implemented: room notification")

	select { //nolint:govet // unreachable after panic — remove panic when implemented
	case result := <-h.response:
		return nil, &result, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// Respond delivers the user's decision to the handler.
// Called indirectly via AgentRuntime.Respond from the API layer.
func (h *ConfirmationHandler) Respond(result dag.Result) {
	h.response <- result
}

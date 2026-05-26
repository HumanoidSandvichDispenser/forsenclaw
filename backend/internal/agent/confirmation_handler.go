package agent

import (
	"context"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// ConfirmationHandler manages the user confirmation flow for a single tool call.
// On Handle, it registers in the registry, notifies the room via the notifier,
// and blocks until the user responds via Respond or the context is cancelled.
// The runtime calls Respond via AgentRuntime.Respond.
type ConfirmationHandler struct {
	call      inference.ToolCallWire
	nodeID    string
	agentName string
	roomID    string
	registry  *ConfirmationRegistry
	notifier  ConfirmationNotifier

	response chan dag.Result
}

// NewConfirmationHandler creates a ConfirmationHandler for the given tool call.
func NewConfirmationHandler(
	call inference.ToolCallWire,
	nodeID, agentName, roomID string,
	registry *ConfirmationRegistry,
	notifier ConfirmationNotifier,
) *ConfirmationHandler {
	return &ConfirmationHandler{
		call:      call,
		nodeID:    nodeID,
		agentName: agentName,
		roomID:    roomID,
		registry:  registry,
		notifier:  notifier,
		response:  make(chan dag.Result, 1),
	}
}

func (h *ConfirmationHandler) Handle(ctx context.Context, _ map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	pc := PendingConfirmation{
		NodeID:    h.nodeID,
		AgentName: h.agentName,
		RoomID:    h.roomID,
		ToolName:  h.call.Function.Name,
		Args:      h.call.Function.Arguments,
	}

	if h.registry != nil {
		h.registry.Register(pc)
	}
	if h.notifier != nil {
		h.notifier.NotifyConfirmationPending(h.roomID, pc)
	}

	defer func() {
		if h.registry != nil {
			h.registry.Deregister(h.roomID, h.nodeID)
		}
	}()

	select {
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

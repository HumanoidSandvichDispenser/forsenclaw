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
	roomID    int64
	reason    string
	registry  *ConfirmationRegistry
	notifier  ConfirmationNotifier

	response chan dag.Result
}

// NewConfirmationHandler creates a ConfirmationHandler for the given tool call.
// reason explains why confirmation is required (a policy.Reason value) so the
// UI can distinguish e.g. a declassification write-down from a plain approval.
func NewConfirmationHandler(
	call inference.ToolCallWire,
	nodeID, agentName string,
	roomID int64,
	reason string,
	registry *ConfirmationRegistry,
	notifier ConfirmationNotifier,
) *ConfirmationHandler {
	return &ConfirmationHandler{
		call:      call,
		nodeID:    nodeID,
		agentName: agentName,
		roomID:    roomID,
		reason:    reason,
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
		Reason:    h.reason,
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

// Describe identifies this node to the DAG viewer: a confirmation node labelled
// with the tool awaiting approval, waiting on the policy reason (e.g.
// "blp_write_down").
func (h *ConfirmationHandler) Describe() dag.NodeInfo {
	return dag.NodeInfo{
		Kind:      KindConfirmation,
		Label:     h.call.Function.Name,
		WaitingOn: h.reason,
	}
}

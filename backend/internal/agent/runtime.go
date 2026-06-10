package agent

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/tool"
)

// ResponseWriter persists completed agent responses to the room transcript.
// Defined here as an interface to avoid an import cycle with the store/api packages.
type ResponseWriter interface {
	WriteAgentResponse(ctx context.Context, roomID int64, agentName string, content string, toolCalls []inference.ToolCallWire, usage inference.Usage) error
	WriteToolResult(ctx context.Context, roomID int64, agentName string, toolCallID string, toolName string, result string) error
}

type StreamWriter interface {
	StreamAgentDelta(ctx context.Context, roomID int64, agentName string, delta string) error
	// StreamAgentError surfaces a failed inference turn to the room so clients
	// can show why and stop any in-progress typing indicator.
	StreamAgentError(ctx context.Context, roomID int64, agentName string, message string) error
}

// DAGStreamWriter streams a single DAG node-state transition for an agent to
// subscribed clients. Calls originate from the DAG's transition observer, which
// runs under the DAG lock, so implementations must not block or call back into
// the runtime or DAG.
type DAGStreamWriter interface {
	StreamDAGUpdate(agentName string, node DAGNode)
}

// Assembler assembles the context window for an agent invocation.
// Defined here as an interface to avoid an import cycle with the memory package.
type Assembler interface {
	Assemble(ctx context.Context, agent *Agent, req Request, tools []inference.ToolDefinition) (inference.ContextPayload, error)
	// EffectiveClearance returns the effective clearance for the given agent
	// and room ID: min(agent.Clearance, room.Clearance). If roomID is zero,
	// returns the agent's clearance. This is used for BLP tool filtering.
	EffectiveClearance(ctx context.Context, agent *Agent, roomID int64) (int, error)
}

// Compactor compacts an agent's transcript for a room after a turn settles.
// Defined here as an interface to avoid an import cycle with the memory package.
type Compactor interface {
	MaybeCompact(ctx context.Context, agent *Agent, roomID int64) error
}

// ToolExecutor executes tool calls and supplies tool definitions.
// Defined here as an interface to avoid a direct dependency on the tool package's
// concrete executor.
type ToolExecutor interface {
	AllDefinitions() []inference.ToolDefinition
	Execute(ctx context.Context, inv tool.Invocation, call inference.ToolCallWire) (string, error)
}

// ResourceClearanceResolver is optionally implemented by a ToolExecutor to
// derive a per-call resource clearance from a tool call's arguments, overriding
// the static tool clearance for policy evaluation. ok is false when the call
// has no argument-derived clearance, in which case the static value is used.
type ResourceClearanceResolver interface {
	ResolveResourceClearance(call inference.ToolCallWire) (int, bool)
}

// RuntimeDeps groups optional dependencies for AgentRuntime.
type RuntimeDeps struct {
	Registry             *inference.Registry
	Assembler            Assembler
	Executor             ToolExecutor
	ConfirmationRegistry *ConfirmationRegistry
	Notifier             ConfirmationNotifier
	ResponseWriter       ResponseWriter
	StreamWriter         StreamWriter
	DAGStream            DAGStreamWriter
	Compactor            Compactor
	ResourcePolicies     []config.Statement
}

// AgentRuntime drives the request DAG for a single agent.
// Each agent gets exactly one runtime; concurrency is across agents, not within one.
type AgentRuntime struct {
	mu    sync.Mutex
	agent *Agent
	dag   *dag.DAG

	registry             *inference.Registry
	assembler            Assembler
	executor             ToolExecutor
	confirmationRegistry *ConfirmationRegistry
	notifier             ConfirmationNotifier

	// responseWriter is called after a root InferenceHandler node resolves.
	// May be nil (no-op in that case).
	responseWriter ResponseWriter
	streamWriter   StreamWriter
	dagStream      DAGStreamWriter

	// compactor runs after a root inference node resolves; may be nil.
	compactor Compactor

	// resourcePolicies are restriction-only statements scoped to resources,
	// passed into each request's policy engine alongside the agent's grants.
	resourcePolicies []config.Statement

	// now supplies the clock for node timing; overridable in tests. The dag
	// package is time-free, so the runtime owns when transitions happened.
	now func() time.Time

	// timing records per-node lifecycle timestamps, written from the transition
	// observer and read when composing a DAGNode. Guarded by timingMu, which is
	// independent of mu to keep the observer off the mu→dag.mu lock path.
	timingMu sync.Mutex
	timing   map[string]*nodeTiming

	// work is pulsed when new work may be available.
	work chan struct{}

	// idle is broadcast whenever the runtime has no pending or in-progress nodes.
	idle *sync.Cond
}

// NewAgentRuntime creates a runtime for the given agent.
func NewAgentRuntime(agent *Agent, deps RuntimeDeps) *AgentRuntime {
	r := &AgentRuntime{
		agent:                agent,
		dag:                  dag.New(),
		registry:             deps.Registry,
		assembler:            deps.Assembler,
		executor:             deps.Executor,
		confirmationRegistry: deps.ConfirmationRegistry,
		notifier:             deps.Notifier,
		responseWriter:       deps.ResponseWriter,
		streamWriter:         deps.StreamWriter,
		dagStream:            deps.DAGStream,
		compactor:            deps.Compactor,
		resourcePolicies:     deps.ResourcePolicies,
		now:                  time.Now,
		timing:               make(map[string]*nodeTiming),
		work:                 make(chan struct{}, 1),
	}
	r.idle = sync.NewCond(&r.mu)
	r.dag.Observe(r.observe)
	return r
}

// Enqueue adds a request to the DAG and wakes the run loop. If the DAG has
// fully settled since the last request, it is reset first so the viewer shows
// only live work — settled nodes from the previous request persist until this
// next request arrives (the until-next-request retention model). In-flight work
// is never discarded. mu is held so the settled-check, reset, and add are atomic
// against a concurrent Enqueue.
func (r *AgentRuntime) Enqueue(req Request) {
	r.mu.Lock()
	if r.dag.AllSettled() {
		r.dag.Reset()
		r.resetTiming()
	}
	r.dag.Add(req.ID, &InferenceHandler{
		req:                  req,
		agent:                r.agent,
		registry:             r.registry,
		assembler:            r.assembler,
		executor:             r.executor,
		confirmationRegistry: r.confirmationRegistry,
		notifier:             r.notifier,
		streamWriter:         r.streamWriter,
		responseWriter:       r.responseWriter,
		resourcePolicies:     r.resourcePolicies,
	}, "")
	r.mu.Unlock()
	r.pulse()
}

// Respond delivers a user decision to a confirmation node.
// Called by the API layer when the user approves, denies, or modifies a tool call.
// Returns false if the node does not exist or is not a confirmation handler.
func (r *AgentRuntime) Respond(nodeID string, result dag.Result) bool {
	node := r.dag.Get(nodeID)
	if node == nil {
		return false
	}
	h, ok := node.Handler.(interface{ Respond(dag.Result) })
	if !ok {
		return false
	}
	h.Respond(result)
	return true
}

// Run is the main processing loop. Runs until ctx is cancelled.
func (r *AgentRuntime) Run(ctx context.Context) {
	for {
		if node := r.dag.NextReady(); node != nil {
			r.runNode(ctx, node)
			continue
		}

		// Only broadcast idle when all nodes have settled — goroutines may
		// still be running even when there are no pending nodes.
		r.mu.Lock()
		if r.dag.AllSettled() {
			r.idle.Broadcast()
		}
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-r.work:
		}
	}
}

// WaitIdle blocks until all nodes in the DAG have settled (resolved or failed).
func (r *AgentRuntime) WaitIdle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for !r.dag.AllSettled() {
		r.idle.Wait()
	}
}

// Snapshot returns the agent's current DAG as viewer nodes: the dag package's
// structural projection composed with runtime-recorded timing.
func (r *AgentRuntime) Snapshot() []DAGNode {
	views := r.dag.Snapshot()
	out := make([]DAGNode, len(views))
	for i, v := range views {
		out[i] = r.nodeView(v)
	}
	return out
}

// observe is the DAG transition observer. It records timing and forwards the
// transition to the stream. Runs under the DAG lock — must not block.
func (r *AgentRuntime) observe(v dag.NodeView) {
	r.recordTiming(v)
	if r.dagStream != nil {
		r.dagStream.StreamDAGUpdate(r.agent.Name(), r.nodeView(v))
	}
}

// recordTiming stamps the node's lifecycle timestamp for the observed state.
// set-if-zero is required because the pending state is emitted both on creation
// (Add) and on re-readiness (a blocked parent's last child clearing); only the
// first counts as created.
func (r *AgentRuntime) recordTiming(v dag.NodeView) {
	r.timingMu.Lock()
	defer r.timingMu.Unlock()
	t := r.timing[v.ID]
	if t == nil {
		t = &nodeTiming{}
		r.timing[v.ID] = t
	}
	switch v.State {
	case dag.NodePending:
		if t.created.IsZero() {
			t.created = r.now()
		}
	case dag.NodeInProgress:
		if t.started.IsZero() {
			t.started = r.now()
		}
	case dag.NodeResolved, dag.NodeFailed:
		if t.settled.IsZero() {
			t.settled = r.now()
		}
	}
}

// resetTiming clears all recorded timing. Called under mu alongside dag.Reset.
func (r *AgentRuntime) resetTiming() {
	r.timingMu.Lock()
	r.timing = make(map[string]*nodeTiming)
	r.timingMu.Unlock()
}

// nodeView composes a structural NodeView with its recorded timing.
func (r *AgentRuntime) nodeView(v dag.NodeView) DAGNode {
	n := DAGNode{NodeView: v}
	r.timingMu.Lock()
	if t := r.timing[v.ID]; t != nil {
		n.CreatedAt = nonZeroTime(t.created)
		n.StartedAt = nonZeroTime(t.started)
		n.SettledAt = nonZeroTime(t.settled)
	}
	r.timingMu.Unlock()
	return n
}

// pulse sends a non-blocking wake signal to the run loop.
func (r *AgentRuntime) pulse() {
	select {
	case r.work <- struct{}{}:
	default:
	}
}

// ResolveExternal resolves a node that was blocked on an external dependency
// (e.g. another agent's response) and wakes the run loop.
func (r *AgentRuntime) ResolveExternal(nodeID string, result dag.Result) {
	r.dag.Resolve(nodeID, result)
	r.pulse()
}

// FailExternal fails a node that was blocked on an external dependency and
// wakes the run loop.
func (r *AgentRuntime) FailExternal(nodeID string, err error) {
	r.dag.Fail(nodeID, err)
	r.pulse()
}

// runNode executes one node in a goroutine and acts on the result.
func (r *AgentRuntime) runNode(ctx context.Context, node *dag.Node) {
	childResults := r.collectChildResults(node)
	go func() {
		deps, result, err := node.Handler.Handle(ctx, childResults)
		switch {
		case err != nil:
			r.dag.Fail(node.ID, err)
			// Surface a failed inference turn to the room: without this the
			// client never learns the turn died and leaves the typing indicator
			// spinning. Only inference roots map to a room turn.
			if ih, ok := node.Handler.(*InferenceHandler); ok && r.streamWriter != nil {
				if serr := r.streamWriter.StreamAgentError(ctx, ih.req.Payload.RoomID, r.agent.Name(), err.Error()); serr != nil {
					log.Printf("agent %s: failed to stream error: %v", r.agent.Name(), serr)
				}
			}
			log.Printf("agent %s: node %s failed: %v", r.agent.Name(), node.ID, err)
		case result != nil:
			r.dag.Resolve(node.ID, *result)
			if ih, ok := node.Handler.(*InferenceHandler); ok {
				if r.responseWriter != nil && result.Content != "" {
					werr := r.responseWriter.WriteAgentResponse(
						ctx,
						ih.req.Payload.RoomID,
						r.agent.Name(),
						result.Content,
						nil,
						inference.Usage{
							PromptTokens:     result.InputTokens,
							CompletionTokens: result.OutputTokens,
							CachedTokens:     result.CachedTokens,
						},
					)
					if werr != nil {
						log.Printf("agent %s: failed to write response: %v", r.agent.Name(), werr)
					}
				}
				// The turn has settled; compact the transcript if it has grown
				// past the configured trigger. Best-effort — never fail the turn.
				if r.compactor != nil && ih.req.Payload.RoomID != 0 {
					if cerr := r.compactor.MaybeCompact(ctx, r.agent, ih.req.Payload.RoomID); cerr != nil {
						log.Printf("agent %s: compaction failed: %v", r.agent.Name(), cerr)
					}
				}
			}
		default:
			for _, dep := range deps {
				r.dag.Add(dep.ID, dep.Handler, node.ID)
			}
		}
		r.pulse()
	}()
}

// collectChildResults gathers results from all resolved children of a node.
func (r *AgentRuntime) collectChildResults(node *dag.Node) map[string]dag.Result {
	results := make(map[string]dag.Result, len(node.Children))
	for _, childID := range node.Children {
		if child := r.dag.Get(childID); child != nil && child.Result != nil {
			results[childID] = *child.Result
		}
	}
	return results
}

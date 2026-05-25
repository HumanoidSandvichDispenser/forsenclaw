package agent

import (
	"context"
	"sync"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// Assembler assembles the context window for an agent invocation.
// Defined here as an interface to avoid an import cycle with the memory package.
type Assembler interface {
	Assemble(ctx context.Context, agent *Agent, req Request, tools []inference.ToolDefinition) (inference.ContextPayload, error)
}

// ToolExecutor executes tool calls and supplies tool definitions.
// Defined here as an interface to avoid a direct dependency on the mcp package.
type ToolExecutor interface {
	AllDefinitions() []inference.ToolDefinition
	Execute(ctx context.Context, call inference.ToolCallWire) (string, error)
}

// AgentRuntime drives the request DAG for a single agent.
// Each agent gets exactly one runtime; concurrency is across agents, not within one.
type AgentRuntime struct {
	mu    sync.Mutex
	agent *Agent
	dag   *dag.DAG

	registry  *inference.Registry
	assembler Assembler
	executor  ToolExecutor

	// work is pulsed when new work may be available.
	work chan struct{}

	// idle is broadcast whenever the runtime has no pending or in-progress nodes.
	idle *sync.Cond
}

// NewAgentRuntime creates a runtime for the given agent.
func NewAgentRuntime(agent *Agent, registry *inference.Registry, assembler Assembler, executor ToolExecutor) *AgentRuntime {
	r := &AgentRuntime{
		agent:     agent,
		dag:       dag.New(),
		registry:  registry,
		assembler: assembler,
		executor:  executor,
		work:      make(chan struct{}, 1),
	}
	r.idle = sync.NewCond(&r.mu)
	return r
}

// Enqueue adds a request to the DAG and wakes the run loop.
func (r *AgentRuntime) Enqueue(req Request) {
	r.dag.Add(req.ID, &InferenceHandler{
		req:       req,
		agent:     r.agent,
		registry:  r.registry,
		assembler: r.assembler,
		executor:  r.executor,
	}, "")
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
		case result != nil:
			r.dag.Resolve(node.ID, *result)
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

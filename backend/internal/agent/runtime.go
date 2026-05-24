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
	// TODO: define when we know what the runtime needs
}

// AgentRuntime drives the request DAG for a single agent.
// Each agent gets exactly one runtime; concurrency is across agents, not within one.
type AgentRuntime struct {
	mu    sync.Mutex
	agent *Agent
	dag   *dag.DAG

	registry  *inference.Registry
	assembler Assembler

	// work is pulsed when new work may be available.
	work chan struct{}

	// idle is broadcast whenever the runtime has no pending or in-progress nodes.
	idle *sync.Cond
}

// NewAgentRuntime creates a runtime for the given agent.
func NewAgentRuntime(agent *Agent, registry *inference.Registry, assembler Assembler) *AgentRuntime {
	r := &AgentRuntime{
		agent:     agent,
		dag:       dag.New(),
		registry:  registry,
		assembler: assembler,
		work:      make(chan struct{}, 1),
	}
	r.idle = sync.NewCond(&r.mu)
	return r
}

// Enqueue adds a request to the DAG and wakes the run loop.
func (r *AgentRuntime) Enqueue(req Request) {
	r.dag.Add(req.ID, &InferenceHandler{req: req}, "")
	r.pulse()
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

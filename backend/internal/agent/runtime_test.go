package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
)

// newTestRuntime builds a minimal AgentRuntime for testing.
// agent/registry/assembler are nil since mock handlers don't use them.
func newTestRuntime() *AgentRuntime {
	r := &AgentRuntime{
		dag:  dag.New(),
		work: make(chan struct{}, 1),
	}
	r.idle = sync.NewCond(&r.mu)
	return r
}

// --- Mock handlers for the confirmation-flow scenario ---

// revisionHandler resolves immediately with a revised prompt.
type revisionHandler struct{}

func (h *revisionHandler) Handle(_ context.Context, _ map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	return nil, &dag.Result{Status: dag.StatusAllowed, Content: "revised prompt"}, nil
}

// confirmationHandler yields a revision dep on first call, then resolves with
// the configured status on second call.
type confirmationHandler struct {
	calls  int
	status dag.ResultStatus
}

func (h *confirmationHandler) Handle(_ context.Context, childResults map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	h.calls++
	if h.calls == 1 {
		return []dag.Dep{{ID: "revision", Handler: &revisionHandler{}}}, nil, nil
	}
	return nil, &dag.Result{Status: h.status, Content: childResults["revision"].Content}, nil
}

// agentBHandler records that it was called.
type agentBHandler struct {
	called *bool
}

func (h *agentBHandler) Handle(_ context.Context, _ map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	*h.called = true
	return nil, &dag.Result{Status: dag.StatusAllowed, Content: "B response"}, nil
}

// inferenceAHandler models Agent A's inference:
//   - Call 1: yields a confirmation dep
//   - Call 2 (after confirmation settles):
//     denied → resolves without Agent B
//     allowed → yields agent_b dep
//   - Call 3 (after agent_b settles): resolves
type inferenceAHandler struct {
	calls              int
	confirmationStatus dag.ResultStatus
	agentBCalled       *bool
}

func (h *inferenceAHandler) Handle(_ context.Context, childResults map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	h.calls++
	switch h.calls {
	case 1:
		return []dag.Dep{{ID: "confirmation", Handler: &confirmationHandler{status: h.confirmationStatus}}}, nil, nil
	case 2:
		if childResults["confirmation"].Status == dag.StatusDenied {
			return nil, &dag.Result{Status: dag.StatusAllowed, Content: "done without B"}, nil
		}
		return []dag.Dep{{ID: "agent_b", Handler: &agentBHandler{called: h.agentBCalled}}}, nil, nil
	default:
		return nil, &dag.Result{Status: dag.StatusAllowed, Content: "done with B"}, nil
	}
}

// TestConfirmationFlow_Deny: user denies after revision — agent B must not be invoked.
func TestConfirmationFlow_Deny(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := newTestRuntime()
	agentBCalled := false
	r.dag.Add("inference_a", &inferenceAHandler{
		confirmationStatus: dag.StatusDenied,
		agentBCalled:       &agentBCalled,
	}, "")
	r.pulse()

	go r.Run(ctx)
	r.WaitIdle()

	if agentBCalled {
		t.Fatal("agent B must not be called when confirmation is denied")
	}
	if n := r.dag.Get("inference_a"); n.State != dag.NodeResolved {
		t.Fatalf("inference_a: expected resolved, got %s", n.State)
	}
}

// TestConfirmationFlow_Accept: user accepts after revision — agent B must be invoked.
func TestConfirmationFlow_Accept(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := newTestRuntime()
	agentBCalled := false
	r.dag.Add("inference_a", &inferenceAHandler{
		confirmationStatus: dag.StatusAllowed,
		agentBCalled:       &agentBCalled,
	}, "")
	r.pulse()

	go r.Run(ctx)
	r.WaitIdle()

	if !agentBCalled {
		t.Fatal("agent B must be called when confirmation is accepted")
	}
	if n := r.dag.Get("inference_a"); n.State != dag.NodeResolved {
		t.Fatalf("inference_a: expected resolved, got %s", n.State)
	}
}

// TestWaitIdle_ReturnsWhenDrained: WaitIdle blocks until all nodes settle.
func TestWaitIdle_ReturnsWhenDrained(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := newTestRuntime()
	done := &dag.Result{Status: dag.StatusAllowed}
	r.dag.Add("a", &stubRuntimeHandler{result: done}, "")
	r.dag.Add("b", &stubRuntimeHandler{result: done}, "")
	r.pulse()

	go r.Run(ctx)
	r.WaitIdle()

	if !r.dag.AllSettled() {
		t.Fatal("expected DAG to be fully settled after WaitIdle")
	}
}

// TestResolveExternal_UnblocksRuntime: ResolveExternal resolves an externally-
// blocked node and wakes the run loop.
func TestResolveExternal_UnblocksRuntime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := newTestRuntime()
	done := &dag.Result{Status: dag.StatusAllowed}

	// parent waits for "ext" which is never added by any handler — simulates
	// an external dependency that the dispatcher resolves.
	r.dag.Add("parent", &stubRuntimeHandler{result: done}, "")
	r.dag.Add("ext", &stubRuntimeHandler{result: done}, "parent")

	// Resolve "ext" externally instead of via a handler.
	r.ResolveExternal("ext", dag.Result{Status: dag.StatusAllowed})
	r.pulse()

	go r.Run(ctx)
	r.WaitIdle()

	if n := r.dag.Get("parent"); n.State != dag.NodeResolved {
		t.Fatalf("parent: expected resolved, got %s", n.State)
	}
}

// stubRuntimeHandler resolves immediately with a fixed result.
type stubRuntimeHandler struct {
	result *dag.Result
}

func (h *stubRuntimeHandler) Handle(_ context.Context, _ map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	return nil, h.result, nil
}

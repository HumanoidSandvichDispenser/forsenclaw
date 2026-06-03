package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// newTestRuntime builds a minimal AgentRuntime for testing.
// agent/registry/assembler are nil since mock handlers don't use them.
func newTestRuntime() *AgentRuntime {
	r := &AgentRuntime{
		dag:    dag.New(),
		now:    time.Now,
		timing: make(map[string]*nodeTiming),
		work:   make(chan struct{}, 1),
	}
	r.idle = sync.NewCond(&r.mu)
	r.dag.Observe(r.observe)
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

// --- DAG viewer: Describe, timing, snapshot, enqueue reset ---

func TestInferenceHandler_Describe(t *testing.T) {
	if got := (&InferenceHandler{}).Describe(); got.Kind != KindInference {
		t.Fatalf("kind = %q, want %q", got.Kind, KindInference)
	}
}

func TestConfirmationHandler_Describe(t *testing.T) {
	h := NewConfirmationHandler(
		inference.ToolCallWire{Function: inference.ToolFunctionWire{Name: "webfetch"}},
		"node1", "agent", 3, "blp_write_down", nil, nil,
	)
	got := h.Describe()
	if got.Kind != KindConfirmation {
		t.Fatalf("kind = %q, want %q", got.Kind, KindConfirmation)
	}
	if got.Label != "webfetch" {
		t.Fatalf("label = %q, want webfetch", got.Label)
	}
	if got.WaitingOn != "blp_write_down" {
		t.Fatalf("waiting_on = %q, want blp_write_down", got.WaitingOn)
	}
}

// fakeClock returns t0, t0+1s, t0+2s, ... on successive calls so timing
// assertions are deterministic and ordered.
func fakeClock() func() time.Time {
	t0 := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	var n int
	return func() time.Time {
		t := t0.Add(time.Duration(n) * time.Second)
		n++
		return t
	}
}

func TestSnapshot_ComposesTimingFromTransitions(t *testing.T) {
	r := newTestRuntime()
	r.now = fakeClock()

	r.dag.Add("root", &stubRuntimeHandler{}, "") // pending  -> created
	r.dag.NextReady()                            // in_prog  -> started
	r.dag.Resolve("root", dag.Result{Status: dag.StatusAllowed})

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 node, got %d", len(snap))
	}
	n := snap[0]
	if n.CreatedAt == nil || n.StartedAt == nil || n.SettledAt == nil {
		t.Fatalf("expected all timestamps set, got %+v", n)
	}
	if !n.CreatedAt.Before(*n.StartedAt) || !n.StartedAt.Before(*n.SettledAt) {
		t.Fatalf("timestamps not ordered: created=%v started=%v settled=%v",
			n.CreatedAt, n.StartedAt, n.SettledAt)
	}
}

// TestTiming_CreatedSetOnce: pending fires both on Add and when a blocked parent
// becomes ready again; only the first sets created.
func TestTiming_CreatedSetOnce(t *testing.T) {
	r := newTestRuntime()
	r.now = fakeClock()

	r.dag.Add("parent", &stubRuntimeHandler{}, "") // parent created at t0
	wantCreated := r.Snapshot()[0].CreatedAt

	r.dag.Add("child", &stubRuntimeHandler{}, "parent")           // parent -> blocked
	r.dag.Resolve("child", dag.Result{Status: dag.StatusAllowed}) // parent -> pending again

	var parent DAGNode
	for _, n := range r.Snapshot() {
		if n.ID == "parent" {
			parent = n
		}
	}
	if parent.CreatedAt == nil || !parent.CreatedAt.Equal(*wantCreated) {
		t.Fatalf("parent created changed on re-readiness: want %v, got %v", wantCreated, parent.CreatedAt)
	}
}

func TestEnqueue_ResetsWhenSettled(t *testing.T) {
	r := newTestRuntime()

	r.dag.Add("old", &stubRuntimeHandler{}, "")
	r.dag.Resolve("old", dag.Result{Status: dag.StatusAllowed}) // DAG now settled

	r.Enqueue(Request{ID: "new"})

	snap := r.Snapshot()
	if len(snap) != 1 || snap[0].ID != "new" {
		t.Fatalf("expected only new node after reset, got %+v", snap)
	}
	r.timingMu.Lock()
	_, oldTiming := r.timing["old"]
	r.timingMu.Unlock()
	if oldTiming {
		t.Fatal("expected old node timing to be cleared on reset")
	}
}

func TestEnqueue_KeepsInFlightWork(t *testing.T) {
	r := newTestRuntime()

	r.dag.Add("inflight", &stubRuntimeHandler{}, "") // pending, not settled

	r.Enqueue(Request{ID: "new"})

	if len(r.Snapshot()) != 2 {
		t.Fatalf("expected in-flight node preserved alongside new, got %d nodes", len(r.Snapshot()))
	}
}

// recordingDAGStream captures StreamDAGUpdate calls.
type recordingDAGStream struct {
	mu      sync.Mutex
	updates []DAGNode
}

func (s *recordingDAGStream) StreamDAGUpdate(_ string, node DAGNode) {
	s.mu.Lock()
	s.updates = append(s.updates, node)
	s.mu.Unlock()
}

func TestObserver_ForwardsToStream(t *testing.T) {
	r := newTestRuntime()
	r.agent = &Agent{Definition: &config.AgentDefinition{Name: "agent"}}
	stream := &recordingDAGStream{}
	r.dagStream = stream

	r.dag.Add("root", &stubRuntimeHandler{}, "") // one pending transition

	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.updates) != 1 {
		t.Fatalf("expected 1 stream update, got %d", len(stream.updates))
	}
	if stream.updates[0].ID != "root" || stream.updates[0].State != dag.NodePending {
		t.Fatalf("unexpected stream update: %+v", stream.updates[0])
	}
}

// mockResponseWriter records WriteAgentResponse calls.
type mockResponseWriter struct {
	calls []mockResponseCall
}

type mockResponseCall struct {
	roomID    int64
	agentName string
	content   string
}

func (m *mockResponseWriter) WriteAgentResponse(_ context.Context, roomID int64, agentName string, content string, _ []inference.ToolCallWire, _, _ int) error {
	m.calls = append(m.calls, mockResponseCall{roomID, agentName, content})
	return nil
}

func (m *mockResponseWriter) WriteToolResult(_ context.Context, _ int64, _ string, _ string, _ string, _ string) error {
	return nil
}

// TestRuntime_NilStreamWriter_DoesNotPanic verifies that a runtime with no
// streamWriter set handles a resolved node without panicking.
func TestRuntime_NilStreamWriter_DoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rw := &mockResponseWriter{}
	r := &AgentRuntime{
		dag:            dag.New(),
		work:           make(chan struct{}, 1),
		responseWriter: rw,
		// streamWriter intentionally nil
	}
	r.idle = sync.NewCond(&r.mu)

	r.dag.Add("node", &stubRuntimeHandler{
		result: &dag.Result{Status: dag.StatusAllowed, Content: "response"},
	}, "")
	r.pulse()

	go r.Run(ctx)
	r.WaitIdle()

	if n := r.dag.Get("node"); n.State != dag.NodeResolved {
		t.Fatalf("node state = %s, want resolved", n.State)
	}
}

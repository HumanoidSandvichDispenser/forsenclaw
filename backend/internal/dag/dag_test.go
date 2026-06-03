package dag

import (
	"context"
	"testing"
)

// stubHandler is a no-op Handler for testing DAG structure.
type stubHandler struct{}

func (s *stubHandler) Handle(_ context.Context, _ map[string]Result) ([]Dep, *Result, error) {
	return nil, nil, nil
}

// describingHandler is a no-op Handler that also self-describes via Describer.
type describingHandler struct {
	info NodeInfo
}

func (h *describingHandler) Handle(_ context.Context, _ map[string]Result) ([]Dep, *Result, error) {
	return nil, nil, nil
}

func (h *describingHandler) Describe() NodeInfo { return h.info }

func TestAddNodeNoParen_StartsPending(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")

	n := d.nodes["a"]
	if n.State != NodePending {
		t.Fatalf("expected pending, got %s", n.State)
	}
}

func TestNextReady_ReturnsNodeAndMarksInProgress(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")

	n := d.NextReady()
	if n == nil {
		t.Fatal("expected a node, got nil")
	}
	if n.ID != "a" {
		t.Fatalf("expected node a, got %s", n.ID)
	}
	if n.State != NodeInProgress {
		t.Fatalf("expected in_progress, got %s", n.State)
	}
}

func TestNextReady_NilWhenNothingPending(t *testing.T) {
	d := New()
	if n := d.NextReady(); n != nil {
		t.Fatalf("expected nil, got %s", n.ID)
	}
}

func TestAddChild_ParentBecomesBlocked(t *testing.T) {
	d := New()
	d.Add("parent", &stubHandler{}, "")
	d.Add("child", &stubHandler{}, "parent")

	if d.nodes["parent"].State != NodeBlocked {
		t.Fatalf("expected parent to be blocked, got %s", d.nodes["parent"].State)
	}
}

func TestResolveChild_ParentBecomesPending(t *testing.T) {
	d := New()
	d.Add("parent", &stubHandler{}, "")
	d.Add("child", &stubHandler{}, "parent")
	d.Resolve("child", Result{Status: StatusAllowed})

	if d.nodes["parent"].State != NodePending {
		t.Fatalf("expected parent to be pending, got %s", d.nodes["parent"].State)
	}
}

func TestFailChild_ParentBecomesPending(t *testing.T) {
	d := New()
	d.Add("parent", &stubHandler{}, "")
	d.Add("child", &stubHandler{}, "parent")
	d.Fail("child", nil)

	if d.nodes["parent"].State != NodePending {
		t.Fatalf("expected parent to be pending, got %s", d.nodes["parent"].State)
	}
}

func TestResolveNonexistent_NoPanic(t *testing.T) {
	d := New()
	d.Resolve("ghost", Result{})
}

func TestMultipleChildren_ParentStaysBlockedUntilLast(t *testing.T) {
	d := New()
	d.Add("parent", &stubHandler{}, "")
	d.Add("child1", &stubHandler{}, "parent")
	d.Add("child2", &stubHandler{}, "parent")

	d.Resolve("child1", Result{Status: StatusAllowed})
	if d.nodes["parent"].State != NodeBlocked {
		t.Fatalf("expected parent still blocked after first child, got %s", d.nodes["parent"].State)
	}

	d.Resolve("child2", Result{Status: StatusAllowed})
	if d.nodes["parent"].State != NodePending {
		t.Fatalf("expected parent pending after last child, got %s", d.nodes["parent"].State)
	}
}

func TestResolveIdempotent_NoPanic(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")
	d.Resolve("a", Result{Status: StatusAllowed})
	d.Resolve("a", Result{Status: StatusAllowed}) // second call should be a no-op
	if d.nodes["a"].State != NodeResolved {
		t.Fatalf("expected resolved, got %s", d.nodes["a"].State)
	}
}

func TestNextReady_InsertionOrder(t *testing.T) {
	d := New()
	d.Add("first", &stubHandler{}, "")
	d.Add("second", &stubHandler{}, "")

	n := d.NextReady()
	if n.ID != "first" {
		t.Fatalf("expected first, got %s", n.ID)
	}
}

func TestAllSettled_EmptyDAG(t *testing.T) {
	d := New()
	if !d.AllSettled() {
		t.Fatal("empty DAG should be settled")
	}
}

func TestAllSettled_FalseWhenPending(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")
	if d.AllSettled() {
		t.Fatal("DAG with pending node should not be settled")
	}
}

func TestAllSettled_FalseWhenBlocked(t *testing.T) {
	d := New()
	d.Add("parent", &stubHandler{}, "")
	d.Add("child", &stubHandler{}, "parent")
	d.Resolve("child", Result{Status: StatusAllowed})
	// parent is now pending; resolve it too so only blocked state is tested
	d.Add("parent2", &stubHandler{}, "")
	d.Add("child2", &stubHandler{}, "parent2")
	// parent2 is blocked on child2, child2 still pending
	if d.AllSettled() {
		t.Fatal("DAG with blocked node should not be settled")
	}
}

func TestAllSettled_TrueWhenAllResolved(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")
	d.Add("b", &stubHandler{}, "")
	d.Resolve("a", Result{Status: StatusAllowed})
	d.Resolve("b", Result{Status: StatusAllowed})
	if !d.AllSettled() {
		t.Fatal("DAG with all resolved nodes should be settled")
	}
}

func TestAllSettled_TrueWhenMixedResolvedAndFailed(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")
	d.Add("b", &stubHandler{}, "")
	d.Resolve("a", Result{Status: StatusAllowed})
	d.Fail("b", nil)
	if !d.AllSettled() {
		t.Fatal("DAG with all resolved/failed nodes should be settled")
	}
}

func TestSnapshot_CapturesStructureAndDescription(t *testing.T) {
	d := New()
	d.Add("parent", &describingHandler{info: NodeInfo{Kind: "inference", Label: "root"}}, "")
	d.Add("child", &stubHandler{}, "parent")

	views := d.Snapshot()
	if len(views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(views))
	}
	parent := views[0]
	if parent.ID != "parent" || parent.State != NodeBlocked {
		t.Fatalf("unexpected parent view: %+v", parent)
	}
	if parent.Kind != "inference" || parent.Label != "root" {
		t.Fatalf("expected description from handler, got kind=%q label=%q", parent.Kind, parent.Label)
	}
	if len(parent.Children) != 1 || parent.Children[0] != "child" {
		t.Fatalf("expected child edge, got %v", parent.Children)
	}
}

func TestSnapshot_UndescribedNodeHasEmptyKind(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")
	if k := d.Snapshot()[0].Kind; k != "" {
		t.Fatalf("expected empty kind for undescribed node, got %q", k)
	}
}

func TestSnapshot_IsCopy(t *testing.T) {
	d := New()
	d.Add("parent", &stubHandler{}, "")
	d.Add("child", &stubHandler{}, "parent")

	views := d.Snapshot()
	views[0].Children[0] = "mutated"
	if got := d.Snapshot()[0].Children[0]; got != "child" {
		t.Fatalf("snapshot mutation leaked into DAG: %q", got)
	}
}

func TestObserver_FiresOnEveryTransition(t *testing.T) {
	d := New()
	var states []NodeState
	d.Observe(func(v NodeView) {
		if v.ID == "child" {
			states = append(states, v.State)
		}
	})
	d.Add("parent", &stubHandler{}, "")
	d.Add("child", &stubHandler{}, "parent") // child pending
	d.NextReady()                            // parent? no — parent is blocked; child is pending
	d.Resolve("child", Result{Status: StatusAllowed})

	// child: pending (add) -> in_progress (NextReady) -> resolved (settle)
	want := []NodeState{NodePending, NodeInProgress, NodeResolved}
	if len(states) != len(want) {
		t.Fatalf("expected %v transitions for child, got %v", want, states)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("transition %d: want %s, got %s", i, want[i], states[i])
		}
	}
}

func TestObserver_FiresOnBlockerCleared(t *testing.T) {
	d := New()
	var parentStates []NodeState
	d.Observe(func(v NodeView) {
		if v.ID == "parent" {
			parentStates = append(parentStates, v.State)
		}
	})
	d.Add("parent", &stubHandler{}, "")
	d.Add("child", &stubHandler{}, "parent")
	d.Resolve("child", Result{Status: StatusAllowed}) // unblocks parent

	// parent: pending (add) -> blocked (child added) -> pending (blocker cleared)
	want := []NodeState{NodePending, NodeBlocked, NodePending}
	if len(parentStates) != len(want) {
		t.Fatalf("expected %v parent transitions, got %v", want, parentStates)
	}
	for i := range want {
		if parentStates[i] != want[i] {
			t.Fatalf("parent transition %d: want %s, got %s", i, want[i], parentStates[i])
		}
	}
}

func TestReset_ClearsNodes(t *testing.T) {
	d := New()
	d.Add("a", &stubHandler{}, "")
	d.Resolve("a", Result{Status: StatusAllowed})
	d.Reset()
	if !d.AllSettled() {
		t.Fatal("reset DAG should be settled")
	}
	if len(d.Snapshot()) != 0 {
		t.Fatalf("expected empty snapshot after reset, got %d nodes", len(d.Snapshot()))
	}
}

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

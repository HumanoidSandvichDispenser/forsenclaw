package agent

import (
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
)

// Node kinds reported by handlers via dag.Describer. The dag package keeps Kind
// an opaque string; this vocabulary is owned by the agent layer, since a
// handler stating what it is is an intrinsic fact about itself. Only the node
// types that actually exist in the DAG are listed: inference roots and
// confirmation deps. (Plain tool calls run inline and are not DAG nodes.)
const (
	KindInference    = "inference"
	KindConfirmation = "confirmation"
)

// DAGNode is the agent-layer view of a DAG node: the structural projection from
// the dag package composed with timing recorded by the runtime. The dag package
// is time-free; timestamps live here.
type DAGNode struct {
	dag.NodeView
	CreatedAt *time.Time `json:"created_at,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	SettledAt *time.Time `json:"settled_at,omitempty"`
}

// nodeTiming records lifecycle timestamps for a single node. Zero values mean
// the transition has not been observed. Written only from the runtime's
// transition observer, read under timingMu.
type nodeTiming struct {
	created time.Time
	started time.Time
	settled time.Time
}

// nonZeroTime returns a pointer to t, or nil if t is the zero value, so unset
// timestamps marshal as absent rather than the zero time.
func nonZeroTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

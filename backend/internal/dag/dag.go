package dag

import (
	"context"
	"sync"
)

// ResultStatus is the outcome of a resolved node.
type ResultStatus string

const (
	StatusAllowed ResultStatus = "allowed"
	StatusDenied  ResultStatus = "denied"
	StatusFailed  ResultStatus = "failed"
)

// Result is what a node produces when it resolves.
type Result struct {
	Content string
	Source  string
	Status  ResultStatus
}

// Handler is the interface implemented by each node type. The runtime calls
// Handle when a node is ready to run. childResults contains the results of
// all resolved dependencies, keyed by node ID.
//
// Returns:
//   - deps: new dependencies to add (if yielding)
//   - result: the final result (if done)
//   - err: unrecoverable failure
type Handler interface {
	Handle(ctx context.Context, childResults map[string]Result) (deps []Dep, result *Result, err error)
}

// Dep describes a dependency a handler wants to add.
type Dep struct {
	ID      string
	Handler Handler
}

// NodeState is the lifecycle state of a node in the DAG.
type NodeState string

const (
	NodePending    NodeState = "pending"     // ready to be picked up
	NodeInProgress NodeState = "in_progress" // currently running
	NodeBlocked    NodeState = "blocked"     // waiting on child nodes
	NodeResolved   NodeState = "resolved"    // completed successfully
	NodeFailed     NodeState = "failed"      // completed with error
)

// Node is a single vertex in the DAG.
type Node struct {
	ID    string
	State NodeState

	Handler Handler

	// BlockedBy holds the IDs of child nodes this node is waiting on.
	BlockedBy []string

	// Children holds the IDs of all child nodes issued by this node.
	Children []string

	// Result is set when the node resolves.
	Result *Result

	// Err is set when the node fails.
	Err error
}

// DAG is a dependency graph of nodes.
type DAG struct {
	mu    sync.Mutex
	nodes map[string]*Node
	order []*Node // insertion order for NextReady
}

// New returns an empty DAG.
func New() *DAG {
	return &DAG{nodes: make(map[string]*Node)}
}

// Add adds a new node to the DAG. If parentID is non-empty, the parent is
// marked as blocked on this node. Panics if handler is nil.
func (d *DAG) Add(id string, handler Handler, parentID string) {
	if handler == nil {
		panic("dag: handler must not be nil")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	n := &Node{
		ID:      id,
		State:   NodePending,
		Handler: handler,
	}
	d.nodes[id] = n
	d.order = append(d.order, n)
	if parentID != "" {
		if parent, ok := d.nodes[parentID]; ok {
			parent.State = NodeBlocked
			parent.BlockedBy = append(parent.BlockedBy, id)
			parent.Children = append(parent.Children, id)
		}
	}
}

// Get returns the node with the given ID, or nil if not found.
func (d *DAG) Get(id string) *Node {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.nodes[id]
}

// NextReady returns the oldest pending node and marks it in-progress, or nil.
func (d *DAG) NextReady() *Node {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, n := range d.order {
		if n.State == NodePending {
			n.State = NodeInProgress
			return n
		}
	}
	return nil
}

// Resolve marks a node as resolved and removes it from its parent's BlockedBy.
func (d *DAG) Resolve(id string, result Result) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.settle(id, func(n *Node) {
		n.State = NodeResolved
		n.Result = &result
	})
}

// Fail marks a node as failed and removes it from its parent's BlockedBy.
func (d *DAG) Fail(id string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.settle(id, func(n *Node) {
		n.State = NodeFailed
		n.Err = err
	})
}

// settle applies f to the node and removes it from its parent's BlockedBy.
// Called under the mutex.
func (d *DAG) settle(id string, f func(*Node)) {
	node, ok := d.nodes[id]
	if !ok {
		return
	}
	f(node)
	d.removeBlocker(id)
}

// AllSettled reports whether every node in the DAG has reached a terminal state
// (resolved or failed). Returns true for an empty DAG.
func (d *DAG) AllSettled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, n := range d.nodes {
		switch n.State {
		case NodePending, NodeInProgress, NodeBlocked:
			return false
		}
	}
	return true
}

// removeBlocker removes id from the BlockedBy list of any node that contains it.
// Called under the mutex.
func (d *DAG) removeBlocker(id string) {
	for _, n := range d.nodes {
		for i, blocker := range n.BlockedBy {
			if blocker == id {
				n.BlockedBy = append(n.BlockedBy[:i], n.BlockedBy[i+1:]...)
				if len(n.BlockedBy) == 0 {
					n.State = NodePending
				}
				return
			}
		}
	}
}

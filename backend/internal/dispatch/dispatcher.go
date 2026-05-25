package dispatch

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
)

// Dispatcher routes incoming requests to the appropriate AgentRuntime.
// Room messages are batched — multiple messages arriving for the same agent
// before it has processed them are combined into a single Request.
// System and event requests bypass batching and are enqueued immediately.
type Dispatcher struct {
	manager *agent.Manager

	mu      sync.Mutex
	pending map[string][]agent.Message // agentName → buffered room messages
	work    chan struct{}
}

// NewDispatcher creates a Dispatcher backed by the given Manager.
func NewDispatcher(manager *agent.Manager) *Dispatcher {
	return &Dispatcher{
		manager: manager,
		pending: make(map[string][]agent.Message),
		work:    make(chan struct{}, 1),
	}
}

// Submit routes a request to the target agent.
// Room requests are buffered and batched; system and event requests are enqueued directly.
func (d *Dispatcher) Submit(req agent.Request) {
	if req.Source == agent.SourceRoom {
		d.mu.Lock()
		d.pending[req.Target] = append(d.pending[req.Target], req.Payload.Messages...)
		d.mu.Unlock()
		d.pulse()
		return
	}

	if rt := d.manager.Runtime(req.Target); rt != nil {
		rt.Enqueue(req)
	}
}

// Run is the dispatch loop. Drains buffered room messages and enqueues them as
// batched Requests. Runs until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		d.mu.Lock()
		batch := d.pending
		d.pending = make(map[string][]agent.Message)
		d.mu.Unlock()

		for agentName, msgs := range batch {
			rt := d.manager.Runtime(agentName)
			if rt == nil {
				continue
			}
			rt.Enqueue(agent.Request{
				ID:     newRequestID(),
				Target: agentName,
				Source: agent.SourceRoom,
				Payload: agent.RequestPayload{
					Messages: msgs,
				},
			})
		}

		select {
		case <-ctx.Done():
			return
		case <-d.work:
		}
	}
}

// pulse sends a non-blocking wake signal to the run loop.
func (d *Dispatcher) pulse() {
	select {
	case d.work <- struct{}{}:
	default:
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

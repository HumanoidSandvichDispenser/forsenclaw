package dispatch

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
)

// roomKey identifies a (agent, room) pair for batching.
type roomKey struct {
	agentName string
	roomID    int64
}

// Dispatcher routes incoming requests to the appropriate AgentRuntime.
// Room messages are batched — multiple messages arriving for the same agent
// and room before it has processed them are combined into a single Request.
// System and event requests bypass batching and are enqueued immediately.
type Dispatcher struct {
	manager *agent.Manager

	mu      sync.Mutex
	pending map[roomKey][]agent.Message // (agentName, roomID) → buffered room messages
	work    chan struct{}
}

// NewDispatcher creates a Dispatcher backed by the given Manager.
func NewDispatcher(manager *agent.Manager) *Dispatcher {
	return &Dispatcher{
		manager: manager,
		pending: make(map[roomKey][]agent.Message),
		work:    make(chan struct{}, 1),
	}
}

// Submit routes a request to the target agent.
// Room requests are buffered and batched; system and event requests are enqueued directly.
func (d *Dispatcher) Submit(req agent.Request) {
	if req.Source == agent.SourceRoom {
		key := roomKey{agentName: req.Target, roomID: req.Payload.RoomID}
		d.mu.Lock()
		d.pending[key] = append(d.pending[key], req.Payload.Messages...)
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
		d.pending = make(map[roomKey][]agent.Message)
		d.mu.Unlock()

		for key, msgs := range batch {
			rt := d.manager.Runtime(key.agentName)
			if rt == nil {
				continue
			}
			rt.Enqueue(agent.Request{
				ID:     newRequestID(),
				Target: key.agentName,
				Source: agent.SourceRoom,
				Payload: agent.RequestPayload{
					RoomID:   key.roomID,
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

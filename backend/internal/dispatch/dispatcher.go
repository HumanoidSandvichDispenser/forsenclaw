package dispatch

import (
	"crypto/rand"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
)

// Dispatcher routes requests to the appropriate AgentRuntime.
// It is the single point through which all agent-bound traffic flows,
// including future cross-agent routing.
type Dispatcher struct {
	manager *agent.Manager
}

// NewDispatcher creates a Dispatcher backed by the given Manager.
func NewDispatcher(manager *agent.Manager) *Dispatcher {
	return &Dispatcher{manager: manager}
}

// Submit routes a request to the target agent's runtime.
func (d *Dispatcher) Submit(req agent.Request) {
	rt := d.manager.Runtime(req.Target)
	if rt == nil {
		return
	}
	if req.ID == "" {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		req.ID = fmt.Sprintf("%x", b)
	}
	rt.Enqueue(req)
}

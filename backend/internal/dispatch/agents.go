package dispatch

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// RegisterAgent starts a goroutine for the given agent and prepares it to
// receive RFCs. If the agent is already registered, this is a no-op.
func (d *Dispatcher) RegisterAgent(ag *agent.Agent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	name := ag.Name()
	if _, ok := d.agents[name]; ok {
		log.Printf("dispatcher: agent %q already registered", name)
		return
	}

	entry := &agentEntry{
		agent: ag,
		queue: make(chan room.RFC, 16),
		done:  make(chan struct{}),
	}
	d.agents[name] = entry

	go d.agentLoop(entry)
	log.Printf("dispatcher: registered agent %q", name)
}

// UnregisterAgent signals the agent's goroutine to stop and waits for it to
// drain its queue. The goroutine exits after processing pending RFCs.
func (d *Dispatcher) UnregisterAgent(name string) {
	d.mu.Lock()
	entry, ok := d.agents[name]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.agents, name)
	close(entry.queue)
	d.mu.Unlock()

	<-entry.done
	log.Printf("dispatcher: unregistered agent %q", name)
}

// All returns a snapshot of all registered agent names.
func (d *Dispatcher) All() map[string]struct{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	snapshot := make(map[string]struct{}, len(d.agents))
	for name := range d.agents {
		snapshot[name] = struct{}{}
	}
	return snapshot
}

// IssueRFC enqueues an RFC to the target agent's processing queue. Returns an
// error if the agent is not registered.
func (d *Dispatcher) IssueRFC(rfc room.RFC) error {
	if rfc.Target == "" {
		return fmt.Errorf("RFC target is required")
	}

	// Extract agent name from actor ID (format: "agent:<name>")
	targetName := strings.TrimPrefix(rfc.Target, "agent:")
	if targetName == rfc.Target {
		return fmt.Errorf("RFC target %q is not an agent", rfc.Target)
	}

	d.mu.RLock()
	entry, ok := d.agents[targetName]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("agent %q is not registered", targetName)
	}

	d.mu.Lock()
	d.processing[rfc.RoomID] = true
	d.mu.Unlock()

	select {
	case entry.queue <- rfc:
		return nil
	default:
		d.mu.Lock()
		delete(d.processing, rfc.RoomID)
		d.mu.Unlock()
		return fmt.Errorf("agent %q RFC queue is full", targetName)
	}
}

// BroadcastRFC sends an RFC to all agent participants in a room simultaneously.
// This is used by protocols like Broadcast. FreeForm does not use this.
func (d *Dispatcher) BroadcastRFC(r *room.Room, payload room.RFCPayload) error {
	var errs []error
	for _, p := range r.Participants {
		if !p.IsAgent() {
			continue
		}
		rfc := room.RFC{
			ID:      uuid.New().String(),
			RoomID:  r.ID,
			Target:  p.ID,
			Payload: payload,
		}
		if err := d.IssueRFC(rfc); err != nil {
			errs = append(errs, fmt.Errorf("broadcast to %s: %w", p.ID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("broadcast errors: %v", errs)
	}
	return nil
}

// HandleUserMessage is the entry point for user messages. It validates the
// message, writes it to the transcript, and notifies the room's protocol.
func (d *Dispatcher) HandleUserMessage(ctx context.Context, roomID string, sender room.Actor, msg room.Message) error {
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	if err := d.appendToTranscript(ctx, roomID, msg); err != nil {
		return fmt.Errorf("append transcript: %w", err)
	}

	r, err := d.store.GetRoom(ctx, roomID)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}

	if r.ParticipantByID(sender.ID) == nil {
		return fmt.Errorf("sender %q is not a participant in room %q", sender.ID, roomID)
	}

	// If an agent is currently processing an RFC for this room, queue as interjection.
	d.mu.Lock()
	if d.processing[roomID] {
		d.interjections[roomID] = append(d.interjections[roomID], msg)
		d.mu.Unlock()
		if d.hub != nil {
			d.hub.Broadcast(roomID, StreamEvent{Type: "interjection_queued", RoomID: roomID})
		}
		return nil
	}
	d.mu.Unlock()

	proto, err := d.getProtocol(ctx, r)
	if err != nil {
		return fmt.Errorf("get protocol: %w", err)
	}
	if err := proto.OnMessage(r, sender, msg); err != nil {
		return fmt.Errorf("protocol onMessage: %w", err)
	}
	return nil
}

// agentLoop is the per-agent goroutine. It drains the RFC queue and sleeps
// when empty.
func (d *Dispatcher) agentLoop(entry *agentEntry) {
	log.Printf("dispatcher: agent loop started for %q", entry.agent.Name())
	defer close(entry.done)

	for rfc := range entry.queue {
		if err := d.processRFC(context.Background(), entry.agent, rfc); err != nil {
			log.Printf("dispatcher: error processing RFC %s for agent %q: %v", rfc.ID, entry.agent.Name(), err)
		}
	}
}

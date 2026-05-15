// Package dispatch provides the RFC dispatcher and per-agent goroutine lifecycle.
// The Dispatcher routes RFCs to agent goroutines, manages room protocols, and
// coordinates transcript I/O.
package dispatch

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// Dispatcher routes RFCs to agents, manages room protocols, and coordinates
// the agent invocation pipeline. It implements the room.Dispatcher interface.
type Dispatcher struct {
	mu sync.RWMutex

	// agents maps agent names to their runtime goroutine entries.
	agents map[string]*agentEntry

	// manager provides access to loaded agent definitions.
	manager *agent.Manager

	// registry resolves model strings to provider adapters.
	registry ModelResolver

	// assembler builds context windows for agent invocations.
	assembler *memory.Assembler

	// store persists room metadata.
	store room.Store

	// paths resolves XDG directories.
	paths *paths.Paths

	// transcripts maps room IDs to their JSONL writers.
	transcripts map[string]*room.TranscriptWriter

	// protocols maps room IDs to their active protocol instances.
	protocols map[string]room.Protocol

	// processing tracks which room has an active RFC being processed.
	processing map[string]bool // room ID -> true

	// interjections queues messages that arrived while an agent was responding.
	interjections map[string][]room.Message // room ID -> queued messages
}

// agentEntry represents a registered agent's goroutine and queue.
type agentEntry struct {
	agent *agent.Agent
	queue chan room.RFC
	done  chan struct{}
}

// ModelResolver resolves model tiers to provider adapters. The inference.Registry
// satisfies this interface.
type ModelResolver interface {
	ResolveTier(agentDef *config.AgentDefinition, tier inference.ModelTier) (inference.Provider, string, error)
}

// NewDispatcher creates a new dispatcher with the given dependencies.
func NewDispatcher(
	mgr *agent.Manager,
	reg ModelResolver,
	asm *memory.Assembler,
	store room.Store,
	p *paths.Paths,
) *Dispatcher {
	return &Dispatcher{
		agents:        make(map[string]*agentEntry),
		manager:       mgr,
		registry:      reg,
		assembler:     asm,
		store:         store,
		paths:         p,
		transcripts:   make(map[string]*room.TranscriptWriter),
		protocols:     make(map[string]room.Protocol),
		processing:    make(map[string]bool),
		interjections: make(map[string][]room.Message),
	}
}

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

	// Mark room as processing
	d.mu.Lock()
	d.processing[rfc.RoomID] = true
	d.mu.Unlock()

	select {
	case entry.queue <- rfc:
		return nil
	default:
		// Queue is full (shouldn't happen with buffer size 16 for normal use)
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

	// Write to transcript
	if err := d.appendToTranscript(ctx, roomID, msg); err != nil {
		return fmt.Errorf("append transcript: %w", err)
	}

	// Get room
	r, err := d.store.GetRoom(ctx, roomID)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}

	// Check if sender is a participant
	if r.ParticipantByID(sender.ID) == nil {
		return fmt.Errorf("sender %q is not a participant in room %q", sender.ID, roomID)
	}

	// If an agent is currently processing an RFC for this room, queue as interjection
	d.mu.Lock()
	if d.processing[roomID] {
		d.interjections[roomID] = append(d.interjections[roomID], msg)
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	// Notify protocol
	proto, err := d.getProtocol(ctx, r)
	if err != nil {
		return fmt.Errorf("get protocol: %w", err)
	}

	if err := proto.OnMessage(r, sender, msg); err != nil {
		return fmt.Errorf("protocol onMessage: %w", err)
	}

	return nil
}

// StartProtocol instantiates and starts the protocol for a room. This must be
// called after a room is created before messages can be handled.
func (d *Dispatcher) StartProtocol(r *room.Room) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var proto room.Protocol
	switch r.ProtocolType {
	case room.ProtocolFreeForm:
		p, err := room.NewFreeFormProtocol(r)
		if err != nil {
			return fmt.Errorf("create freeform protocol: %w", err)
		}
		proto = p
	default:
		return fmt.Errorf("unsupported protocol type: %q", r.ProtocolType)
	}

	if err := proto.Start(r, d); err != nil {
		return fmt.Errorf("start protocol: %w", err)
	}

	d.protocols[r.ID] = proto
	return nil
}

// getProtocol returns the active protocol for a room, creating it if necessary.
func (d *Dispatcher) getProtocol(ctx context.Context, r *room.Room) (room.Protocol, error) {
	d.mu.RLock()
	proto, ok := d.protocols[r.ID]
	d.mu.RUnlock()
	if ok {
		return proto, nil
	}

	// Protocol not started yet — start it now
	if err := d.StartProtocol(r); err != nil {
		return nil, err
	}

	d.mu.RLock()
	proto = d.protocols[r.ID]
	d.mu.RUnlock()
	return proto, nil
}

// appendToTranscript writes a message to the room's JSONL transcript file.
func (d *Dispatcher) appendToTranscript(ctx context.Context, roomID string, msg room.Message) error {
	d.mu.Lock()
	writer, ok := d.transcripts[roomID]
	if !ok {
		var err error
		writer, err = room.NewTranscriptWriter(d.paths.RoomsDir(), roomID)
		if err != nil {
			d.mu.Unlock()
			return fmt.Errorf("create transcript writer: %w", err)
		}
		d.transcripts[roomID] = writer
	}
	d.mu.Unlock()

	return writer.Append(ctx, msg)
}

// agentLoop is the per-agent goroutine. It drains the RFC queue and sleeps
// when empty.
func (d *Dispatcher) agentLoop(entry *agentEntry) {
	defer close(entry.done)

	for rfc := range entry.queue {
		if err := d.processRFC(context.Background(), entry.agent, rfc); err != nil {
			log.Printf("dispatcher: error processing RFC %s for agent %q: %v", rfc.ID, entry.agent.Name(), err)
		}
	}
}

// processRFC assembles context, invokes the model, and delivers the response.
func (d *Dispatcher) processRFC(ctx context.Context, ag *agent.Agent, rfc room.RFC) error {
	// 1. Get room
	r, err := d.store.GetRoom(ctx, rfc.RoomID)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}

	// 2. Collect any pending interjections for this room
	d.mu.Lock()
	interjections := d.interjections[rfc.RoomID]
	d.interjections[rfc.RoomID] = nil
	d.mu.Unlock()

	// 3. Read room history from transcript
	history, err := room.ReadMessages(ctx, d.paths.RoomsDir(), rfc.RoomID, room.ReadOpts{})
	if err != nil {
		return fmt.Errorf("read history: %w", err)
	}

	// 4. Assemble context
	assembled, err := d.assembler.Assemble(ctx, ag, memory.AssembleRequest{
		RoomID:        rfc.RoomID,
		RoomHistory:   history,
		Interjections: interjections,
	})
	if err != nil {
		return fmt.Errorf("assemble context: %w", err)
	}

	// 5. Resolve model tier (primary for room RFCs)
	provider, modelID, err := d.registry.ResolveTier(ag.Definition, inference.TierPrimary)
	if err != nil {
		return fmt.Errorf("resolve model: %w", err)
	}

	// 6. Call inference (streaming)
	req := inference.InferRequest{
		Model:    modelID,
		Messages: assembled.Messages,
	}
	ch, err := provider.Infer(ctx, req)
	if err != nil {
		return fmt.Errorf("inference: %w", err)
	}

	// 7. Collect stream
	var content strings.Builder
	for chunk := range ch {
		content.WriteString(chunk.Content)
	}

	// 8. Build response message
	responseMsg := room.Message{
		ID:           uuid.New().String(),
		Timestamp:    time.Now().UTC(),
		RoomID:       rfc.RoomID,
		Sender:       room.Actor{ID: "agent:" + ag.Name(), Type: room.ActorAgent, Clearance: ag.Definition.Clearance, Name: ag.Definition.Name},
		ClearanceTag: ag.Definition.Clearance,
		Type:         room.MessageText,
		Content:      content.String(),
	}

	// 9. Write to transcript
	if err := d.appendToTranscript(ctx, rfc.RoomID, responseMsg); err != nil {
		return fmt.Errorf("append response: %w", err)
	}

	// 10. Update room updated_at
	if err := d.store.UpdateRoom(ctx, r); err != nil {
		log.Printf("dispatcher: warning: failed to update room %q: %v", r.ID, err)
	}

	// 11. Mark room as no longer processing
	d.mu.Lock()
	delete(d.processing, rfc.RoomID)
	d.mu.Unlock()

	// 12. Notify protocol
	proto, err := d.getProtocol(ctx, r)
	if err != nil {
		return fmt.Errorf("get protocol: %w", err)
	}
	if err := proto.OnRFCResponse(r, rfc, responseMsg); err != nil {
		return fmt.Errorf("protocol onRFCResponse: %w", err)
	}

	return nil
}

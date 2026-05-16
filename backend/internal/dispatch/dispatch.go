// Package dispatch provides the RFC dispatcher and per-agent goroutine lifecycle.
// The Dispatcher routes RFCs to agent goroutines, manages room protocols, and
// coordinates transcript I/O.
package dispatch

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

	// ctxConfig holds the context assembly configuration.
	ctxConfig config.ContextConfig

	// transcripts maps room IDs to their JSONL writers.
	transcripts map[string]*room.TranscriptWriter

	// protocols maps room IDs to their active protocol instances.
	protocols map[string]room.Protocol

	// processing tracks which room has an active RFC being processed.
	processing map[string]bool // room ID -> true

	// interjections queues messages that arrived while an agent was responding.
	interjections map[string][]room.Message // room ID -> queued messages

	// hub broadcasts real-time events to WebSocket clients. May be nil.
	hub Broadcaster
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

// Broadcaster sends real-time events to connected WebSocket clients.
type Broadcaster interface {
	Broadcast(roomID string, event StreamEvent)
}

// StreamEvent represents a real-time event broadcast to room subscribers.
type StreamEvent struct {
	Type    string `json:"type"`    // "typing" | "chunk" | "message" | "agent_error" | "interjection_queued"
	RoomID  string `json:"room_id"`
	Content string `json:"content,omitempty"`
	// Message contains the full message for "message" events.
	// It is omitted for other event types.
	Message *MessageEvent `json:"message,omitempty"`
}

// MessageEvent is a simplified message representation for WebSocket broadcasts.
type MessageEvent struct {
	ID           string `json:"id"`
	Timestamp    string `json:"timestamp"`
	RoomID       string `json:"room_id"`
	SenderID     string `json:"sender_id"`
	SenderName   string `json:"sender_name"`
	SenderType   string `json:"sender_type"`
	ClearanceTag int    `json:"clearance_tag"`
	Type         string `json:"type"`
	Content      string `json:"content"`
}

// NewDispatcher creates a new dispatcher with the given dependencies.
func NewDispatcher(
	mgr *agent.Manager,
	reg ModelResolver,
	asm *memory.Assembler,
	store room.Store,
	p *paths.Paths,
	ctxCfg config.ContextConfig,
) *Dispatcher {
	return &Dispatcher{
		agents:        make(map[string]*agentEntry),
		manager:       mgr,
		registry:      reg,
		assembler:     asm,
		store:         store,
		paths:         p,
		ctxConfig:     ctxCfg,
		transcripts:   make(map[string]*room.TranscriptWriter),
		protocols:     make(map[string]room.Protocol),
		processing:    make(map[string]bool),
		interjections: make(map[string][]room.Message),
	}
}

// SetBroadcaster sets the real-time event broadcaster. Must be called before
// any RFCs are processed if WebSocket streaming is desired.
func (d *Dispatcher) SetBroadcaster(hub Broadcaster) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hub = hub
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

		// Broadcast that an interjection was queued
		if d.hub != nil {
			d.hub.Broadcast(roomID, StreamEvent{
				Type:   "interjection_queued",
				RoomID: roomID,
			})
		}
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

// loadCrossRoomFeed reads recent messages from all other rooms the agent
// participates in, excluding the current room.
func (d *Dispatcher) loadCrossRoomFeed(ctx context.Context, ag *agent.Agent, currentRoomID string) ([]memory.CrossRoomMessage, error) {
	// Find all rooms where this agent is a participant
	rooms, err := d.store.ListRooms(ctx, room.ListOpts{
		Participant: "agent:" + ag.Name(),
		Limit:       200,
	})
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}

	var feed []memory.CrossRoomMessage
	for _, r := range rooms {
		if r.ID == currentRoomID {
			continue
		}

		// Get compaction cursor for this room
		cursor, err := d.store.GetCompactionCursor(ctx, ag.Name(), r.ID)
		if err != nil {
			return nil, fmt.Errorf("get cursor for room %s: %w", r.ID, err)
		}

		// Read tail of this room
		msgs, err := room.ReadMessagesTail(d.paths.RoomsDir(), r.ID, cursor.Offset, d.ctxConfig.OtherRoomWindow)
		if err != nil {
			return nil, fmt.Errorf("read tail for room %s: %w", r.ID, err)
		}

		// Filter by clearance and add to feed
		for _, m := range msgs {
			if m.ClearanceTag > ag.Definition.Clearance {
				continue
			}
			feed = append(feed, memory.CrossRoomMessage{
				Message: m,
				RoomID:  r.ID,
			})
		}
	}

	return feed, nil
}

// maybeCompact checks if compaction is needed and performs it if so.
// Compaction failure is non-fatal; the real RFC proceeds regardless.
func (d *Dispatcher) maybeCompact(ctx context.Context, ag *agent.Agent, roomID string, cursor *room.CompactionCursor, assembled *memory.AssembledContext) error {
	// Estimate assembled context size
	var assembledSize int
	for _, m := range assembled.Messages {
		assembledSize += len(m.Content)
	}

	// Check if compaction is needed
	if assembledSize < d.ctxConfig.CompactionTrigger {
		return nil
	}

	// Get total message count
	totalCount, err := room.TotalLineCount(d.paths.RoomsDir(), roomID)
	if err != nil {
		return fmt.Errorf("count transcript lines: %w", err)
	}

	// Check if there's enough to compact outside the guaranteed window
	guaranteed := d.ctxConfig.MinimumGuaranteed
	minBatch := d.ctxConfig.MinimumGuaranteed // Using minimum_guaranteed as min_batch floor
	available := totalCount - cursor.Offset - guaranteed
	if available < minBatch {
		log.Printf("dispatcher: compaction skipped for room %s, not enough messages outside guaranteed window (%d < %d)", roomID, available, minBatch)
		return nil
	}

	// Calculate batch size: how many messages to compact to reach target
	// Walk forward from cursor, accumulating bytes until we'd drop enough
	bytesToRemove := assembledSize - d.ctxConfig.CompactionTarget
	batchSize, accumulatedBytes, err := d.calculateCompactionBatch(roomID, cursor.Offset, bytesToRemove, available)
	if err != nil {
		return fmt.Errorf("calculate compaction batch: %w", err)
	}

	if batchSize <= 0 {
		log.Printf("dispatcher: compaction skipped for room %s, batch size <= 0", roomID)
		return nil
	}

	log.Printf("dispatcher: compacting %d messages (%d bytes) from room %s for agent %s", batchSize, accumulatedBytes, roomID, ag.Name())

	// Build compaction context: just the batch to summarize
	batchMessages, err := room.ReadMessagesFromOffset(d.paths.RoomsDir(), roomID, cursor.Offset, batchSize)
	if err != nil {
		return fmt.Errorf("read compaction batch: %w", err)
	}

	// Build compaction request
	var batchContent strings.Builder
	batchContent.WriteString("## Compaction Request\n\n")
	batchContent.WriteString("Summarize the following conversation messages into a concise summary. Write this summary to your daily note.\n\n")
	for _, m := range batchMessages {
		batchContent.WriteString(fmt.Sprintf("%s: %s\n", m.Sender.Name, m.Content))
	}

	// Call model for compaction (using routine tier to save cost)
	provider, modelID, err := d.registry.ResolveTier(ag.Definition, inference.TierRoutine)
	if err != nil {
		return fmt.Errorf("resolve routine model for compaction: %w", err)
	}

	compactionReq := inference.InferRequest{
		Model: modelID,
		Messages: []inference.Message{
			{Role: inference.RoleSystem, Content: ag.Definition.RoleDescription},
			{Role: inference.RoleUser, Content: batchContent.String()},
		},
	}

	ch, err := provider.Infer(ctx, compactionReq)
	if err != nil {
		return fmt.Errorf("compaction inference: %w", err)
	}

	var summary strings.Builder
	streamComplete := false
	for chunk := range ch {
		if chunk.FinishReason != "" {
			streamComplete = true
		}
		summary.WriteString(chunk.Content)
	}
	if !streamComplete {
		return fmt.Errorf("compaction stream ended unexpectedly")
	}

	// Write summary to daily note
	if ag.Definition.FeatureFlags.DailyNotes {
		agentDir := d.paths.AgentDataDir(ag.Name())
		memoryDir := filepath.Join(agentDir, "memory")
		if err := os.MkdirAll(memoryDir, 0o755); err != nil {
			return fmt.Errorf("mkdir memory: %w", err)
		}
		todayFile := filepath.Join(memoryDir, time.Now().UTC().Format("2006-01-02")+".md")
		if err := func() error {
			f, err := os.OpenFile(todayFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("open daily note: %w", err)
			}
			defer f.Close()

			header := fmt.Sprintf("\n\n## Compacted summary from room %s (%s)\n\n", roomID, time.Now().UTC().Format("2006-01-02 15:04"))
			if _, err := f.WriteString(header); err != nil {
				return fmt.Errorf("write compaction header: %w", err)
			}
			if _, err := f.WriteString(summary.String()); err != nil {
				return fmt.Errorf("write compaction summary: %w", err)
			}
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("write newline: %w", err)
			}
			return nil
		}(); err != nil {
			return err
		}
	}

	// Advance cursor
	newCursor := &room.CompactionCursor{
		AgentName: ag.Name(),
		RoomID:    roomID,
		Offset:    cursor.Offset + batchSize,
	}
	if err := d.store.SetCompactionCursor(ctx, newCursor); err != nil {
		return fmt.Errorf("advance compaction cursor: %w", err)
	}

	log.Printf("dispatcher: compaction complete for room %s, cursor advanced to %d", roomID, newCursor.Offset)
	return nil
}

// calculateCompactionBatch reads forward from the current cursor offset and
// returns how many messages should be compacted to remove at least bytesToRemove.
// It never returns more than maxAvailable.
func (d *Dispatcher) calculateCompactionBatch(roomID string, cursorOffset, bytesToRemove, maxAvailable int) (int, int, error) {
	if maxAvailable <= 0 || bytesToRemove <= 0 {
		return 0, 0, nil
	}

	// Read messages from cursor offset
	msgs, err := room.ReadMessagesFromOffset(d.paths.RoomsDir(), roomID, cursorOffset, maxAvailable)
	if err != nil {
		return 0, 0, err
	}

	accumulated := 0
	batchSize := 0
	for _, m := range msgs {
		accumulated += len(m.Content)
		batchSize++
		if accumulated >= bytesToRemove {
			break
		}
	}

	return batchSize, accumulated, nil
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
	log.Printf("dispatcher: agent loop started for %q", entry.agent.Name())

	defer close(entry.done)

	for rfc := range entry.queue {
		if err := d.processRFC(context.Background(), entry.agent, rfc); err != nil {
			log.Printf("dispatcher: error processing RFC %s for agent %q: %v", rfc.ID, entry.agent.Name(), err)
		}
	}
}

// processRFC assembles context, invokes the model, and delivers the response.
func (d *Dispatcher) processRFC(ctx context.Context, ag *agent.Agent, rfc room.RFC) error {
	delete(d.processing, rfc.RoomID) // ensure processing state is cleared on exit

	log.Printf("dispatcher: begin processing RFC %s for agent %q in room %q", rfc.ID, ag.Name(), rfc.RoomID)

	// 1. Get room
	r, err := d.store.GetRoom(ctx, rfc.RoomID)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}

	log.Printf("dispatcher: fetched room %q for RFC %s; participants=%d", r.ID, rfc.ID, len(r.Participants))

	// 2. Collect any pending interjections for this room
	d.mu.Lock()
	interjections := d.interjections[rfc.RoomID]
	d.interjections[rfc.RoomID] = nil
	d.mu.Unlock()

	// 3. Read current room history (windowed tail from compacted_offset)
	cursor, err := d.store.GetCompactionCursor(ctx, ag.Name(), rfc.RoomID)
	if err != nil {
		return fmt.Errorf("get compaction cursor: %w", err)
	}

	currentHistory, err := room.ReadMessagesTail(d.paths.RoomsDir(), rfc.RoomID, cursor.Offset, d.ctxConfig.CurrentRoomWindow)
	if err != nil {
		return fmt.Errorf("read current room tail: %w", err)
	}

	// 4. Load cross-room feed from other rooms this agent participates in
	crossRoomFeed, err := d.loadCrossRoomFeed(ctx, ag, rfc.RoomID)
	if err != nil {
		return fmt.Errorf("load cross-room feed: %w", err)
	}

	// 5. Assemble context
	assembled, err := d.assembler.Assemble(ctx, ag, memory.AssembleRequest{
		RoomID:             rfc.RoomID,
		CrossRoomFeed:      crossRoomFeed,
		CurrentRoomHistory: currentHistory,
		Interjections:      interjections,
	})
	if err != nil {
		return fmt.Errorf("assemble context: %w", err)
	}

	log.Printf("dispatcher: assembled context with %d messages for RFC %s in room %q", len(assembled.Messages), rfc.ID, r.ID)

	// 6. Check if compaction is needed before processing the real RFC
	if err := d.maybeCompact(ctx, ag, rfc.RoomID, cursor, assembled); err != nil {
		log.Printf("dispatcher: compaction failed for RFC %s, proceeding anyway: %v", rfc.ID, err)
	}
	// Re-read current room history after compaction in case cursor advanced
	cursor, err = d.store.GetCompactionCursor(ctx, ag.Name(), rfc.RoomID)
	if err != nil {
		return fmt.Errorf("get compaction cursor after compaction: %w", err)
	}
	currentHistory, err = room.ReadMessagesTail(d.paths.RoomsDir(), rfc.RoomID, cursor.Offset, d.ctxConfig.CurrentRoomWindow)
	if err != nil {
		return fmt.Errorf("re-read current room tail: %w", err)
	}
	// Re-assemble with potentially compacted context
	assembled, err = d.assembler.Assemble(ctx, ag, memory.AssembleRequest{
		RoomID:             rfc.RoomID,
		CrossRoomFeed:      crossRoomFeed,
		CurrentRoomHistory: currentHistory,
		Interjections:      interjections,
	})
	if err != nil {
		return fmt.Errorf("reassemble context: %w", err)
	}

	// 5. Resolve model tier (primary for room RFCs)
	provider, modelID, err := d.registry.ResolveTier(ag.Definition, inference.TierPrimary)
	if err != nil {
		d.handleInferenceError(ctx, rfc, err, "")
		return fmt.Errorf("resolve model: %w", err)
	}

	// 6. Call inference (streaming)
	req := inference.InferRequest{
		Model:    modelID,
		Messages: assembled.Messages,
	}

	// Broadcast typing indicator
	if d.hub != nil {
		d.hub.Broadcast(rfc.RoomID, StreamEvent{
			Type:   "typing",
			RoomID: rfc.RoomID,
		})
	}

	log.Printf("dispatcher: starting inference for RFC %s with model %q for agent %q in room %q", rfc.ID, modelID, ag.Name(), r.ID)

	ch, err := provider.Infer(ctx, req)
	if err != nil {
		d.handleInferenceError(ctx, rfc, err, "")
		return fmt.Errorf("inference: %w", err)
	}

	// 7. Collect stream with real-time broadcasting
	var content strings.Builder
	streamComplete := false
	for chunk := range ch {
		if chunk.FinishReason != "" {
			streamComplete = true
		}
		content.WriteString(chunk.Content)
		if d.hub != nil {
			d.hub.Broadcast(rfc.RoomID, StreamEvent{
				Type:    "chunk",
				RoomID:  rfc.RoomID,
				Content: chunk.Content,
			})
		}
	}

	log.Printf("dispatcher: inference stream ended for RFC %s in room %q; complete=%v", rfc.ID, r.ID, streamComplete)

	// If the stream ended without a finish reason, it was an error
	if !streamComplete {
		partial := content.String()
		d.handleInferenceError(ctx, rfc, fmt.Errorf("inference stream ended unexpectedly"), partial)
		return fmt.Errorf("inference stream ended unexpectedly")
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

	// 12. Broadcast complete message
	if d.hub != nil {
		d.hub.Broadcast(rfc.RoomID, StreamEvent{
			Type:   "message",
			RoomID: rfc.RoomID,
			Message: &MessageEvent{
				ID:           responseMsg.ID,
				Timestamp:    responseMsg.Timestamp.Format(time.RFC3339),
				RoomID:       responseMsg.RoomID,
				SenderID:     responseMsg.Sender.ID,
				SenderName:   responseMsg.Sender.Name,
				SenderType:   string(responseMsg.Sender.Type),
				ClearanceTag: responseMsg.ClearanceTag,
				Type:         string(responseMsg.Type),
				Content:      responseMsg.Content,
			},
		})
	}

	// 13. Notify protocol
	proto, err := d.getProtocol(ctx, r)
	if err != nil {
		return fmt.Errorf("get protocol: %w", err)
	}
	if err := proto.OnRFCResponse(r, rfc, responseMsg); err != nil {
		return fmt.Errorf("protocol onRFCResponse: %w", err)
	}

	log.Printf("dispatcher: successfully processed RFC %s for agent %q in room %q", rfc.ID, ag.Name(), r.ID)

	return nil
}

// handleInferenceError writes a system error message to the transcript,
// broadcasts an error event, and cleans up the processing state.
func (d *Dispatcher) handleInferenceError(ctx context.Context, rfc room.RFC, err error, partialContent string) {
	log.Printf("dispatcher: inference error for RFC %s: %v", rfc.ID, err)

	// Write system error message to transcript
	errorMsg := room.Message{
		ID:        uuid.New().String(),
		Timestamp: time.Now().UTC(),
		RoomID:    rfc.RoomID,
		Sender:    room.Actor{ID: "system", Type: room.ActorUser, Clearance: 0, Name: "System"},
		ClearanceTag: 0,
		Type:         room.MessageSystem,
		Content:      fmt.Sprintf("Agent error: %v", err),
	}
	if err := d.appendToTranscript(ctx, rfc.RoomID, errorMsg); err != nil {
		log.Printf("dispatcher: failed to append error message: %v", err)
	}

	// Broadcast error event
	if d.hub != nil {
		d.hub.Broadcast(rfc.RoomID, StreamEvent{
			Type:    "agent_error",
			RoomID:  rfc.RoomID,
			Content: err.Error(),
		})
	}

	// Notify protocol (if possible)
	// NOTE: processing[roomID] is intentionally NOT cleared here.
	// The caller (processRFC) is responsible for cleanup at step 11 so that
	// the room is not prematurely marked idle mid-flight, which would allow
	// interjections to bypass the queue.
	if r, err := d.store.GetRoom(ctx, rfc.RoomID); err == nil {
		if proto, err := d.getProtocol(ctx, r); err == nil {
			proto.OnRFCResponse(r, rfc, errorMsg)
		}
	}

	_ = partialContent // reserved for future use (e.g. partial response in error payload)
}

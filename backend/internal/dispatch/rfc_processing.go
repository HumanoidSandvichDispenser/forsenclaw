package dispatch

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// processRFC assembles context, invokes the model, and delivers the response.
func (d *Dispatcher) processRFC(ctx context.Context, ag *agent.Agent, rfc room.RFC) error {
	// Ensure processing state is always cleared, even on early return.
	defer func() {
		d.mu.Lock()
		delete(d.processing, rfc.RoomID)
		d.mu.Unlock()
	}()

	log.Printf("dispatcher: begin processing RFC %s for agent %q in room %q", rfc.ID, ag.Name(), rfc.RoomID)

	r, err := d.store.GetRoom(ctx, rfc.RoomID)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}

	interjections := d.collectInterjections(rfc.RoomID)

	assembled, cursor, err := d.assembleContext(ctx, ag, rfc, interjections)
	if err != nil {
		return err
	}

	assembled, err = d.compactAndReassembleIfNeeded(ctx, ag, rfc, cursor, assembled, interjections)
	if err != nil {
		log.Printf("dispatcher: compaction failed for RFC %s, proceeding anyway: %v", rfc.ID, err)
	}

	provider, modelID, err := d.registry.ResolveTier(ag.Definition, inference.TierPrimary)
	if err != nil {
		d.handleInferenceError(ctx, rfc, err, "")
		return fmt.Errorf("resolve model: %w", err)
	}

	log.Printf("dispatcher: starting inference for RFC %s with model %q for agent %q in room %q", rfc.ID, modelID, ag.Name(), r.ID)

	executor := d.buildExecutor(ag)
	content, usage, err := d.runToolLoop(ctx, ag, rfc, provider, modelID, assembled, executor)
	if err != nil {
		return err
	}

	if content == "" {
		log.Printf("dispatcher: tool loop returned empty prose for RFC %s, skipping response delivery", rfc.ID)
		return nil
	}

	responseMsg := d.buildResponseMessage(ag, rfc, content, usage)

	if err := d.deliverResponse(ctx, rfc, r, responseMsg); err != nil {
		return err
	}

	if err := d.notifyProtocol(ctx, r, rfc, responseMsg); err != nil {
		return err
	}

	log.Printf("dispatcher: successfully processed RFC %s for agent %q in room %q", rfc.ID, ag.Name(), r.ID)
	return nil
}

// collectInterjections drains and returns any queued interjections for the room.
func (d *Dispatcher) collectInterjections(roomID string) []room.Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	interjections := d.interjections[roomID]
	d.interjections[roomID] = nil
	return interjections
}

// assembleContext reads room history and cross-room feed, then assembles the
// full context window for the RFC.
func (d *Dispatcher) assembleContext(ctx context.Context, ag *agent.Agent, rfc room.RFC, interjections []room.Message) (*memory.AssembledContext, *room.CompactionCursor, error) {
	cursor, err := d.store.GetCompactionCursor(ctx, ag.Name(), rfc.RoomID)
	if err != nil {
		return nil, nil, fmt.Errorf("get compaction cursor: %w", err)
	}

	currentHistory, err := room.ReadMessagesTail(d.paths.RoomsDir(), rfc.RoomID, cursor.Offset, d.ctxConfig.CurrentRoomWindow)
	if err != nil {
		return nil, nil, fmt.Errorf("read current room tail: %w", err)
	}

	crossRoomFeed, err := d.loadCrossRoomFeed(ctx, ag, rfc.RoomID)
	if err != nil {
		return nil, nil, fmt.Errorf("load cross-room feed: %w", err)
	}

	assembled, err := d.assembler.Assemble(ctx, ag, memory.AssembleRequest{
		RoomID:             rfc.RoomID,
		CrossRoomFeed:      crossRoomFeed,
		CurrentRoomHistory: currentHistory,
		Interjections:      interjections,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("assemble context: %w", err)
	}

	log.Printf("dispatcher: assembled context for RFC %s", rfc.ID)
	return assembled, cursor, nil
}

// broadcastAndCollect calls inference with the given payload, streams chunks to
// connected clients via the WebSocket hub, and returns the complete response
// content along with the final StreamingChunk (which carries usage and native
// tool calls). On inference failure, handleInferenceError is called before
// returning. The typing indicator is NOT broadcast here; callers are responsible
// for it.
func (d *Dispatcher) broadcastAndCollect(ctx context.Context, rfc room.RFC, provider inference.Provider, payload inference.ContextPayload) (string, inference.StreamingChunk, error) {
	ch, err := provider.Infer(ctx, payload)
	if err != nil {
		d.handleInferenceError(ctx, rfc, err, "")
		return "", inference.StreamingChunk{}, fmt.Errorf("inference: %w", err)
	}

	var content strings.Builder
	var finalChunk inference.StreamingChunk
	streamComplete := false
	for chunk := range ch {
		if chunk.FinishReason != "" {
			streamComplete = true
			finalChunk = chunk
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

	if !streamComplete {
		partial := content.String()
		d.handleInferenceError(ctx, rfc, fmt.Errorf("inference stream ended unexpectedly"), partial)
		return "", inference.StreamingChunk{}, fmt.Errorf("inference stream ended unexpectedly")
	}

	return content.String(), finalChunk, nil
}

// buildResponseMessage constructs the agent's response as a room.Message.
func (d *Dispatcher) buildResponseMessage(ag *agent.Agent, rfc room.RFC, content string, usage inference.Usage) room.Message {
	msgUsage := room.Usage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	return room.Message{
		ID:           uuid.New().String(),
		Timestamp:    time.Now().UTC(),
		RoomID:       rfc.RoomID,
		Sender:       room.Actor{ID: "agent:" + ag.Name(), Type: room.ActorAgent, Clearance: ag.Definition.Clearance, Name: ag.Definition.Name},
		ClearanceTag: ag.Definition.Clearance,
		Type:         room.MessageText,
		Content:      content,
		Usage:        &msgUsage,
	}
}

// deliverResponse writes the response to the transcript, updates the room,
// and broadcasts the complete message event to connected clients.
func (d *Dispatcher) deliverResponse(ctx context.Context, rfc room.RFC, r *room.Room, msg room.Message) error {
	if err := d.appendToTranscript(ctx, rfc.RoomID, msg); err != nil {
		return fmt.Errorf("append response: %w", err)
	}

	if err := d.store.UpdateRoom(ctx, r); err != nil {
		log.Printf("dispatcher: warning: failed to update room %q: %v", r.ID, err)
	}

	if d.hub != nil {
		d.hub.Broadcast(rfc.RoomID, StreamEvent{
			Type:   "message",
			RoomID: rfc.RoomID,
			Message: &MessageEvent{
				ID:           msg.ID,
				Timestamp:    msg.Timestamp.Format(time.RFC3339),
				RoomID:       msg.RoomID,
				SenderID:     msg.Sender.ID,
				SenderName:   msg.Sender.Name,
				SenderType:   string(msg.Sender.Type),
				ClearanceTag: msg.ClearanceTag,
				Type:         string(msg.Type),
				Content:      msg.Content,
				Usage:        msg.Usage,
			},
		})
	}

	return nil
}

// notifyProtocol calls OnRFCResponse on the room's active protocol.
func (d *Dispatcher) notifyProtocol(ctx context.Context, r *room.Room, rfc room.RFC, responseMsg room.Message) error {
	proto, err := d.getProtocol(ctx, r)
	if err != nil {
		return fmt.Errorf("get protocol: %w", err)
	}
	if err := proto.OnRFCResponse(r, rfc, responseMsg); err != nil {
		return fmt.Errorf("protocol onRFCResponse: %w", err)
	}
	return nil
}

// handleInferenceError writes a system error message to the transcript and
// broadcasts an error event. Processing state cleanup is handled by the
// defer in processRFC — do not clear it here.
func (d *Dispatcher) handleInferenceError(ctx context.Context, rfc room.RFC, inferErr error, partialContent string) {
	log.Printf("dispatcher: inference error for RFC %s: %v", rfc.ID, inferErr)

	errorMsg := room.Message{
		ID:           uuid.New().String(),
		Timestamp:    time.Now().UTC(),
		RoomID:       rfc.RoomID,
		Sender:       room.Actor{ID: "system", Type: room.ActorSystem, Clearance: 0, Name: "System"},
		ClearanceTag: 0,
		Type:         room.MessageSystem,
		Content:      fmt.Sprintf("Agent error: %v", inferErr),
	}
	if err := d.appendToTranscript(ctx, rfc.RoomID, errorMsg); err != nil {
		log.Printf("dispatcher: failed to append error message: %v", err)
	}

	if d.hub != nil {
		d.hub.Broadcast(rfc.RoomID, StreamEvent{
			Type:    "agent_error",
			RoomID:  rfc.RoomID,
			Content: inferErr.Error(),
		})
	}

	if r, err := d.store.GetRoom(ctx, rfc.RoomID); err == nil {
		if proto, err := d.getProtocol(ctx, r); err == nil {
			proto.OnRFCResponse(r, rfc, errorMsg)
		}
	}

	_ = partialContent // reserved: partial response in error payload
}

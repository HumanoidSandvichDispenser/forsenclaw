// Package dispatch provides the RFC dispatcher and per-agent goroutine lifecycle.
// The Dispatcher routes RFCs to agent goroutines, manages room protocols, and
// coordinates transcript I/O.
package dispatch

import (
	"sync"

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

package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/audit"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

const createRoomToolID = "create_room"

// RoomCreator persists a new room. Satisfied by *store.SQLiteStore.
type RoomCreator interface {
	CreateRoom(ctx context.Context, r *room.Room) error
}

// MessageAppender appends a message to a room's conversation tree. Satisfied by
// *store.SQLiteStore.
type MessageAppender interface {
	AppendMessage(ctx context.Context, roomID int64, msg room.Message) (int64, error)
}

// ActorResolver maps an actor ID ("agent:<name>" / "user:<name>") to the
// room.Actor it denotes, supplying the clearance that actor operates at. It is
// implemented by the wiring layer, which has access to the agent manager.
type ActorResolver interface {
	ResolveActor(id string) (room.Actor, error)
}

// CreateRoomClient is a built-in tool that lets an agent spin up a new room at a
// chosen clearance ceiling and seed it with a curated context summary. Creating
// a room below the caller's effective clearance is a Bell-LaPadula write-down:
// the policy engine evaluates the requested ceiling (via DynamicClearance) and
// gates it behind a confirmation before this tool ever runs. By the time Call
// executes, the write-down is authorized — so the rule is NOT re-checked here.
//
// The new room is a clean cut: the source room's history is never copied. Only
// the agent's hand-written context_summary crosses, tagged with provenance.
type CreateRoomClient struct {
	rooms        RoomCreator
	messages     MessageAppender
	user         room.Actor
	resolver     ActorResolver
	resolveLevel func(string) (int, bool)
}

// NewCreateRoom builds the create_room tool. user is the configured user, always
// added as a participant; resolver may be set later via SetResolver (the agent
// manager that backs it is constructed after the tool registry).
func NewCreateRoom(rooms RoomCreator, messages MessageAppender, user room.Actor) *CreateRoomClient {
	return &CreateRoomClient{rooms: rooms, messages: messages, user: user}
}

// SetResolver injects the actor resolver. Separated from construction to break
// the wiring cycle: the resolver is backed by the agent manager, which depends
// (transitively) on the tool registry this client lives in.
func (c *CreateRoomClient) SetResolver(r ActorResolver) { c.resolver = r }

// SetLevelResolver injects a resolver for named clearance levels (e.g.
// "confidential" → 2). When unset, clearance_ceiling must be an integer string.
func (c *CreateRoomClient) SetLevelResolver(f func(string) (int, bool)) { c.resolveLevel = f }

// parseCeiling resolves a clearance_ceiling argument, accepting a named level
// when a level resolver is configured and falling back to integer parsing.
func (c *CreateRoomClient) parseCeiling(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if c.resolveLevel != nil {
		if n, ok := c.resolveLevel(raw); ok {
			return n, true
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (c *CreateRoomClient) ToolIDs() []string { return []string{createRoomToolID} }
func (c *CreateRoomClient) Healthy() bool     { return true }

// ResourceClearance reports the requested clearance ceiling so the policy engine
// can evaluate the write-down against the caller's effective clearance. ok is
// false when no parseable ceiling is present, leaving the static tool clearance
// in effect. Satisfies mcp.DynamicClearance.
func (c *CreateRoomClient) ResourceClearance(params map[string]string) (int, bool) {
	return c.parseCeiling(params["clearance_ceiling"])
}

func (c *CreateRoomClient) Call(ctx context.Context, toolID string, params map[string]string) (string, error) {
	if toolID != createRoomToolID {
		return "", fmt.Errorf("unsupported tool %q", toolID)
	}
	if c.resolver == nil {
		return "", fmt.Errorf("create_room is not fully initialised (no actor resolver)")
	}

	name := strings.TrimSpace(params["name"])
	if name == "" {
		return "", fmt.Errorf("missing required parameter %q", "name")
	}
	summary := strings.TrimSpace(params["context_summary"])
	if summary == "" {
		return "", fmt.Errorf("missing required parameter %q", "context_summary")
	}
	ceiling, ok := c.parseCeiling(params["clearance_ceiling"])
	if !ok {
		return "", fmt.Errorf("invalid clearance_ceiling %q: not a known level or integer", params["clearance_ceiling"])
	}
	if ceiling < 0 {
		return "", fmt.Errorf("clearance_ceiling must be non-negative, got %d", ceiling)
	}

	// Identify the seeding agent from context (stamped by the inference handler).
	agentName := audit.AgentIDFromContext(ctx)
	if agentName == "" {
		return "", fmt.Errorf("no calling agent in context")
	}
	seeder, err := c.resolver.ResolveActor("agent:" + agentName)
	if err != nil {
		return "", fmt.Errorf("resolve calling agent %q: %w", agentName, err)
	}

	participants, err := c.buildParticipants(seeder, params["participants"])
	if err != nil {
		return "", err
	}

	r := room.Room{
		Name:         name,
		Participants: participants,
		Clearance:    ceiling,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := c.rooms.CreateRoom(ctx, &r); err != nil {
		return "", fmt.Errorf("create room: %w", err)
	}

	// Seed the room with the curated summary, tagged with provenance so readers
	// know it was deliberately declassified into this lower-clearance room.
	seed := room.Message{
		RoomID:       r.ID,
		Sender:       seeder,
		ClearanceTag: ceiling,
		Type:         room.MessageText,
		Content: fmt.Sprintf(
			"[Declassified into clearance-%d room by agent %s]\n\n%s",
			ceiling, seeder.Name, summary,
		),
		Timestamp: time.Now().UTC(),
	}
	if _, err := c.messages.AppendMessage(ctx, r.ID, seed); err != nil {
		return "", fmt.Errorf("seed room: %w", err)
	}

	return fmt.Sprintf(
		"Created room %q (id=%d) at clearance %d with %d participant(s).",
		name, r.ID, ceiling, len(participants),
	), nil
}

// buildParticipants assembles the participant list: the seeding agent (joins by
// default), the configured user, and any explicitly named participants from the
// comma-separated raw list. Deduplicated by actor ID.
func (c *CreateRoomClient) buildParticipants(seeder room.Actor, raw string) ([]room.Actor, error) {
	seen := make(map[string]bool)
	var out []room.Actor
	add := func(a room.Actor) {
		if a.ID == "" || seen[a.ID] {
			return
		}
		seen[a.ID] = true
		out = append(out, a)
	}

	add(seeder)
	add(c.user)

	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		actor, err := c.resolver.ResolveActor(id)
		if err != nil {
			return nil, fmt.Errorf("resolve participant %q: %w", id, err)
		}
		add(actor)
	}
	return out, nil
}

func (c *CreateRoomClient) NativeDefinitions() []inference.ToolDefinition {
	return []inference.ToolDefinition{
		{
			Name: createRoomToolID,
			Description: "Create a new room at a chosen clearance ceiling and seed it with a " +
				"curated context summary. Use this to deliberately move a distilled subset of " +
				"the current context into a lower-clearance room (declassification): only the " +
				"summary you write crosses over — the current room's history does not. Creating " +
				"a room below your effective clearance requires user confirmation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Human-readable name for the new room.",
					},
					"clearance_ceiling": map[string]interface{}{
						"type":        "string",
						"description": "Clearance ceiling for the new room, as an integer (e.g. \"2\"). Must not exceed your effective clearance.",
					},
					"context_summary": map[string]interface{}{
						"type":        "string",
						"description": "The curated context to seed the room with. This is the only content that crosses into the new room.",
					},
					"participants": map[string]interface{}{
						"type":        "string",
						"description": "Optional comma-separated actor IDs to add (e.g. \"agent:researcher,user:alice\"). You and the user are always included.",
					},
				},
				"required": []string{"name", "clearance_ceiling", "context_summary"},
			},
		},
	}
}

func (c *CreateRoomClient) XMLSchemas() []string { return nil }

var _ mcp.MCPClient = (*CreateRoomClient)(nil)
var _ mcp.ToolDescriber = (*CreateRoomClient)(nil)
var _ mcp.DynamicClearance = (*CreateRoomClient)(nil)

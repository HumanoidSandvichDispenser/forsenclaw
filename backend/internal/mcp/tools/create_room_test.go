package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/audit"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// --- fakes ---

type fakeRoomCreator struct {
	created *room.Room
	nextID  int64
	err     error
}

func (f *fakeRoomCreator) CreateRoom(_ context.Context, r *room.Room) error {
	if f.err != nil {
		return f.err
	}
	if f.nextID == 0 {
		f.nextID = 42
	}
	r.ID = f.nextID
	f.created = r
	return nil
}

type fakeMessageAppender struct {
	roomID int64
	msg    room.Message
	err    error
}

func (f *fakeMessageAppender) AppendMessage(_ context.Context, roomID int64, msg room.Message) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.roomID = roomID
	f.msg = msg
	return 1, nil
}

// fakeResolver resolves any agent:/user: ID to an actor with a fixed clearance,
// keyed by name, falling back to a default clearance.
type fakeResolver struct {
	clearances map[string]int
}

func (f *fakeResolver) ResolveActor(id string) (room.Actor, error) {
	var typ room.ActorType
	var name string
	switch {
	case strings.HasPrefix(id, "agent:"):
		typ, name = room.ActorAgent, id[len("agent:"):]
	case strings.HasPrefix(id, "user:"):
		typ, name = room.ActorUser, id[len("user:"):]
	default:
		return room.Actor{}, errResolve(id)
	}
	cl := 1
	if f.clearances != nil {
		if c, ok := f.clearances[name]; ok {
			cl = c
		}
	}
	return room.Actor{ID: id, Type: typ, Name: name, Clearance: cl}, nil
}

type errResolve string

func (e errResolve) Error() string { return "cannot resolve " + string(e) }

func newTestTool() (*CreateRoomClient, *fakeRoomCreator, *fakeMessageAppender) {
	rooms := &fakeRoomCreator{}
	msgs := &fakeMessageAppender{}
	user := room.Actor{ID: "user:alice", Type: room.ActorUser, Name: "alice", Clearance: 5}
	tool := NewCreateRoom(rooms, msgs, user)
	tool.SetResolver(&fakeResolver{clearances: map[string]int{
		"seeder": 4, "researcher": 3, "alice": 5,
	}})
	return tool, rooms, msgs
}

func callWith(name string) context.Context {
	return audit.WithAgentID(context.Background(), name)
}

// --- tests ---

func TestCreateRoom_HappyPath(t *testing.T) {
	tool, rooms, msgs := newTestTool()

	out, err := tool.Call(callWith("seeder"), "create_room", map[string]string{
		"name":              "low room",
		"clearance_ceiling": "2",
		"context_summary":   "the gist",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if rooms.created == nil {
		t.Fatal("expected a room to be created")
	}
	if rooms.created.Clearance != 2 {
		t.Errorf("room clearance = %d, want 2", rooms.created.Clearance)
	}
	if rooms.created.Name != "low room" {
		t.Errorf("room name = %q, want %q", rooms.created.Name, "low room")
	}

	// Seeding agent + user, both present, deduplicated.
	ids := participantIDs(rooms.created.Participants)
	if len(ids) != 2 {
		t.Fatalf("participants = %v, want seeder + user", ids)
	}
	if !ids["agent:seeder"] || !ids["user:alice"] {
		t.Errorf("participants = %v, want agent:seeder and user:alice", ids)
	}

	// Seed message: tagged at the new (low) ceiling, provenance in content.
	if msgs.roomID != rooms.created.ID {
		t.Errorf("seed roomID = %d, want %d", msgs.roomID, rooms.created.ID)
	}
	if msgs.msg.ClearanceTag != 2 {
		t.Errorf("seed ClearanceTag = %d, want 2", msgs.msg.ClearanceTag)
	}
	if msgs.msg.Sender.ID != "agent:seeder" {
		t.Errorf("seed sender = %q, want agent:seeder", msgs.msg.Sender.ID)
	}
	if !strings.Contains(msgs.msg.Content, "Declassified into clearance-2 room by agent seeder") {
		t.Errorf("seed missing provenance tag: %q", msgs.msg.Content)
	}
	if !strings.Contains(msgs.msg.Content, "the gist") {
		t.Errorf("seed missing summary: %q", msgs.msg.Content)
	}
	if !strings.Contains(out, "id=42") {
		t.Errorf("output = %q, want room id", out)
	}
}

func TestCreateRoom_NamedParticipantsDeduped(t *testing.T) {
	tool, rooms, _ := newTestTool()

	_, err := tool.Call(callWith("seeder"), "create_room", map[string]string{
		"name":              "r",
		"clearance_ceiling": "1",
		"context_summary":   "s",
		// researcher added; seeder named again (dup); user named again (dup).
		"participants": "agent:researcher, agent:seeder ,user:alice",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	ids := participantIDs(rooms.created.Participants)
	if len(ids) != 3 {
		t.Fatalf("participants = %v, want 3 unique", ids)
	}
	for _, want := range []string{"agent:seeder", "user:alice", "agent:researcher"} {
		if !ids[want] {
			t.Errorf("missing participant %q in %v", want, ids)
		}
	}
}

func TestCreateRoom_MissingParams(t *testing.T) {
	cases := map[string]map[string]string{
		"no name":     {"clearance_ceiling": "1", "context_summary": "s"},
		"no summary":  {"name": "r", "clearance_ceiling": "1"},
		"no ceiling":  {"name": "r", "context_summary": "s"},
		"bad ceiling": {"name": "r", "clearance_ceiling": "x", "context_summary": "s"},
		"neg ceiling": {"name": "r", "clearance_ceiling": "-1", "context_summary": "s"},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			tool, rooms, _ := newTestTool()
			if _, err := tool.Call(callWith("seeder"), "create_room", params); err == nil {
				t.Fatal("expected error, got nil")
			}
			if rooms.created != nil {
				t.Error("no room should be created on validation failure")
			}
		})
	}
}

func TestCreateRoom_NoCallingAgent(t *testing.T) {
	tool, _, _ := newTestTool()
	_, err := tool.Call(context.Background(), "create_room", map[string]string{
		"name": "r", "clearance_ceiling": "1", "context_summary": "s",
	})
	if err == nil {
		t.Fatal("expected error when no calling agent in context")
	}
}

func TestCreateRoom_NoResolver(t *testing.T) {
	tool := NewCreateRoom(&fakeRoomCreator{}, &fakeMessageAppender{}, room.Actor{})
	_, err := tool.Call(callWith("seeder"), "create_room", map[string]string{
		"name": "r", "clearance_ceiling": "1", "context_summary": "s",
	})
	if err == nil {
		t.Fatal("expected error when resolver is unset")
	}
}

func TestCreateRoom_UnsupportedToolID(t *testing.T) {
	tool, _, _ := newTestTool()
	if _, err := tool.Call(callWith("seeder"), "nope", nil); err == nil {
		t.Fatal("expected error for unsupported tool id")
	}
}

func TestCreateRoom_ResourceClearance(t *testing.T) {
	tool, _, _ := newTestTool()
	if n, ok := tool.ResourceClearance(map[string]string{"clearance_ceiling": "3"}); !ok || n != 3 {
		t.Errorf("ResourceClearance = (%d,%v), want (3,true)", n, ok)
	}
	if _, ok := tool.ResourceClearance(map[string]string{"clearance_ceiling": "x"}); ok {
		t.Error("ResourceClearance should report ok=false for unparseable ceiling")
	}
	if _, ok := tool.ResourceClearance(map[string]string{}); ok {
		t.Error("ResourceClearance should report ok=false when ceiling absent")
	}
}

func participantIDs(actors []room.Actor) map[string]bool {
	out := make(map[string]bool, len(actors))
	for _, a := range actors {
		out[a.ID] = true
	}
	return out
}

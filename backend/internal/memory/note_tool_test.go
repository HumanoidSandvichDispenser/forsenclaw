package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/tool"
)

// TestNoteTool_WritesAtOperatingClearance verifies the note lands in the daily
// notes of the caller's operating-clearance directory and is readable back at
// that level but not below it.
func TestNoteTool_WritesAtOperatingClearance(t *testing.T) {
	_, _, p := newTestAssembler(t)
	nt := NewNoteTool(p)

	out, err := nt.Invoke(context.Background(), tool.Invocation{
		AgentName:          "housewife",
		OperatingClearance: 3,
	}, map[string]string{"content": "user prefers tea over coffee"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out == "" {
		t.Error("expected a confirmation message")
	}

	// Readable when operating at the level it was written.
	atLevel, err := ReadDailyNotes(p.AgentClearanceDir("housewife", 3), true)
	if err != nil {
		t.Fatalf("ReadDailyNotes c3: %v", err)
	}
	if len(atLevel) == 0 || !strings.Contains(atLevel[0].Content, "prefers tea") {
		t.Fatalf("note not found at clearance 3: %v", atLevel)
	}

	// Not present in a lower-clearance directory.
	below, err := ReadDailyNotes(p.AgentClearanceDir("housewife", 1), true)
	if err != nil {
		t.Fatalf("ReadDailyNotes c1: %v", err)
	}
	if len(below) != 0 {
		t.Errorf("note leaked into clearance 1: %v", below)
	}
}

// TestNoteTool_EmptyContent rejects a blank note.
func TestNoteTool_EmptyContent(t *testing.T) {
	_, _, p := newTestAssembler(t)
	nt := NewNoteTool(p)
	if _, err := nt.Invoke(context.Background(), tool.Invocation{AgentName: "housewife", OperatingClearance: 1}, map[string]string{"content": "   "}); err == nil {
		t.Error("expected error for empty note content")
	}
}

// TestCompactTool_ForcesCompaction drives compaction through the tool, checking
// the room guard, resolver wiring, and target parsing.
func TestCompactTool_ForcesCompaction(t *testing.T) {
	_, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(t, store, 5, alice)
	ctx := context.Background()
	body := strings.Repeat("x", 100)
	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := store.AppendMessage(ctx, r.ID, room.Message{
			Timestamp: time.Now(), RoomID: r.ID, Sender: alice,
			ClearanceTag: 5, Type: room.MessageText, Content: body,
		})
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		ids = append(ids, id)
	}

	ct := NewCompactTool(NewCompactor(p, store, store, nil, 1<<20, 1<<19))

	// Without a resolver the tool refuses rather than panicking.
	if _, err := ct.Invoke(ctx, tool.Invocation{AgentName: "housewife", RoomID: r.ID}, nil); err == nil {
		t.Error("expected error when resolver is unset")
	}
	// Without a room it refuses.
	ct.SetResolver(func(string) *agent.Agent { return ag })
	if _, err := ct.Invoke(ctx, tool.Invocation{AgentName: "housewife"}, nil); err == nil {
		t.Error("expected error without a room context")
	}

	if _, err := ct.Invoke(ctx, tool.Invocation{AgentName: "housewife", RoomID: r.ID}, map[string]string{"target": "150"}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	offset, err := store.GetCompactionOffset(ctx, "housewife", r.ID)
	if err != nil {
		t.Fatalf("GetCompactionOffset: %v", err)
	}
	if want := ids[3]; offset != want {
		t.Fatalf("offset = %d, want %d", offset, want)
	}
}

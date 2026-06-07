package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	storedb "github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
)

// TestCompactor_HardDrop verifies that with daily notes disabled, compaction
// advances the offset past the oldest messages until the post-offset transcript
// is back under target, always preserving the most recent message. No model is
// invoked on this path, so the registry is unused.
func TestCompactor_HardDrop(t *testing.T) {
	_, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5) // DailyNotes false

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

	// total = 500. trigger 250 fires; drop oldest until remaining <= 150.
	c := NewCompactor(p, store, store, nil, 250, 150)
	if err := c.MaybeCompact(ctx, ag, r.ID); err != nil {
		t.Fatalf("MaybeCompact: %v", err)
	}

	offset, err := store.GetCompactionOffset(ctx, "housewife", r.ID)
	if err != nil {
		t.Fatalf("GetCompactionOffset: %v", err)
	}
	// Dropping m1..m4 leaves 100 bytes (<=150); boundary is m4.
	if want := ids[3]; offset != want {
		t.Fatalf("offset = %d, want %d", offset, want)
	}

	remaining, err := store.GetMessages(ctx, r.ID, storedb.ReadOpts{CompactionID: offset})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != ids[4] {
		t.Fatalf("post-compaction window = %v, want only the last message", remaining)
	}
}

// TestCompactor_ForceTarget verifies the on-demand path compacts down to an
// explicit target even when the configured trigger would not have fired.
func TestCompactor_ForceTarget(t *testing.T) {
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

	// Configured trigger is far above the 500-byte transcript, so the auto path
	// is a no-op; the explicit target forces compaction anyway.
	c := NewCompactor(p, store, store, nil, 1<<20, 1<<19)
	if err := c.MaybeCompact(ctx, ag, r.ID); err != nil {
		t.Fatalf("MaybeCompact: %v", err)
	}
	if offset, _ := store.GetCompactionOffset(ctx, "housewife", r.ID); offset != 0 {
		t.Fatalf("auto path compacted below trigger: offset %d", offset)
	}

	if err := c.Compact(ctx, ag, r.ID, 150); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	offset, err := store.GetCompactionOffset(ctx, "housewife", r.ID)
	if err != nil {
		t.Fatalf("GetCompactionOffset: %v", err)
	}
	if want := ids[3]; offset != want {
		t.Fatalf("offset = %d, want %d", offset, want)
	}
}

// TestCompactor_BelowTrigger verifies compaction is a no-op when the transcript
// has not grown past the trigger.
func TestCompactor_BelowTrigger(t *testing.T) {
	_, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(t, store, 5, alice)
	ctx := context.Background()
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID, Sender: alice,
		ClearanceTag: 5, Type: room.MessageText, Content: "small",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	c := NewCompactor(p, store, store, nil, 1<<20, 1<<19)
	if err := c.MaybeCompact(ctx, ag, r.ID); err != nil {
		t.Fatalf("MaybeCompact: %v", err)
	}
	offset, err := store.GetCompactionOffset(ctx, "housewife", r.ID)
	if err != nil {
		t.Fatalf("GetCompactionOffset: %v", err)
	}
	if offset != 0 {
		t.Fatalf("offset advanced to %d on a below-trigger transcript", offset)
	}
}

// TestAssembler_ClearanceMemoryAggregation verifies that assembly at an operating
// clearance reads only the clearance levels at or below it: a level-3 memory file
// is invisible to an agent operating at clearance 2.
func TestAssembler_ClearanceMemoryAggregation(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	if err := WriteMemory(p.AgentClearanceDir("housewife", 1), "alpha-fact"); err != nil {
		t.Fatalf("write c1 memory: %v", err)
	}
	if err := WriteMemory(p.AgentClearanceDir("housewife", 3), "gamma-fact"); err != nil {
		t.Fatalf("write c3 memory: %v", err)
	}

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 2, Name: "Alice"}
	r := newTestRoom(t, store, 2, alice) // operating clearance = min(5,2) = 2

	payload, err := assembler.Assemble(context.Background(), ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if !strings.Contains(payload.Memory, "alpha-fact") {
		t.Errorf("memory missing c1 content at operating clearance 2: %q", payload.Memory)
	}
	if strings.Contains(payload.Memory, "gamma-fact") {
		t.Errorf("memory leaked c3 content at operating clearance 2: %q", payload.Memory)
	}
}

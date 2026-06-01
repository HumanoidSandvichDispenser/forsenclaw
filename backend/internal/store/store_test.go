package store

import (
	"context"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

func newTestStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	dbPath := t.TempDir() + "/rooms.db"
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store, dbPath
}

func newTestRoom(participants ...room.Actor) room.Room {
	return room.Room{
		Participants: participants,
		Clearance:    5,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func TestSQLiteStore_CreateRoom(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}
	r := newTestRoom(alice, housewife)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// ID should be assigned by autoincrement
	if r.ID <= 0 {
		t.Fatalf("expected room ID to be assigned, got %d", r.ID)
	}

	// Verify the room was persisted
	got, err := store.GetRoom(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.ID != r.ID {
		t.Errorf("ID mismatch: got %d, want %d", got.ID, r.ID)
	}
	if len(got.Participants) != 2 {
		t.Errorf("participant count: got %d, want 2", len(got.Participants))
	}
	if got.Clearance != 5 {
		t.Errorf("clearance: got %d, want 5", got.Clearance)
	}
}

func TestSQLiteStore_CreateRoom_InvalidRoom(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	r := room.Room{Clearance: 5} // missing participants

	err := store.CreateRoom(ctx, &r)
	if err == nil {
		t.Fatal("expected error for invalid room, got nil")
	}
}

func TestSQLiteStore_CreateRoom_DuplicateID(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5}
	r := newTestRoom(alice, housewife)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom first: %v", err)
	}

	// Second insert with explicit ID should fail (autoincrement PK conflict)
	r2 := newTestRoom(alice, housewife)
	r2.ID = r.ID
	err := store.CreateRoom(ctx, &r2)
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
}

func TestSQLiteStore_GetRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	_, err := store.GetRoom(ctx, 999999)
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestSQLiteStore_ListRooms(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5}
	bob := room.Actor{ID: "user:bob", Type: room.ActorUser, Clearance: 5}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5}
	scout := room.Actor{ID: "agent:scout", Type: room.ActorAgent, Clearance: 2}

	ctx := context.Background()

	room1 := newTestRoom(alice, housewife)
	room2 := newTestRoom(alice, scout)
	room3 := newTestRoom(bob, scout)

	for _, r := range []room.Room{room1, room2, room3} {
		if err := store.CreateRoom(ctx, &r); err != nil {
			t.Fatalf("CreateRoom: %v", err)
		}
	}

	// List all rooms
	all, err := store.ListRooms(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 rooms, got %d", len(all))
	}

	// Filter by participant
	aliceRooms, err := store.ListRooms(ctx, ListOpts{Participant: "user:alice", Limit: 10})
	if err != nil {
		t.Fatalf("ListRooms by participant: %v", err)
	}
	if len(aliceRooms) != 2 {
		t.Fatalf("expected 2 rooms for alice, got %d", len(aliceRooms))
	}

	// Pagination: limit
	limited, err := store.ListRooms(ctx, ListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("ListRooms limit: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("expected 2 rooms with limit=2, got %d", len(limited))
	}

	// Pagination: offset
	offset, err := store.ListRooms(ctx, ListOpts{Limit: 10, Offset: 2})
	if err != nil {
		t.Fatalf("ListRooms offset: %v", err)
	}
	if len(offset) != 1 {
		t.Fatalf("expected 1 room with offset=2, got %d", len(offset))
	}
}

func TestSQLiteStore_ListRooms_DefaultLimit(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 250; i++ {
		alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5}
		agent := room.Actor{ID: "agent:scout", Type: room.ActorAgent, Clearance: 2}
		r := newTestRoom(alice, agent)
		if err := store.CreateRoom(ctx, &r); err != nil {
			t.Fatalf("CreateRoom %d: %v", i, err)
		}
	}

	// Default limit should cap at 50
	rooms, err := store.ListRooms(ctx, ListOpts{})
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 50 {
		t.Fatalf("expected default limit of 50, got %d", len(rooms))
	}

	// Max limit should cap at 200
	rooms, err = store.ListRooms(ctx, ListOpts{Limit: 500})
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 200 {
		t.Fatalf("expected max limit of 200, got %d", len(rooms))
	}
}

func TestSQLiteStore_UpdateRoom(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 1}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 1}
	scout := room.Actor{ID: "agent:scout", Type: room.ActorAgent, Clearance: 1}

	r := newTestRoom(alice, housewife)
	r.Clearance = 1
	ctx := context.Background()
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Update participants
	r.Participants = []room.Actor{alice, scout}
	if err := store.UpdateRoom(ctx, &r); err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}

	// Verify
	got, err := store.GetRoom(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRoom after update: %v", err)
	}
	if len(got.Participants) != 2 {
		t.Errorf("participants: got %d, want 2", len(got.Participants))
	}
	// Participants are returned sorted by actor_id ASC: agent:scout < user:alice
	if got.Participants[0].ID != "agent:scout" {
		t.Errorf("participant[0]: got %q, want agent:scout", got.Participants[0].ID)
	}
}

func TestSQLiteStore_UpdateRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5}
	r := newTestRoom(alice)
	r.ID = 999999

	ctx := context.Background()
	err := store.UpdateRoom(ctx, &r)
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestSQLiteStore_DeleteRoom(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5}
	r := newTestRoom(alice, housewife)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Delete
	if err := store.DeleteRoom(ctx, r.ID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}

	// Verify it's gone
	_, err := store.GetRoom(ctx, r.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestSQLiteStore_DeleteRoom_CleansUpOrphans(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}
	r := newTestRoom(alice, housewife)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Append a message.
	_, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now().UTC(),
		RoomID:    r.ID,
		Sender:    alice,
		Type:      room.MessageText,
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Set a compaction cursor.
	if err := store.SetCompactionOffset(ctx, "housewife", r.ID, 1); err != nil {
		t.Fatalf("SetCompactionOffset: %v", err)
	}

	// Delete the room.
	if err := store.DeleteRoom(ctx, r.ID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}

	// Messages should be gone.
	msgs, err := store.GetMessages(ctx, r.ID, ReadOpts{})
	if err != nil {
		t.Fatalf("GetMessages after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after room delete, got %d", len(msgs))
	}

	// Compaction cursor should be gone (returns 0 as if never set).
	offset, err := store.GetCompactionOffset(ctx, "housewife", r.ID)
	if err != nil {
		t.Fatalf("GetCompactionOffset after delete: %v", err)
	}
	if offset != 0 {
		t.Errorf("expected compaction offset 0 after room delete, got %d", offset)
	}
}

func TestSQLiteStore_DeleteRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	err := store.DeleteRoom(ctx, 999999)
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestSQLiteStore_CompactionOffset(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Non-existent entry → should default to 0
	offset, err := store.GetCompactionOffset(ctx, "housewife", 1)
	if err != nil {
		t.Fatalf("GetCompactionOffset: %v", err)
	}
	if offset != 0 {
		t.Fatalf("expected offset 0 for new entry, got %d", offset)
	}

	// Set offset
	if err := store.SetCompactionOffset(ctx, "housewife", 1, 50); err != nil {
		t.Fatalf("SetCompactionOffset: %v", err)
	}

	offset, err = store.GetCompactionOffset(ctx, "housewife", 1)
	if err != nil {
		t.Fatalf("GetCompactionOffset after set: %v", err)
	}
	if offset != 50 {
		t.Fatalf("expected offset 50, got %d", offset)
	}

	// Update offset
	if err := store.SetCompactionOffset(ctx, "housewife", 1, 100); err != nil {
		t.Fatalf("SetCompactionOffset update: %v", err)
	}

	offset, err = store.GetCompactionOffset(ctx, "housewife", 1)
	if err != nil {
		t.Fatalf("GetCompactionOffset after update: %v", err)
	}
	if offset != 100 {
		t.Fatalf("expected offset 100, got %d", offset)
	}
}

func TestSQLiteStore_CompactionOffset_Invalid(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	if err := store.SetCompactionOffset(ctx, "", 1, int64(10)); err == nil {
		t.Fatal("expected error for missing agentName")
	}
	if err := store.SetCompactionOffset(ctx, "housewife", 0, int64(10)); err == nil {
		t.Fatal("expected error for missing roomID")
	}
	if err := store.SetCompactionOffset(ctx, "housewife", 1, int64(-1)); err == nil {
		t.Fatal("expected error for negative offset")
	}
}

func TestSQLiteStore_RoomName(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}
	r := newTestRoom(alice, housewife)
	r.Name = "Alice's Kitchen"

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Verify name persisted
	got, err := store.GetRoom(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.Name != "Alice's Kitchen" {
		t.Errorf("name: got %q, want %q", got.Name, "Alice's Kitchen")
	}

	// Update name
	r.Name = "Renamed Kitchen"
	if err := store.UpdateRoom(ctx, &r); err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}

	got, err = store.GetRoom(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRoom after update: %v", err)
	}
	if got.Name != "Renamed Kitchen" {
		t.Errorf("name after update: got %q, want %q", got.Name, "Renamed Kitchen")
	}

	// List should also return the name
	rooms, err := store.ListRooms(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("expected 1 room, got %d", len(rooms))
	}
	if rooms[0].Name != "Renamed Kitchen" {
		t.Errorf("list name: got %q, want %q", rooms[0].Name, "Renamed Kitchen")
	}
}

func TestSQLiteStore_DBFileCreated(t *testing.T) {
	dbPath := t.TempDir() + "/rooms.db"

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
}

func TestSQLiteStore_Messages(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create a room first
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}
	r := newTestRoom(alice, housewife)
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Append messages
	msg1 := room.Message{
		Timestamp: time.Now().UTC(),
		RoomID:    r.ID,
		Sender:    alice,
		Type:      room.MessageText,
		Content:   "Hello",
	}
	id1, err := store.AppendMessage(ctx, r.ID, msg1)
	if err != nil {
		t.Fatalf("AppendMessage 1: %v", err)
	}
	if id1 <= 0 {
		t.Errorf("expected positive message id, got %d", id1)
	}

	msg2 := room.Message{
		Timestamp: time.Now().UTC(),
		RoomID:    r.ID,
		Sender:    housewife,
		Type:      room.MessageText,
		Content:   "Hi there",
	}
	id2, err := store.AppendMessage(ctx, r.ID, msg2)
	if err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}
	if id2 <= id1 {
		t.Errorf("expected id2 > id1, got id1=%d id2=%d", id1, id2)
	}

	// Get all messages
	msgs, err := store.GetMessages(ctx, r.ID, ReadOpts{})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello" {
		t.Errorf("msg[0]: got content=%q, want Hello", msgs[0].Content)
	}
	if msgs[1].Content != "Hi there" {
		t.Errorf("msg[1]: got content=%q, want Hi there", msgs[1].Content)
	}

	// Get with limit (tail behaviour: last 1 message)
	limited, err := store.GetMessages(ctx, r.ID, ReadOpts{Limit: 1})
	if err != nil {
		t.Fatalf("GetMessages limit: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected 1 message with limit=1, got %d", len(limited))
	}
	if limited[0].ID != id2 {
		t.Errorf("limited[0]: got id=%d, want %d", limited[0].ID, id2)
	}

	// Get with compaction: stop before id1
	compacted, err := store.GetMessages(ctx, r.ID, ReadOpts{CompactionID: id1})
	if err != nil {
		t.Fatalf("GetMessages compaction: %v", err)
	}
	if len(compacted) != 1 {
		t.Fatalf("expected 1 message with compaction boundary, got %d", len(compacted))
	}
	if compacted[0].ID != id2 {
		t.Errorf("compacted[0]: got id=%d, want %d", compacted[0].ID, id2)
	}
}

// appendTestMsg is a helper that appends a text message and returns its ID.
func appendTestMsg(t *testing.T, s *SQLiteStore, roomID int64, sender room.Actor, content string) int64 {
	t.Helper()
	id, err := s.AppendMessage(context.Background(), roomID, room.Message{
		Timestamp: time.Now().UTC(),
		RoomID:    roomID,
		Sender:    sender,
		Type:      room.MessageText,
		Content:   content,
	})
	if err != nil {
		t.Fatalf("AppendMessage(%q): %v", content, err)
	}
	return id
}

// insertForkMsg inserts a message directly into the DB with a specific parent,
// bypassing AppendMessage so the room head is not updated.
func insertForkMsg(t *testing.T, s *SQLiteStore, roomID int64, parentID int64, sender room.Actor, content string) int64 {
	t.Helper()
	msg := room.Message{
		Timestamp: time.Now().UTC(),
		RoomID:    roomID,
		ParentID:  &parentID,
		Sender:    sender,
		Type:      room.MessageText,
		Content:   content,
	}
	if err := s.DB().Create(&msg).Error; err != nil {
		t.Fatalf("insertForkMsg(%q): %v", content, err)
	}
	return msg.ID
}

func TestSQLiteStore_SwitchBranch(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(alice)
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Build linear chain: A → B → C (head = C).
	idA := appendTestMsg(t, store, r.ID, alice, "A")
	idB := appendTestMsg(t, store, r.ID, alice, "B")
	idC := appendTestMsg(t, store, r.ID, alice, "C")
	_ = idA

	// Insert D as an alternate child of B, creating a fork at B.
	idD := insertForkMsg(t, store, r.ID, idB, alice, "D")

	// Switch to D — D has no children so head should be D.
	if err := store.SwitchBranch(ctx, r.ID, idD); err != nil {
		t.Fatalf("SwitchBranch to D: %v", err)
	}
	msgs, err := store.GetMessages(ctx, r.ID, ReadOpts{})
	if err != nil {
		t.Fatalf("GetMessages after switch to D: %v", err)
	}
	wantContents(t, msgs, "A", "B", "D")

	// Switch back to C — head should return to C.
	if err := store.SwitchBranch(ctx, r.ID, idC); err != nil {
		t.Fatalf("SwitchBranch to C: %v", err)
	}
	msgs, err = store.GetMessages(ctx, r.ID, ReadOpts{})
	if err != nil {
		t.Fatalf("GetMessages after switch to C: %v", err)
	}
	wantContents(t, msgs, "A", "B", "C")
}

func TestSQLiteStore_SwitchBranch_FollowsCursors(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(alice)
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Build: A → B → C (head = C). Then insert D as fork child of B,
	// E as child of D, with branch cursor D→E already set.
	// SwitchBranch(D) should walk D→E and set head = E.
	idA := appendTestMsg(t, store, r.ID, alice, "A")
	idB := appendTestMsg(t, store, r.ID, alice, "B")
	_ = appendTestMsg(t, store, r.ID, alice, "C")
	_ = idA

	idD := insertForkMsg(t, store, r.ID, idB, alice, "D")
	idE := insertForkMsg(t, store, r.ID, idD, alice, "E")

	// Manually seed a branch cursor D→E (as if E was previously the active child of D).
	if err := store.DB().Exec(
		"INSERT INTO message_branch_cursors (parent_id, child_id) VALUES (?, ?)", idD, idE,
	).Error; err != nil {
		t.Fatalf("seed cursor D→E: %v", err)
	}

	if err := store.SwitchBranch(ctx, r.ID, idD); err != nil {
		t.Fatalf("SwitchBranch to D: %v", err)
	}
	msgs, err := store.GetMessages(ctx, r.ID, ReadOpts{})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	wantContents(t, msgs, "A", "B", "D", "E")
}

func TestSQLiteStore_GetSiblings(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(alice)
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Build: A → B, with C as a fork sibling of B (both children of A).
	idA := appendTestMsg(t, store, r.ID, alice, "A")
	idB := appendTestMsg(t, store, r.ID, alice, "B")
	idC := insertForkMsg(t, store, r.ID, idA, alice, "C")

	// Both B and C are children of A.
	siblings, err := store.GetSiblings(ctx, idB)
	if err != nil {
		t.Fatalf("GetSiblings(B): %v", err)
	}
	wantContents(t, siblings, "B", "C")

	siblings, err = store.GetSiblings(ctx, idC)
	if err != nil {
		t.Fatalf("GetSiblings(C): %v", err)
	}
	wantContents(t, siblings, "B", "C")
}

func TestSQLiteStore_GetSiblings_Root(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(alice)
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Two root messages (parent_id = NULL) in the same room.
	idA := appendTestMsg(t, store, r.ID, alice, "root-A")
	// Insert root-B directly so it also has parent_id = NULL.
	rootB := room.Message{
		Timestamp: time.Now().UTC(),
		RoomID:    r.ID,
		ParentID:  nil,
		Sender:    alice,
		Type:      room.MessageText,
		Content:   "root-B",
	}
	if err := store.DB().Create(&rootB).Error; err != nil {
		t.Fatalf("insert root-B: %v", err)
	}

	siblings, err := store.GetSiblings(ctx, idA)
	if err != nil {
		t.Fatalf("GetSiblings(root-A): %v", err)
	}
	wantContents(t, siblings, "root-A", "root-B")
}

// wantContents asserts that msgs has exactly the given contents in order.
func wantContents(t *testing.T, msgs []room.Message, want ...string) {
	t.Helper()
	if len(msgs) != len(want) {
		got := make([]string, len(msgs))
		for i, m := range msgs {
			got[i] = m.Content
		}
		t.Fatalf("got %d messages %v, want %d %v", len(msgs), got, len(want), want)
	}
	for i, w := range want {
		if msgs[i].Content != w {
			t.Errorf("msgs[%d]: got %q, want %q", i, msgs[i].Content, w)
		}
	}
}

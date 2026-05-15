package room

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rooms.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store, dbPath
}

func newTestRoom(participants ...Actor) Room {
	return Room{
		ID:               uuid.New().String(),
		Participants:     participants,
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
}

func TestSQLiteStore_CreateRoom(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5, Name: "Alice"}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5, Name: "Housewife"}
	room := newTestRoom(alice, housewife)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &room); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Verify the room was persisted
	got, err := store.GetRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.ID != room.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, room.ID)
	}
	if len(got.Participants) != 2 {
		t.Errorf("participant count: got %d, want 2", len(got.Participants))
	}
	if got.ClearanceCeiling != 5 {
		t.Errorf("clearance: got %d, want 5", got.ClearanceCeiling)
	}
}

func TestSQLiteStore_CreateRoom_InvalidRoom(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	room := Room{ID: uuid.New().String()} // missing participants

	err := store.CreateRoom(ctx, &room)
	if err == nil {
		t.Fatal("expected error for invalid room, got nil")
	}
}

func TestSQLiteStore_CreateRoom_DuplicateID(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}
	room := newTestRoom(alice, housewife)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &room); err != nil {
		t.Fatalf("CreateRoom first: %v", err)
	}

	// Second insert with same ID should fail
	room2 := newTestRoom(alice, housewife)
	room2.ID = room.ID
	err := store.CreateRoom(ctx, &room2)
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
}

func TestSQLiteStore_GetRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	_, err := store.GetRoom(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestSQLiteStore_ListRooms(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	bob := Actor{ID: "user:bob", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}
	scout := Actor{ID: "agent:scout", Type: ActorAgent, Clearance: 2}

	ctx := context.Background()

	room1 := newTestRoom(alice, housewife)
	room2 := newTestRoom(alice, scout)
	room3 := newTestRoom(bob, scout)

	for _, r := range []Room{room1, room2, room3} {
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

	// Filter by protocol
	freeformRooms, err := store.ListRooms(ctx, ListOpts{Protocol: string(ProtocolFreeForm), Limit: 10})
	if err != nil {
		t.Fatalf("ListRooms by protocol: %v", err)
	}
	if len(freeformRooms) != 3 {
		t.Fatalf("expected 3 freeform rooms, got %d", len(freeformRooms))
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
		alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
		agent := Actor{ID: "agent:scout", Type: ActorAgent, Clearance: 2}
		room := newTestRoom(alice, agent)
		if err := store.CreateRoom(ctx, &room); err != nil {
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

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 1}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 1}
	scout := Actor{ID: "agent:scout", Type: ActorAgent, Clearance: 1}

	room := newTestRoom(alice, housewife)
	room.ClearanceCeiling = 1
	ctx := context.Background()
	if err := store.CreateRoom(ctx, &room); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Update participants
	room.Participants = []Actor{alice, scout}
	if err := store.UpdateRoom(ctx, &room); err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}

	// Verify
	got, err := store.GetRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("GetRoom after update: %v", err)
	}
	if len(got.Participants) != 2 {
		t.Errorf("participants: got %d, want 2", len(got.Participants))
	}
	if got.Participants[1].ID != "agent:scout" {
		t.Errorf("participant[1]: got %q, want scout", got.Participants[1].ID)
	}
}

func TestSQLiteStore_UpdateRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	room := newTestRoom(alice)
	room.ID = "nonexistent"

	ctx := context.Background()
	err := store.UpdateRoom(ctx, &room)
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestSQLiteStore_DeleteRoom(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}
	room := newTestRoom(alice, housewife)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &room); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Delete
	if err := store.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}

	// Verify it's gone
	_, err := store.GetRoom(ctx, room.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestSQLiteStore_DeleteRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	err := store.DeleteRoom(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestSQLiteStore_DBFileCreated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rooms.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("expected DB file to be created")
	}
}

func TestSQLiteStore_ProtocolStatePersistence(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}
	room := newTestRoom(alice, housewife)

	// Set a protocol state
	state := ProtocolState{Type: "freeform", State: []byte(`{"turn_count":3}`)}
	room.ProtocolState, _ = json.Marshal(state)

	ctx := context.Background()
	if err := store.CreateRoom(ctx, &room); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	got, err := store.GetRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}

	var gotState ProtocolState
	if err := json.Unmarshal(got.ProtocolState, &gotState); err != nil {
		t.Fatalf("unmarshal protocol state: %v", err)
	}
	if gotState.Type != "freeform" {
		t.Errorf("protocol state type: got %q, want freeform", gotState.Type)
	}
	if string(gotState.State) != `{"turn_count":3}` {
		t.Errorf("protocol state: got %s, want {\"turn_count\":3}", string(gotState.State))
	}
}

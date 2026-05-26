package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

func newTestStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rooms.db")
	store, err := NewSQLiteStore(dbPath, dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store, dbPath
}

func newTestRoom(participants ...room.Actor) room.Room {
	return room.Room{
		ID:               uuid.New().String(),
		Participants:     participants,
		Clearance: 5,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
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

	// Verify the room was persisted
	got, err := store.GetRoom(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.ID != r.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, r.ID)
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
	r := room.Room{ID: uuid.New().String()} // missing participants

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

	// Second insert with same ID should fail
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
	_, err := store.GetRoom(ctx, "nonexistent")
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
	if got.Participants[1].ID != "agent:scout" {
		t.Errorf("participant[1]: got %q, want scout", got.Participants[1].ID)
	}
}

func TestSQLiteStore_UpdateRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5}
	r := newTestRoom(alice)
	r.ID = "nonexistent"

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

func TestSQLiteStore_DeleteRoom_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	err := store.DeleteRoom(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestSQLiteStore_CompactionOffset(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Non-existent entry → should default to 0
	offset, err := store.GetCompactionOffset(ctx, "housewife", "room_1")
	if err != nil {
		t.Fatalf("GetCompactionOffset: %v", err)
	}
	if offset != 0 {
		t.Fatalf("expected offset 0 for new entry, got %d", offset)
	}

	// Set offset
	if err := store.SetCompactionOffset(ctx, "housewife", "room_1", 50); err != nil {
		t.Fatalf("SetCompactionOffset: %v", err)
	}

	offset, err = store.GetCompactionOffset(ctx, "housewife", "room_1")
	if err != nil {
		t.Fatalf("GetCompactionOffset after set: %v", err)
	}
	if offset != 50 {
		t.Fatalf("expected offset 50, got %d", offset)
	}

	// Update offset
	if err := store.SetCompactionOffset(ctx, "housewife", "room_1", 100); err != nil {
		t.Fatalf("SetCompactionOffset update: %v", err)
	}

	offset, err = store.GetCompactionOffset(ctx, "housewife", "room_1")
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

	if err := store.SetCompactionOffset(ctx, "", "room_1", 10); err == nil {
		t.Fatal("expected error for missing agentName")
	}
	if err := store.SetCompactionOffset(ctx, "housewife", "", 10); err == nil {
		t.Fatal("expected error for missing roomID")
	}
	if err := store.SetCompactionOffset(ctx, "housewife", "room_1", -1); err == nil {
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
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rooms.db")

	store, err := NewSQLiteStore(dbPath, dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("expected DB file to be created")
	}
}

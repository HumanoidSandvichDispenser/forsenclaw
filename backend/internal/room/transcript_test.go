package room

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTranscriptWriter_Append(t *testing.T) {
	dir := t.TempDir()
	roomID := "room_abc"

	w, err := NewTranscriptWriter(dir, roomID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	defer w.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5, Name: "Alice"}
	msg := Message{
		ID:           uuid.New().String(),
		Timestamp:    time.Now().UTC(),
		RoomID:       roomID,
		Sender:       alice,
		ClearanceTag: 5,
		Type:         MessageText,
		Content:      "Hello, housewife!",
	}

	ctx := context.Background()
	if err := w.Append(ctx, msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Verify file exists and has content
	path := filepath.Join(dir, roomID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty transcript file")
	}
}

func TestTranscriptWriter_Append_InvalidMessage(t *testing.T) {
	dir := t.TempDir()
	w, err := NewTranscriptWriter(dir, "room_1")
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	defer w.Close()

	ctx := context.Background()
	invalidMsg := Message{ID: "", Content: ""} // missing required fields

	err = w.Append(ctx, invalidMsg)
	if err == nil {
		t.Fatal("expected error for invalid message, got nil")
	}
}

func TestReadMessages(t *testing.T) {
	dir := t.TempDir()
	roomID := "room_def"

	w, err := NewTranscriptWriter(dir, roomID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5, Name: "Alice"}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5, Name: "Housewife"}

	ctx := context.Background()
	now := time.Now().UTC()

	msgs := []Message{
		{ID: "msg_1", Timestamp: now.Add(-2 * time.Hour), RoomID: roomID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "First message"},
		{ID: "msg_2", Timestamp: now.Add(-1 * time.Hour), RoomID: roomID, Sender: housewife, ClearanceTag: 5, Type: MessageText, Content: "Response"},
		{ID: "msg_3", Timestamp: now, RoomID: roomID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "Latest"},
	}

	for _, msg := range msgs {
		if err := w.Append(ctx, msg); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	w.Close()

	// Read all messages
	got, err := ReadMessages(ctx, dir, roomID, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].ID != "msg_1" {
		t.Errorf("first message: got %q, want msg_1", got[0].ID)
	}
	if got[2].ID != "msg_3" {
		t.Errorf("last message: got %q, want msg_3", got[2].ID)
	}
}

func TestReadMessages_WithLimit(t *testing.T) {
	dir := t.TempDir()
	roomID := "room_limit"

	w, err := NewTranscriptWriter(dir, roomID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 10; i++ {
		msg := Message{
			ID:           uuid.New().String(),
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			RoomID:       roomID,
			Sender:       alice,
			ClearanceTag: 5,
			Type:         MessageText,
			Content:      fmt.Sprintf("Message %d", i),
		}
		if err := w.Append(ctx, msg); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	w.Close()

	// Limit to last 3 messages
	got, err := ReadMessages(ctx, dir, roomID, ReadOpts{Limit: 3})
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	// Last 3 should be messages 7, 8, 9
	if got[0].Content != "Message 7" {
		t.Errorf("first of limited: got %q, want Message 7", got[0].Content)
	}
	if got[2].Content != "Message 9" {
		t.Errorf("last of limited: got %q, want Message 9", got[2].Content)
	}
}

func TestReadMessages_WithTimeFilters(t *testing.T) {
	dir := t.TempDir()
	roomID := "room_time"

	w, err := NewTranscriptWriter(dir, roomID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	ctx := context.Background()
	now := time.Now().UTC()

	msgs := []Message{
		{ID: "old", Timestamp: now.Add(-2 * time.Hour), RoomID: roomID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "Old"},
		{ID: "mid", Timestamp: now.Add(-1 * time.Hour), RoomID: roomID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "Mid"},
		{ID: "new", Timestamp: now, RoomID: roomID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "New"},
	}
	for _, msg := range msgs {
		if err := w.Append(ctx, msg); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	w.Close()

	// After filter
	after := now.Add(-90 * time.Minute)
	got, err := ReadMessages(ctx, dir, roomID, ReadOpts{After: &after})
	if err != nil {
		t.Fatalf("ReadMessages after: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages after filter, got %d", len(got))
	}

	// Before filter
	before := now.Add(-30 * time.Minute)
	got, err = ReadMessages(ctx, dir, roomID, ReadOpts{Before: &before})
	if err != nil {
		t.Fatalf("ReadMessages before: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages before filter, got %d", len(got))
	}

	// Combined after + before
	got, err = ReadMessages(ctx, dir, roomID, ReadOpts{After: &after, Before: &before})
	if err != nil {
		t.Fatalf("ReadMessages combined: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 message in range, got %d", len(got))
	}
	if got[0].ID != "mid" {
		t.Errorf("expected mid message, got %q", got[0].ID)
	}
}

func TestReadMessages_EmptyTranscript(t *testing.T) {
	dir := t.TempDir()
	roomID := "room_empty"

	ctx := context.Background()
	msgs, err := ReadMessages(ctx, dir, roomID, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages for nonexistent transcript, got %d", len(msgs))
	}
}

func TestTranscriptWriter_ConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	roomID := "room_concurrent"

	w, err := NewTranscriptWriter(dir, roomID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	defer w.Close()

	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	ctx := context.Background()
	now := time.Now().UTC()

	// Launch multiple goroutines appending simultaneously
	const numGoroutines = 10
	const msgsPerGoroutine = 20

	done := make(chan struct{}, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(gid int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < msgsPerGoroutine; j++ {
				msg := Message{
					ID:           uuid.New().String(),
					Timestamp:    now.Add(time.Duration(gid*msgsPerGoroutine+j) * time.Millisecond),
					RoomID:       roomID,
					Sender:       alice,
					ClearanceTag: 5,
					Type:         MessageText,
					Content:      "concurrent",
				}
				if err := w.Append(ctx, msg); err != nil {
					t.Errorf("Append goroutine %d msg %d: %v", gid, j, err)
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all messages were written
	msgs, err := ReadMessages(ctx, dir, roomID, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	expected := numGoroutines * msgsPerGoroutine
	if len(msgs) != expected {
		t.Fatalf("expected %d messages, got %d", expected, len(msgs))
	}
}

func TestTranscriptWriter_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested", "rooms")
	roomID := "room_nested"

	w, err := NewTranscriptWriter(subdir, roomID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(subdir); os.IsNotExist(err) {
		t.Fatal("expected directory to be created")
	}
}

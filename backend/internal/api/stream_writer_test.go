package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// subscribeClient registers a client to the hub and waits for the hub to
// process the registration before returning.
func subscribeClient(t *testing.T, hub *Hub, roomID int64) *Client {
	t.Helper()
	client := &Client{
		hub:   hub,
		rooms: map[int64]struct{}{roomID: {}},
		send:  make(chan []byte, 8),
	}
	hub.register <- client
	return client
}

func readDelta(t *testing.T, client *Client) room.MessageDelta {
	t.Helper()
	select {
	case data := <-client.send:
		var event dispatch.StreamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("unmarshal StreamEvent: %v", err)
		}
		if event.Type != "message.delta" {
			t.Fatalf("event.Type = %q, want message.delta", event.Type)
		}
		payloadBytes, _ := json.Marshal(event.Payload)
		var delta room.MessageDelta
		if err := json.Unmarshal(payloadBytes, &delta); err != nil {
			t.Fatalf("unmarshal MessageDelta: %v", err)
		}
		return delta
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delta event")
		return room.MessageDelta{}
	}
}

func TestAgentStreamWriter_BroadcastsDelta(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub()
	go hub.Run()

	client := subscribeClient(t, hub, 1)

	sw := NewAgentStreamWriter(hub)
	if err := sw.StreamAgentDelta(ctx, 1, "minky", "hello"); err != nil {
		t.Fatalf("StreamAgentDelta: %v", err)
	}

	delta := readDelta(t, client)

	if delta.RoomID != 1 {
		t.Errorf("RoomID = %d, want 1", delta.RoomID)
	}
	if delta.Actor.ID != "agent:minky" {
		t.Errorf("Actor.ID = %q, want agent:minky", delta.Actor.ID)
	}
	if delta.Actor.Name != "minky" {
		t.Errorf("Actor.Name = %q, want minky", delta.Actor.Name)
	}
	if delta.Delta != "hello" {
		t.Errorf("Delta = %q, want hello", delta.Delta)
	}
}

func TestAgentStreamWriter_MultipleChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub()
	go hub.Run()

	client := subscribeClient(t, hub, 1)
	sw := NewAgentStreamWriter(hub)

	chunks := []string{"foo", " ", "bar"}
	for _, chunk := range chunks {
		if err := sw.StreamAgentDelta(ctx, 1, "minky", chunk); err != nil {
			t.Fatalf("StreamAgentDelta(%q): %v", chunk, err)
		}
	}

	for _, want := range chunks {
		delta := readDelta(t, client)
		if delta.Delta != want {
			t.Errorf("Delta = %q, want %q", delta.Delta, want)
		}
	}
}

func TestAgentStreamWriter_OnlyBroadcastsToSubscribedRoom(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub()
	go hub.Run()

	// Subscribed to room 2, delta is for room 1.
	client := subscribeClient(t, hub, 2)
	sw := NewAgentStreamWriter(hub)
	_ = sw.StreamAgentDelta(ctx, 1, "minky", "hello")

	select {
	case <-client.send:
		t.Fatal("client subscribed to room 2 should not receive a room 1 event")
	case <-time.After(50 * time.Millisecond):
		// expected: no message
	}
}

func TestAgentStreamWriter_NoSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub()
	go hub.Run()

	sw := NewAgentStreamWriter(hub)
	// No clients — should not block or error.
	if err := sw.StreamAgentDelta(ctx, 1, "minky", "hello"); err != nil {
		t.Fatalf("StreamAgentDelta with no subscribers: %v", err)
	}
}

package room

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewFreeFormProtocol(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5, Name: "Alice"}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5, Name: "Housewife"}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, err := NewFreeFormProtocol(&r)
	if err != nil {
		t.Fatalf("NewFreeFormProtocol: %v", err)
	}
	if proto == nil {
		t.Fatal("expected protocol, got nil")
	}
	if proto.config.MaxTurns != 20 {
		t.Errorf("default max_turns: got %d, want 20", proto.config.MaxTurns)
	}
}

func TestNewFreeFormProtocol_WithConfig(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5, Name: "Alice"}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5, Name: "Housewife"}

	config := FreeFormConfig{MaxTurns: 10}
	configJSON, _ := json.Marshal(config)
	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
		ProtocolConfig:   configJSON,
	}

	proto, err := NewFreeFormProtocol(&r)
	if err != nil {
		t.Fatalf("NewFreeFormProtocol: %v", err)
	}
	if proto.config.MaxTurns != 10 {
		t.Errorf("configured max_turns: got %d, want 10", proto.config.MaxTurns)
	}
}

func TestNewFreeFormProtocol_NilRoom(t *testing.T) {
	_, err := NewFreeFormProtocol(nil)
	if err == nil {
		t.Fatal("expected error for nil room, got nil")
	}
}

func TestNewFreeFormProtocol_WrongParticipantCount(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	_, err := NewFreeFormProtocol(&r)
	if err == nil {
		t.Fatal("expected error for 1 participant, got nil")
	}
}

func TestNewFreeFormProtocol_InvalidConfig(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
		ProtocolConfig:   []byte("not json"),
	}

	_, err := NewFreeFormProtocol(&r)
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

func TestFreeFormProtocol_Start(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)
	if err := proto.Start(&r, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestFreeFormProtocol_OnMessage_IssuesRFC(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5, Name: "Alice"}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5, Name: "Housewife"}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)

	mockDisp := &mockDispatcher{}
	proto.SetDispatcher(mockDisp)

	msg := Message{
		ID: uuid.New().String(), Timestamp: time.Now().UTC(),
		RoomID: r.ID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "Hello!",
	}

	err := proto.OnMessage(&r, alice, msg)
	if err != nil {
		t.Fatalf("OnMessage: %v", err)
	}

	if mockDisp.lastRFC == nil {
		t.Fatal("expected RFC to be issued, got nil")
	}
	if mockDisp.lastRFC.Target != "agent:housewife" {
		t.Errorf("RFC target: got %q, want agent:housewife", mockDisp.lastRFC.Target)
	}
	if mockDisp.lastRFC.RoomID != r.ID {
		t.Errorf("RFC room: got %q, want %q", mockDisp.lastRFC.RoomID, r.ID)
	}
}

func TestFreeFormProtocol_OnMessage_SenderNotParticipant(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)
	mockDisp := &mockDispatcher{}
	proto.SetDispatcher(mockDisp)

	bob := Actor{ID: "user:bob", Type: ActorUser, Clearance: 5}
	msg := Message{
		ID: uuid.New().String(), Timestamp: time.Now().UTC(),
		RoomID: r.ID, Sender: bob, ClearanceTag: 5, Type: MessageText, Content: "Hello!",
	}

	// The protocol validates that the sender is a participant before issuing
	// any RFC. Sending as an unknown actor must return an error.
	err := proto.OnMessage(&r, bob, msg)
	if err == nil {
		t.Fatal("expected error for non-participant sender, got nil")
	}

	// No RFC should be issued.
	if mockDisp.lastRFC != nil {
		t.Fatal("expected no RFC for non-participant sender")
	}
}

func TestFreeFormProtocol_OnMessage_NoAgentTarget(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	bob := Actor{ID: "user:bob", Type: ActorUser, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, bob},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)
	mockDisp := &mockDispatcher{}
	proto.SetDispatcher(mockDisp)

	msg := Message{
		ID: uuid.New().String(), Timestamp: time.Now().UTC(),
		RoomID: r.ID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "Hello!",
	}

	err := proto.OnMessage(&r, alice, msg)
	if err != nil {
		t.Fatalf("OnMessage: %v", err)
	}

	// No RFC should be issued since the other participant is a user
	if mockDisp.lastRFC != nil {
		t.Fatal("expected no RFC for user-only room")
	}
}

func TestFreeFormProtocol_OnRFCResponse(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)
	rfc := RFC{ID: "rfc_1", RoomID: r.ID, Target: "agent:housewife"}
	msg := Message{ID: "msg_1", Content: "Response"}

	if err := proto.OnRFCResponse(&r, rfc, msg); err != nil {
		t.Fatalf("OnRFCResponse: %v", err)
	}

	// Turn count should increment
	if proto.turnCount != 1 {
		t.Errorf("turn count: got %d, want 1", proto.turnCount)
	}
}

func TestFreeFormProtocol_ShouldTerminate(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	config := FreeFormConfig{MaxTurns: 3}
	configJSON, _ := json.Marshal(config)
	r.ProtocolConfig = configJSON

	proto, _ := NewFreeFormProtocol(&r)

	if proto.ShouldTerminate(&r) {
		t.Fatal("expected not terminated at start")
	}

	proto.turnCount = 3
	if !proto.ShouldTerminate(&r) {
		t.Fatal("expected terminated after 3 turns")
	}
}

func TestFreeFormProtocol_ShouldTerminate_NoLimit(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	config := FreeFormConfig{MaxTurns: 0}
	configJSON, _ := json.Marshal(config)
	r.ProtocolConfig = configJSON

	proto, _ := NewFreeFormProtocol(&r)
	proto.turnCount = 999

	if proto.ShouldTerminate(&r) {
		t.Fatal("expected not terminated when max_turns=0")
	}
}

func TestFreeFormProtocol_State(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)
	proto.turnCount = 5

	state := proto.State()
	if state.Type != "freeform" {
		t.Errorf("state type: got %q, want freeform", state.Type)
	}

	var s struct {
		TurnCount int `json:"turn_count"`
	}
	if err := json.Unmarshal(state.State, &s); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if s.TurnCount != 5 {
		t.Errorf("turn_count in state: got %d, want 5", s.TurnCount)
	}
}

func TestFreeFormProtocol_Restore(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)

	state := ProtocolState{
		Type:  "freeform",
		State: []byte(`{"turn_count":7}`),
	}

	if err := proto.Restore(state); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if proto.turnCount != 7 {
		t.Errorf("turn count after restore: got %d, want 7", proto.turnCount)
	}
}

func TestFreeFormProtocol_Restore_WrongType(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
	}

	proto, _ := NewFreeFormProtocol(&r)

	state := ProtocolState{
		Type:  "roundrobin",
		State: []byte(`{"turn_count":7}`),
	}

	err := proto.Restore(state)
	if err == nil {
		t.Fatal("expected error for wrong protocol type, got nil")
	}
}

func TestFreeFormProtocol_OnMessage_Terminated(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}

	config := FreeFormConfig{MaxTurns: 1}
	configJSON, _ := json.Marshal(config)
	r := Room{
		ID:               uuid.New().String(),
		Participants:     []Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     ProtocolFreeForm,
		ProtocolConfig:   configJSON,
	}

	proto, _ := NewFreeFormProtocol(&r)
	proto.SetDispatcher(&mockDispatcher{})
	proto.turnCount = 1

	msg := Message{
		ID: uuid.New().String(), Timestamp: time.Now().UTC(),
		RoomID: r.ID, Sender: alice, ClearanceTag: 5, Type: MessageText, Content: "Hello!",
	}

	err := proto.OnMessage(&r, alice, msg)
	if err == nil {
		t.Fatal("expected error for terminated protocol, got nil")
	}
}

// mockDispatcher is a test double for the Dispatcher interface.
type mockDispatcher struct {
	lastRFC *RFC
}

func (m *mockDispatcher) IssueRFC(rfc RFC) error {
	m.lastRFC = &rfc
	return nil
}

func (m *mockDispatcher) BroadcastRFC(room *Room, payload RFCPayload) error {
	return nil
}

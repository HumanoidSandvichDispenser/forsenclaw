package room

import (
	"strings"
	"testing"
)

func TestActor_Validate(t *testing.T) {
	tests := []struct {
		name    string
		actor   Actor
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid user",
			actor:   Actor{ID: "user:alice", Type: ActorUser, Clearance: 5, Name: "Alice"},
			wantErr: false,
		},
		{
			name:    "valid agent",
			actor:   Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5, Name: "Housewife"},
			wantErr: false,
		},
		{
			name:    "missing id",
			actor:   Actor{Type: ActorUser, Clearance: 1, Name: "Alice"},
			wantErr: true,
			errMsg:  "actor ID is required",
		},
		{
			name:    "invalid type",
			actor:   Actor{ID: "user:alice", Type: "robot", Clearance: 1, Name: "Alice"},
			wantErr: true,
			errMsg:  "invalid actor type",
		},
		{
			name:    "negative clearance",
			actor:   Actor{ID: "user:alice", Type: ActorUser, Clearance: -1, Name: "Alice"},
			wantErr: true,
			errMsg:  "clearance must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.actor.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestActor_IsUser_IsAgent(t *testing.T) {
	user := Actor{ID: "user:alice", Type: ActorUser}
	agent := Actor{ID: "agent:housewife", Type: ActorAgent}

	if !user.IsUser() {
		t.Error("expected user.IsUser() to be true")
	}
	if user.IsAgent() {
		t.Error("expected user.IsAgent() to be false")
	}
	if agent.IsUser() {
		t.Error("expected agent.IsUser() to be false")
	}
	if !agent.IsAgent() {
		t.Error("expected agent.IsAgent() to be true")
	}
}

func TestMessage_Validate(t *testing.T) {
	validSender := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}

	tests := []struct {
		name    string
		msg     Message
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: Message{
				ID: "msg_1", RoomID: "room_1", Sender: validSender,
				Type: MessageText, Content: "Hello",
			},
			wantErr: false,
		},
		{
			name:    "missing id",
			msg:     Message{RoomID: "room_1", Sender: validSender, Type: MessageText, Content: "Hello"},
			wantErr: true,
			errMsg:  "message ID is required",
		},
		{
			name:    "missing room_id",
			msg:     Message{ID: "msg_1", Sender: validSender, Type: MessageText, Content: "Hello"},
			wantErr: true,
			errMsg:  "room_id is required",
		},
		{
			name:    "invalid sender",
			msg:     Message{ID: "msg_1", RoomID: "room_1", Type: MessageText, Content: "Hello"},
			wantErr: true,
			errMsg:  "sender",
		},
		{
			name: "invalid message type",
			msg: Message{
				ID: "msg_1", RoomID: "room_1", Sender: validSender,
				Type: "chat", Content: "Hello",
			},
			wantErr: true,
			errMsg:  "invalid message type",
		},
		{
			name: "missing content",
			msg: Message{
				ID: "msg_1", RoomID: "room_1", Sender: validSender,
				Type: MessageText,
			},
			wantErr: true,
			errMsg:  "content is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRoom_Validate(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}
	scout := Actor{ID: "agent:scout", Type: ActorAgent, Clearance: 2}

	tests := []struct {
		name    string
		room    Room
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid room",
			room: Room{
				ID: "room_1", Participants: []Actor{alice, housewife},
				ClearanceCeiling: 5,
			},
			wantErr: false,
		},
		{
			name: "valid room with name",
			room: Room{
				ID: "room_1", Name: "Alice's Chat",
				Participants:     []Actor{alice, housewife},
				ClearanceCeiling: 5,
			},
			wantErr: false,
		},
		{
			name:    "missing id",
			room:    Room{Participants: []Actor{alice}, ClearanceCeiling: 5},
			wantErr: true,
			errMsg:  "room ID is required",
		},
		{
			name:    "no participants",
			room:    Room{ID: "room_1", ClearanceCeiling: 5},
			wantErr: true,
			errMsg:  "at least one participant",
		},
		{
			name: "negative clearance ceiling",
			room: Room{
				ID: "room_1", Participants: []Actor{alice},
				ClearanceCeiling: -1,
			},
			wantErr: true,
			errMsg:  "clearance_ceiling must be non-negative",
		},
		{
			name: "participant exceeds ceiling",
			room: Room{
				ID: "room_1", Participants: []Actor{alice, scout},
				ClearanceCeiling: 1,
			},
			wantErr: true,
			errMsg:  "exceeds room ceiling",
		},
		{
			name: "invalid participant",
			room: Room{
				ID: "room_1", Participants: []Actor{{ID: "", Type: ActorUser, Clearance: 1}},
				ClearanceCeiling: 5,
			},
			wantErr: true,
			errMsg:  "actor ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.room.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRoom_ParticipantLookups(t *testing.T) {
	alice := Actor{ID: "user:alice", Type: ActorUser, Clearance: 5}
	housewife := Actor{ID: "agent:housewife", Type: ActorAgent, Clearance: 5}
	scout := Actor{ID: "agent:scout", Type: ActorAgent, Clearance: 2}

	r := Room{
		ID:           "room_1",
		Participants: []Actor{alice, housewife, scout},
	}

	if got := r.UserParticipant(); got == nil || got.ID != "user:alice" {
		t.Fatalf("expected UserParticipant() to be alice, got %v", got)
	}
	if got := r.AgentParticipant(); got == nil || got.ID != "agent:housewife" {
		t.Fatalf("expected AgentParticipant() to be housewife, got %v", got)
	}
	if got := r.ParticipantByID("agent:scout"); got == nil || got.ID != "agent:scout" {
		t.Fatalf("expected ParticipantByID(scout) to be scout, got %v", got)
	}
	if got := r.ParticipantByID("nonexistent"); got != nil {
		t.Fatalf("expected ParticipantByID(nonexistent) to be nil, got %v", got)
	}

	// Room with only agents: no user participant
	agentOnly := Room{Participants: []Actor{housewife, scout}}
	if got := agentOnly.UserParticipant(); got != nil {
		t.Fatalf("expected no user participant, got %v", got)
	}
}


func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

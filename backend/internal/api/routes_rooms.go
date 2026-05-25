package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// ---------------------------------------------------------------------------
// Room routes
// ---------------------------------------------------------------------------

func registerRoomRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "create-room",
		Method:      http.MethodPost,
		Path:        "/api/rooms",
		Summary:     "Create a new room",
		Tags:        []string{"Rooms"},
	}, func(ctx context.Context, input *CreateRoomRequest) (*CreateRoomResponse, error) {
		return svc.createRoom(ctx, input)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-rooms",
		Method:      http.MethodGet,
		Path:        "/api/rooms",
		Summary:     "List rooms",
		Tags:        []string{"Rooms"},
	}, func(ctx context.Context, input *ListRoomsRequest) (*ListRoomsResponse, error) {
		return svc.listRooms(ctx, input)
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-room",
		Method:      http.MethodGet,
		Path:        "/api/rooms/{room_id}",
		Summary:     "Get room details",
		Tags:        []string{"Rooms"},
	}, func(ctx context.Context, input *GetRoomRequest) (*GetRoomResponse, error) {
		return svc.getRoom(ctx, input)
	})
}

// ---------------------------------------------------------------------------
// Room handlers
// ---------------------------------------------------------------------------

func (svc *Service) createRoom(ctx context.Context, input *CreateRoomRequest) (*CreateRoomResponse, error) {
	// Resolve participants from IDs
	participants, err := svc.resolveParticipants(input.Body.ParticipantIDs)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	// Default clearance ceiling
	clearanceCeiling := input.Body.ClearanceCeiling
	if clearanceCeiling == 0 {
		clearanceCeiling = 5
	}

	// Build room
	r := room.Room{
		ID:               uuid.New().String(),
		Name:             input.Body.Name,
		Participants:     participants,
		ClearanceCeiling: clearanceCeiling,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}

	// Persist room
	if err := svc.store.CreateRoom(ctx, &r); err != nil {
		return nil, huma.Error500InternalServerError("failed to create room: " + err.Error())
	}

	// Create transcript file (will be reopened by dispatcher on first append)
	writer, err := room.NewTranscriptWriter(svc.paths.RoomsDir(), r.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to create transcript: " + err.Error())
	}
	writer.Close()

	resp := &CreateRoomResponse{}
	resp.Body = toRoomResponse(r)
	return resp, nil
}

func (svc *Service) listRooms(ctx context.Context, input *ListRoomsRequest) (*ListRoomsResponse, error) {
	opts := room.ListOpts{
		Participant: input.Participant,
		Limit:       input.Limit,
		Offset:      input.Offset,
	}

	rooms, err := svc.store.ListRooms(ctx, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list rooms: " + err.Error())
	}

	resp := &ListRoomsResponse{}
	resp.Body.Rooms = make([]RoomResponse, len(rooms))
	for i, r := range rooms {
		resp.Body.Rooms[i] = toRoomResponse(r)
	}
	return resp, nil
}

func (svc *Service) getRoom(ctx context.Context, input *GetRoomRequest) (*GetRoomResponse, error) {
	r, err := svc.store.GetRoom(ctx, input.RoomID)
	if err != nil {
		return nil, huma.Error404NotFound("room not found")
	}

	resp := &GetRoomResponse{}
	resp.Body = toRoomResponse(*r)
	return resp, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveParticipants converts actor ID strings to room.Actor values.
func (svc *Service) resolveParticipants(ids []string) ([]room.Actor, error) {
	actors := make([]room.Actor, 0, len(ids))

	for _, id := range ids {
		// Format: "user:<name>" or "agent:<name>"
		if len(id) < 6 {
			return nil, fmt.Errorf("invalid actor ID: %q", id)
		}

		switch {
		case id[:5] == "user:":
			name := id[5:]
			if name == "" {
				return nil, fmt.Errorf("invalid user ID: %q", id)
			}
			actors = append(actors, room.Actor{
				ID:        id,
				Type:      room.ActorUser,
				Clearance: 5, // root user — F11 will make this dynamic
				Name:      name,
			})

		case id[:6] == "agent:":
			name := id[6:]
			if name == "" {
				return nil, fmt.Errorf("invalid agent ID: %q", id)
			}
			ag := svc.agentMgr.Get(name)
			if ag == nil {
				return nil, fmt.Errorf("agent %q not found", name)
			}
			actors = append(actors, room.Actor{
				ID:        id,
				Type:      room.ActorAgent,
				Clearance: ag.Definition.Clearance,
				Name:      ag.Definition.Name,
			})

		default:
			return nil, fmt.Errorf("invalid actor ID: %q", id)
		}
	}

	return actors, nil
}

// toRoomResponse converts an internal room.Room to the API response type.
func toRoomResponse(r room.Room) RoomResponse {
	participants := make([]ActorResponse, len(r.Participants))
	for i, p := range r.Participants {
		participants[i] = toActorResponse(p)
	}

	return RoomResponse{
		ID:               r.ID,
		Name:             r.Name,
		Participants:     participants,
		ClearanceCeiling: r.ClearanceCeiling,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

// toActorResponse converts an internal room.Actor to the API response type.
func toActorResponse(a room.Actor) ActorResponse {
	return ActorResponse{
		ID:        a.ID,
		Type:      string(a.Type),
		Clearance: a.Clearance,
		Name:      a.Name,
	}
}

// toMessageResponse converts an internal room.Message to the API response type.
func toMessageResponse(m room.Message) MessageResponse {
	return MessageResponse{
		ID:           m.ID,
		Timestamp:    m.Timestamp,
		RoomID:       m.RoomID,
		Sender:       toActorResponse(m.Sender),
		ClearanceTag: m.ClearanceTag,
		Type:         string(m.Type),
		Content:      m.Content,
		Usage:        m.Usage,
		ToolCalls:    m.ToolCalls,
		ToolCallID:   m.ToolCallID,
		ToolName:     m.ToolName,
	}
}

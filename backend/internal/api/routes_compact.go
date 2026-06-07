package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// CompactRoomRequest triggers on-demand compaction of one agent's transcript in
// a room. target is optional; 0 (omitted) uses the configured default.
type CompactRoomRequest struct {
	RoomID int64 `path:"room_id" validate:"required" doc:"Room ID"`
	Body   struct {
		Agent  string `json:"agent"            validate:"required" doc:"Agent whose transcript to compact" example:"housewife"`
		Target int    `json:"target,omitempty"                     doc:"Target size in bytes; omit for the configured default"`
	}
}

// CompactRoomResponse confirms the compaction was requested.
type CompactRoomResponse struct {
	Body struct {
		Compacted bool `json:"compacted"`
	}
}

func registerCompactRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "compact-room",
		Method:      http.MethodPost,
		Path:        "/api/rooms/{room_id}/compact",
		Summary:     "Compact an agent's transcript in a room",
		Tags:        []string{"Rooms"},
	}, func(ctx context.Context, input *CompactRoomRequest) (*CompactRoomResponse, error) {
		return svc.compactRoom(ctx, input)
	})
}

func (svc *Service) compactRoom(ctx context.Context, input *CompactRoomRequest) (*CompactRoomResponse, error) {
	if svc.compactor == nil {
		return nil, huma.Error501NotImplemented("compaction is not available")
	}
	if _, err := svc.rooms.GetRoom(ctx, input.RoomID); err != nil {
		return nil, huma.Error404NotFound("room not found")
	}
	ag := svc.agentMgr.Get(input.Body.Agent)
	if ag == nil {
		return nil, huma.Error404NotFound("agent not found")
	}
	if err := svc.compactor.Compact(ctx, ag, input.RoomID, input.Body.Target); err != nil {
		return nil, huma.Error500InternalServerError("compact transcript", err)
	}
	resp := &CompactRoomResponse{}
	resp.Body.Compacted = true
	return resp, nil
}

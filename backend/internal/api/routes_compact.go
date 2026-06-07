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

// CompactionStatsRequest selects the agent whose compaction state to report for
// a room.
type CompactionStatsRequest struct {
	RoomID int64  `path:"room_id"  validate:"required" doc:"Room ID"`
	Agent  string `query:"agent"   validate:"required" doc:"Agent whose transcript to inspect"`
}

// CompactionStatsResponse reports the live-transcript size and cursor position
// that drive compaction, plus the configured thresholds.
type CompactionStatsResponse struct {
	Body struct {
		Offset   int64 `json:"offset"`
		Messages int   `json:"messages"`
		Bytes    int   `json:"bytes"`
		Trigger  int   `json:"trigger"`
		Target   int   `json:"target"`
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

	huma.Register(api, huma.Operation{
		OperationID: "get-compaction-stats",
		Method:      http.MethodGet,
		Path:        "/api/rooms/{room_id}/compaction",
		Summary:     "Report an agent's compaction state for a room",
		Tags:        []string{"Rooms"},
	}, func(ctx context.Context, input *CompactionStatsRequest) (*CompactionStatsResponse, error) {
		return svc.compactionStats(ctx, input)
	})
}

func (svc *Service) compactionStats(
	ctx context.Context,
	input *CompactionStatsRequest,
) (*CompactionStatsResponse, error) {
	if svc.compactor == nil {
		return nil, huma.Error501NotImplemented("compaction is not available")
	}
	if _, err := svc.rooms.GetRoom(ctx, input.RoomID); err != nil {
		return nil, huma.Error404NotFound("room not found")
	}
	ag := svc.agentMgr.Get(input.Agent)
	if ag == nil {
		return nil, huma.Error404NotFound("agent not found")
	}
	stats, err := svc.compactor.Stats(ctx, ag, input.RoomID)
	if err != nil {
		return nil, huma.Error500InternalServerError("read compaction stats", err)
	}
	resp := &CompactionStatsResponse{}
	resp.Body.Offset = stats.Offset
	resp.Body.Messages = stats.Messages
	resp.Body.Bytes = stats.Bytes
	resp.Body.Trigger = stats.Trigger
	resp.Body.Target = stats.Target
	return resp, nil
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

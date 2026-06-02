package api

import (
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// ---------------------------------------------------------------------------
// Room endpoints
// ---------------------------------------------------------------------------

// CreateRoomRequest is the input for POST /api/rooms.
type CreateRoomRequest struct {
	Body struct {
		ParticipantIDs []string `json:"participant_ids" validate:"required,min=1" doc:"Actor IDs (user:<name> or agent:<name>)"`
		Name           string   `json:"name,omitempty"    doc:"Human-readable room name" example:"Alice's Kitchen"`
		Clearance      int      `json:"clearance"         validate:"min=1"         doc:"Room clearance tier (default 5)" example:"5"`
	}
}

// CreateRoomResponse is the output for POST /api/rooms.
type CreateRoomResponse struct {
	Body RoomResponse
}

// GetRoomRequest is the input for GET /api/rooms/{room_id}.
type GetRoomRequest struct {
	RoomID int64 `path:"room_id" validate:"required" doc:"Room ID"`
}

// GetRoomResponse is the output for GET /api/rooms/{room_id}.
type GetRoomResponse struct {
	Body RoomResponse
}

// ListRoomsRequest is the input for GET /api/rooms.
type ListRoomsRequest struct {
	Participant string `query:"participant" doc:"Filter by participant actor ID" example:"user:alice"`
	Limit       int    `query:"limit"       default:"50" validate:"min=1,max=200" doc:"Max rooms to return"`
	Offset      int    `query:"offset"      default:"0"  validate:"min=0"         doc:"Offset for pagination"`
}

// ListRoomsResponse is the output for GET /api/rooms.
type ListRoomsResponse struct {
	Body struct {
		Rooms []RoomResponse `json:"rooms"`
	}
}

// ---------------------------------------------------------------------------
// Message endpoints
// ---------------------------------------------------------------------------

// SendMessageRequest is the input for POST /api/rooms/{room_id}/messages.
type SendMessageRequest struct {
	RoomID int64 `path:"room_id" validate:"required" doc:"Room ID"`
	Body   struct {
		Sender  string `json:"sender"  validate:"required" doc:"Actor ID of the sender" example:"user:alice"`
		Content string `json:"content" validate:"required" doc:"Message body"`
	}
}

// SendMessageResponse is the output for POST /api/rooms/{room_id}/messages.
type SendMessageResponse struct {
	Body MessageResponse
}

// ListMessagesRequest is the input for GET /api/rooms/{room_id}/messages.
type ListMessagesRequest struct {
	RoomID int64  `path:"room_id" validate:"required" doc:"Room ID"`
	Limit  int    `query:"limit"  default:"50" validate:"min=1,max=200" doc:"Max messages to return"`
	Before string `query:"before" doc:"Cursor: only messages before this RFC3339 timestamp"`
}

// ListMessagesResponse is the output for GET /api/rooms/{room_id}/messages.
type ListMessagesResponse struct {
	Body struct {
		Messages []MessageResponse `json:"messages"`
	}
}

// ---------------------------------------------------------------------------
// Context preview endpoint
// ---------------------------------------------------------------------------

// GetContextPreviewRequest is the input for GET /api/rooms/{room_id}/agents/{agent_name}/context-preview.
type GetContextPreviewRequest struct {
	RoomID               int64  `path:"room_id"               validate:"required" doc:"Room ID"`
	AgentName            string `path:"agent_name"            validate:"required" doc:"Agent name"`
	IncludeCrossRoom     bool   `query:"include_cross_room"     default:"true" doc:"Include cross-room feed"`
	IncludeInterjections bool   `query:"include_interjections" default:"false" doc:"Include queued interjections"`
}

// GetContextPreviewResponse is the output for GET /api/rooms/{room_id}/agents/{agent_name}/context-preview.
type GetContextPreviewResponse struct {
	Body struct {
		Messages         []ContextMessageResponse `json:"messages"`
		Tools            []ContextToolResponse    `json:"tools" doc:"Tool definitions the agent would receive"`
		CompactionOffset int                      `json:"compaction_offset" doc:"Current compaction cursor offset"`
		AssembledBytes   int                      `json:"assembled_bytes" doc:"Total byte size of assembled message content"`
	}
}

// ContextToolResponse is a tool definition as surfaced in a context preview.
type ContextToolResponse struct {
	Name      string `json:"name" doc:"Tool name"`
	Resource  string `json:"resource" doc:"Tool FRSN resource identifier"`
	Clearance int    `json:"clearance" doc:"Tool data-classification clearance tier"`
}

// ---------------------------------------------------------------------------
// Agent endpoints
// ---------------------------------------------------------------------------

// ListAgentsResponse is the output for GET /api/agents.
type ListAgentsResponse struct {
	Body struct {
		Agents []AgentResponse `json:"agents"`
	}
}

// GetAgentRequest is the input for GET /api/agents/{name}.
type GetAgentRequest struct {
	Name string `path:"name" validate:"required" doc:"Agent name"`
}

// GetAgentResponse is the output for GET /api/agents/{name}.
type GetAgentResponse struct {
	Body AgentResponse
}

// ---------------------------------------------------------------------------
// User endpoint
// ---------------------------------------------------------------------------

// GetMeRequest is the input for GET /api/me.
type GetMeRequest struct{}

// GetMeResponse is the output for GET /api/me.
type GetMeResponse struct {
	Body UserResponse
}

// ---------------------------------------------------------------------------
// Shared response types
// ---------------------------------------------------------------------------

// RoomResponse is the API representation of a room.
type RoomResponse struct {
	ID           int64           `json:"id"                doc:"Unique room ID" example:"1"`
	Name         string          `json:"name"              doc:"Human-readable room name" example:"Alice's Kitchen"`
	Participants []ActorResponse `json:"participants"      doc:"Room participants"`
	Clearance    int             `json:"clearance"         doc:"Room clearance tier" example:"5"`
	CreatedAt    time.Time       `json:"created_at"        doc:"Room creation time"`
	UpdatedAt    time.Time       `json:"updated_at"        doc:"Last update time"`
}

// MessageResponse is the API representation of a message.
type MessageResponse struct {
	ID           int64                 `json:"id"             doc:"Message ID"`
	Timestamp    time.Time             `json:"timestamp"      doc:"Message timestamp"`
	RoomID       int64                 `json:"room_id"        doc:"Room this message belongs to"`
	Sender       ActorResponse         `json:"sender"         doc:"Actor who sent this message"`
	ClearanceTag int                   `json:"clearance_tag"  doc:"Classification tier"`
	Type         string                `json:"type"           doc:"Message type" example:"message"`
	Content      string                `json:"content"        doc:"Message body"`
	Usage        *room.Usage           `json:"usage,omitempty" doc:"Token usage for agent responses"`
	ToolCalls    []room.ToolCallRecord `json:"tool_calls,omitempty" doc:"Structured tool calls for tool_call messages"`
	ToolCallID   string                `json:"tool_call_id,omitempty" doc:"Correlating tool call ID for tool_result messages"`
	ToolName     string                `json:"tool_name,omitempty" doc:"Tool identifier for tool_result messages"`
}

// ContextMessageResponse is a simplified message representation for context previews.
type ContextMessageResponse struct {
	Role    string `json:"role" doc:"Message role" example:"user"`
	Content string `json:"content" doc:"Message content"`
	Name    string `json:"name,omitempty" doc:"Tool or actor name if provided"`
}

// ActorResponse is the API representation of an actor.
type ActorResponse struct {
	ID        string `json:"id"          doc:"Actor identifier" example:"user:alice"`
	Type      string `json:"type"        doc:"Actor type" example:"user"`
	Clearance int    `json:"clearance"   doc:"Clearance tier" example:"5"`
	Name      string `json:"name"        doc:"Display name" example:"Alice"`
}

// AgentResponse is the API representation of an agent.
type AgentResponse struct {
	Name            string `json:"name"              doc:"Agent name" example:"housewife"`
	RoleDescription string `json:"role_description"  doc:"Agent role description"`
	PrimaryModel    string `json:"primary_model"     doc:"Primary model ID"`
	RoutineModel    string `json:"routine_model"     doc:"Routine model ID"`
	SensitiveModel  string `json:"sensitive_model"   doc:"Sensitive model ID"`
	Clearance       int    `json:"clearance"          doc:"Agent clearance tier" example:"5"`
	Active          bool   `json:"active"            doc:"Whether agent is currently loaded"`
}

// ---------------------------------------------------------------------------
// Confirmation endpoints
// ---------------------------------------------------------------------------

// ListConfirmationsRequest is the input for GET /api/rooms/{room_id}/confirmations.
type ListConfirmationsRequest struct {
	RoomID int64 `path:"room_id" validate:"required" doc:"Room ID"`
}

// ListConfirmationsResponse is the output for GET /api/rooms/{room_id}/confirmations.
type ListConfirmationsResponse struct {
	Body struct {
		Confirmations []ConfirmationResponse `json:"confirmations"`
	}
}

// RespondConfirmationRequest is the input for POST /api/rooms/{room_id}/confirmations/{node_id}.
type RespondConfirmationRequest struct {
	RoomID int64  `path:"room_id" validate:"required" doc:"Room ID"`
	NodeID string `path:"node_id" validate:"required" doc:"Confirmation node ID"`
	Body   struct {
		// Action is one of: "allow", "deny", "revise".
		// For "allow" with edited arguments, set Args.
		// For "revise", set Feedback.
		Action   string `json:"action"             validate:"required" doc:"allow | deny | revise"`
		Args     string `json:"args,omitempty"     doc:"Edited JSON arguments (action=allow only)"`
		Feedback string `json:"feedback,omitempty" doc:"Revision guidance for the agent (action=revise only)"`
	}
}

// RespondConfirmationResponse is the output for POST /api/rooms/{room_id}/confirmations/{node_id}.
type RespondConfirmationResponse struct{}

// ConfirmationResponse is the API representation of a pending confirmation.
type ConfirmationResponse struct {
	NodeID    string `json:"node_id"    doc:"Unique node ID — used in the respond endpoint"`
	AgentName string `json:"agent_name" doc:"Name of the agent waiting for confirmation"`
	RoomID    int64  `json:"room_id"    doc:"Room the confirmation belongs to"`
	ToolName  string `json:"tool_name"  doc:"Tool the agent wants to call"`
	Args      string `json:"args"       doc:"JSON-encoded tool arguments"`
	Reason    string `json:"reason"     doc:"Why confirmation is required (e.g. blp_write_down, permission_confirmation)"`
}

// UserResponse is the API representation of the authenticated user.
type UserResponse struct {
	ID   string `json:"id"   doc:"User ID" example:"local"`
	Name string `json:"name" doc:"Display name" example:"User"`
	Role string `json:"role" doc:"User role" example:"owner"`
}

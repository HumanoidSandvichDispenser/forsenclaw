package memory

import (
	"context"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// ContextAssembler assembles the full context window for an agent invocation.
// This is a placeholder interface for F3 — the full implementation requires
// rooms (F4), clearance (F5), and MCP tools (F6) which are not yet built.
//
// When those features land, a concrete implementation will fill in the
// RAG retrieval, room history, MCP tool schemas, turn budget, and RFC payload.
type ContextAssembler interface {
	Assemble(ctx context.Context, agent *agent.Agent, req AssembleRequest) (*AssembledContext, error)
}

// AssembleRequest captures the inputs needed for context assembly.
type AssembleRequest struct {
	// RoomID is the target room for this invocation.
	// Empty for proactive/system RFCs.
	RoomID string

	// Messages are the recent messages in the room (or the trigger payload
	// for proactive RFCs).
	Messages []string

	// AvailableTools are the MCP tool schemas this agent may use.
	// Populated by the MCP integration layer (F6).
	AvailableTools []string

	// TurnBudget is the remaining turn budget notice.
	TurnBudget string
}

// AssembledContext is the ordered context window ready for model inference.
type AssembledContext struct {
	// SystemPrompt is the agent's role description.
	SystemPrompt string

	// Memory is the injected MEMORY.md content (possibly truncated).
	Memory string

	// DailyNotes are today's and yesterday's notes.
	DailyNotes []string

	// RAGResults are retrieved chunks from the search index.
	RAGResults []string

	// ToolSchemas are the permitted MCP tool schemas.
	ToolSchemas []string

	// RoomHistory is the recent transcript from the room.
	RoomHistory []string

	// TurnBudget is the budget notice.
	TurnBudget string

	// RFC is the actual request payload.
	RFC string

	// Messages is the final ordered list of messages for the model API call.
	Messages []inference.Message
}

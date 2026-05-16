package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// Assembler assembles the full context window for an agent invocation. It
// reads MEMORY.md, daily notes, and room history, then composes them into a
// ordered list of inference.Messages ready for the model API.
type Assembler struct {
	paths     *paths.Paths
	memBudget int // token budget for MEMORY.md injection
}

// NewAssembler creates a context assembler. If memBudget is 0, a default of
// 4096 tokens is used.
func NewAssembler(p *paths.Paths, memBudget int) *Assembler {
	if memBudget <= 0 {
		memBudget = 4096
	}
	return &Assembler{paths: p, memBudget: memBudget}
}

// CrossRoomMessage is a message from another room, labeled with its room ID.
type CrossRoomMessage struct {
	Message room.Message
	RoomID  string
}

// AssembleRequest captures the inputs needed for context assembly.
type AssembleRequest struct {
	// RoomID is the target room for this invocation. Empty for proactive/system RFCs.
	RoomID string

	// CrossRoomFeed is recent messages from other rooms the agent participates in.
	CrossRoomFeed []CrossRoomMessage

	// CurrentRoomHistory is the windowed tail of the target room's transcript.
	CurrentRoomHistory []room.Message

	// AvailableTools are the MCP tool schemas this agent may use.
	// Populated by the MCP integration layer (F6).
	AvailableTools []string

	// TurnBudget is the remaining turn budget notice.
	TurnBudget string

	// Interjections are messages that arrived while the agent was responding.
	Interjections []room.Message

	// RAGChunks are retrieved chunks from the search index. Always nil in v1.
	RAGChunks []string
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

	// CrossRoomFeed is the formatted recent transcript from other rooms.
	CrossRoomFeed []string

	// CurrentRoomHistory is the formatted windowed tail of the target room.
	CurrentRoomHistory []string

	// TurnBudget is the budget notice.
	TurnBudget string

	// RFC is the actual request payload.
	RFC string

	// Messages is the final ordered list of messages for the model API call.
	Messages []inference.Message
}

// Assemble builds the context window for an agent invocation. The returned
// Messages slice is ordered as:
//
//	[0]   System message: role description + memory + daily notes
//	[1..N] Alternating user/assistant from room history
//	[N+1] User message: RFC payload (interjections + current task)
func (a *Assembler) Assemble(ctx context.Context, ag *agent.Agent, req AssembleRequest) (*AssembledContext, error) {
	if ag == nil {
		return nil, fmt.Errorf("agent is nil")
	}

	result := &AssembledContext{
		SystemPrompt:  ag.Definition.RoleDescription,
		ToolSchemas:   req.AvailableTools,
		TurnBudget:    req.TurnBudget,
		RAGResults:    []string{},
	}

	// 1. MEMORY.md
	memContent, err := ReadMemory(a.paths.AgentDataDir(ag.Name()))
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	if memContent != "" {
		memContent = Truncate(memContent, TruncateOptions{
			Budget:    a.memBudget,
			Counter:   DefaultCounter,
			FromStart: false,
		})
	}
	result.Memory = memContent

	// 2. Daily notes (today + yesterday)
	if ag.Definition.FeatureFlags.DailyNotes {
		notes, err := ReadDailyNotes(a.paths.AgentDataDir(ag.Name()), true)
		if err != nil {
			return nil, fmt.Errorf("read daily notes: %w", err)
		}
		for _, n := range notes {
			result.DailyNotes = append(result.DailyNotes, n.Content)
		}
	}

	// 3. Cross-room feed → formatted strings for AssembledContext
	for _, crm := range req.CrossRoomFeed {
		result.CrossRoomFeed = append(result.CrossRoomFeed, fmt.Sprintf("[#%s] %s: %s", crm.RoomID, crm.Message.Sender.Name, crm.Message.Content))
	}

	// 4. Current room history → formatted strings for AssembledContext
	for _, m := range req.CurrentRoomHistory {
		result.CurrentRoomHistory = append(result.CurrentRoomHistory, fmt.Sprintf("%s: %s", m.Sender.Name, m.Content))
	}

	// 5. Build the inference.Messages slice
	messages, err := a.buildMessages(ag, req, result)
	if err != nil {
		return nil, fmt.Errorf("build messages: %w", err)
	}
	result.Messages = messages

	return result, nil
}

// buildMessages composes the final []inference.Message slice from the
// assembled components.
func (a *Assembler) buildMessages(ag *agent.Agent, req AssembleRequest, assembled *AssembledContext) ([]inference.Message, error) {
	var msgs []inference.Message

	// System message: role description + memory + daily notes + RAG + tool schemas
	systemContent := &strings.Builder{}
	systemContent.WriteString(assembled.SystemPrompt)

	if assembled.Memory != "" {
		systemContent.WriteString("\n\n## Memory\n\n")
		systemContent.WriteString(assembled.Memory)
	}

	if len(assembled.DailyNotes) > 0 {
		systemContent.WriteString("\n\n## Daily Notes\n\n")
		for _, note := range assembled.DailyNotes {
			systemContent.WriteString(note)
			systemContent.WriteString("\n\n")
		}
	}

	if len(assembled.RAGResults) > 0 {
		systemContent.WriteString("\n\n## Relevant Context\n\n")
		for _, r := range assembled.RAGResults {
			systemContent.WriteString(r)
			systemContent.WriteString("\n\n")
		}
	}

	if len(assembled.ToolSchemas) > 0 {
		systemContent.WriteString("\n\n## Available Tools\n\n")
		for _, tool := range assembled.ToolSchemas {
			systemContent.WriteString(tool)
			systemContent.WriteString("\n\n")
		}
	}

	if assembled.TurnBudget != "" {
		systemContent.WriteString("\n\n")
		systemContent.WriteString(assembled.TurnBudget)
	}

	if systemContent.Len() > 0 {
		msgs = append(msgs, inference.Message{
			Role:    inference.RoleSystem,
			Content: systemContent.String(),
		})
	}

	// Cross-room feed (Tier 2): messages from other rooms, chronologically
	if len(req.CrossRoomFeed) > 0 {
		var feedContent strings.Builder
		feedContent.WriteString("## Cross-room activity\n\n")
		for _, crm := range req.CrossRoomFeed {
			relTime := formatRelativeTime(crm.Message.Timestamp)
			feedContent.WriteString(fmt.Sprintf("[#%s — %s] %s: %s\n\n", crm.RoomID, relTime, crm.Message.Sender.Name, crm.Message.Content))
		}
		msgs = append(msgs, inference.Message{
			Role:    inference.RoleUser,
			Content: feedContent.String(),
		})
	}

	// Current room history (Tier 3): convert room.Message to inference.Message
	for _, m := range req.CurrentRoomHistory {
		role := inference.RoleUser
		if m.Sender.IsAgent() {
			// If the sender is the agent being invoked, mark as assistant
			if m.Sender.ID == "agent:"+ag.Name() {
				role = inference.RoleAssistant
			} else {
				// Another agent speaking — treat as user message with name
				role = inference.RoleUser
			}
		}
		msgs = append(msgs, inference.Message{
			Role:    role,
			Content: fmt.Sprintf("%s: %s", m.Sender.Name, m.Content),
			Name:    m.Sender.Name,
		})
	}

	// RFC payload: interjections + current task
	var rfcContent strings.Builder

	if len(req.Interjections) > 0 {
		rfcContent.WriteString("## Interjections\n\n")
		for _, m := range req.Interjections {
			rfcContent.WriteString(fmt.Sprintf("%s: %s\n\n", m.Sender.Name, m.Content))
		}
	}

	rfcContent.WriteString("## Task\n\n")
	// The RFC payload messages are the current task
	for _, m := range req.CurrentRoomHistory {
		// The last message is typically the one triggering this RFC
		// For FreeForm, it's the user's most recent message
		if m.Sender.IsUser() {
			rfcContent.WriteString(fmt.Sprintf("%s: %s", m.Sender.Name, m.Content))
		}
	}

	if rfcContent.Len() > 0 {
		msgs = append(msgs, inference.Message{
			Role:    inference.RoleUser,
			Content: rfcContent.String(),
		})
	}

	return msgs, nil
}

// formatRelativeTime returns a human-readable relative time string.
func formatRelativeTime(t time.Time) string {
	delta := time.Since(t)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	case delta < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

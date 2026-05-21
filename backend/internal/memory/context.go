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
// structured AssembledContext ready for model inference.
type Assembler struct {
	paths     *paths.Paths
	memBudget int // default token budget for MEMORY.md injection
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

	// AvailableTools are the MCP tool schemas this agent may use,
	// as pre-formatted strings for XML tool mode.
	AvailableTools []string

	// ToolDefinitions are the structured tool definitions for native tool calling.
	ToolDefinitions []inference.ToolDefinition

	// TurnBudget is the remaining turn budget notice.
	TurnBudget string

	// Interjections are messages that arrived while the agent was responding.
	Interjections []room.Message

	// RAGChunks are retrieved chunks from the search index. Always nil in v1.
	RAGChunks []string
}

// AssembledContext is the structured context window ready for model inference.
type AssembledContext struct {
	// SystemPrompt is the agent's role description.
	SystemPrompt string

	// Memory is the injected MEMORY.md content (possibly truncated).
	Memory string

	// DailyNotes are today's and yesterday's notes.
	DailyNotes []string

	// RAGResults are retrieved chunks from the search index.
	RAGResults []string

	// ToolSchemas are the permitted MCP tool schemas as pre-formatted strings (XML mode).
	ToolSchemas []string

	// ToolDefinitions are the structured tool definitions for native tool calling.
	ToolDefinitions []inference.ToolDefinition

	// CrossRoomFeed is the formatted recent transcript from other rooms.
	CrossRoomFeed []string

	// CurrentRoomHistory is the formatted windowed tail of the target room.
	CurrentRoomHistory []string

	// TurnBudget is the budget notice.
	TurnBudget string

	// History is the pre-role-assigned room transcript (all but the last
	// message, which becomes the RFC). It is consumed by the tool loop as the
	// starting turnHistory and is NOT included in ContextPayload by
	// ToContextPayload — the caller sets payload.History explicitly.
	History []inference.HistoryMessage

	// RFC is the final user request payload (interjections + last message).
	RFC string
}

// ToContextPayload converts the assembled context into an inference.ContextPayload
// ready to pass to a provider. TurnBudget is pre-appended to the system prompt.
func (a *AssembledContext) ToContextPayload(model string) inference.ContextPayload {
	systemPrompt := a.SystemPrompt
	if a.TurnBudget != "" {
		systemPrompt += "\n\n" + a.TurnBudget
	}
	return inference.ContextPayload{
		Model:           model,
		SystemPrompt:    systemPrompt,
		Memory:          a.Memory,
		DailyNotes:      a.DailyNotes,
		RAGResults:      a.RAGResults,
		ToolSchemas:     a.ToolSchemas,
		ToolDefinitions: a.ToolDefinitions,
		CrossRoomFeed:   a.CrossRoomFeed,
		RFC:             a.RFC,
	}
}

func (a *Assembler) memoryBudgetForAgent(ag *agent.Agent) int {
	if ag != nil && ag.Definition != nil && ag.Definition.MemoryBudget > 0 {
		return ag.Definition.MemoryBudget
	}
	return a.memBudget
}

// Assemble builds the context window for an agent invocation.
func (a *Assembler) Assemble(ctx context.Context, ag *agent.Agent, req AssembleRequest) (*AssembledContext, error) {
	if ag == nil {
		return nil, fmt.Errorf("agent is nil")
	}

	result := &AssembledContext{
		SystemPrompt:    ag.Definition.RoleDescription,
		ToolSchemas:     req.AvailableTools,
		ToolDefinitions: req.ToolDefinitions,
		TurnBudget:      req.TurnBudget,
		RAGResults:      []string{},
	}

	// 1. MEMORY.md
	memContent, err := ReadMemory(a.paths.AgentDataDir(ag.Name()))
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	if memContent != "" {
		memContent = Truncate(memContent, TruncateOptions{
			Budget:    a.memoryBudgetForAgent(ag),
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

	// 3. Cross-room feed → formatted strings with relative timestamps
	for _, crm := range req.CrossRoomFeed {
		relTime := formatRelativeTime(crm.Message.Timestamp)
		result.CrossRoomFeed = append(result.CrossRoomFeed, fmt.Sprintf("[#%s %s][%s] %s", crm.RoomID, relTime, crm.Message.Sender.Name, crm.Message.Content))
	}

	// 4. Current room history → formatted strings for size tracking
	// Tool call/result messages are excluded from formatted strings; they only
	// appear in the structured History slice below.
	for _, m := range req.CurrentRoomHistory {
		if m.Type == room.MessageToolCall || m.Type == room.MessageToolResult {
			continue
		}
		result.CurrentRoomHistory = append(result.CurrentRoomHistory, fmt.Sprintf("%s: %s", m.Sender.Name, m.Content))
	}

	// 5. Build pre-role-assigned History (all messages except the last text
	//    message, which becomes the RFC). Tool call/result messages are
	//    reconstructed as structured HistoryMessages for provider adapters.
	historyMsgs := req.CurrentRoomHistory
	if len(historyMsgs) > 0 {
		historyMsgs = historyMsgs[:len(historyMsgs)-1]
	}
	for _, m := range historyMsgs {
		switch m.Type {
		case room.MessageToolCall:
			if len(m.ToolCalls) > 0 {
				// Native mode: reconstruct assistant message with structured tool calls.
				toolCalls := make([]inference.ToolCallWire, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					toolCalls[i] = inference.ToolCallWire{
						ID:   tc.ID,
						Type: "function",
						Function: inference.ToolFunctionWire{
							Name:      tc.ToolName,
							Arguments: tc.Arguments,
						},
					}
				}
				result.History = append(result.History, inference.HistoryMessage{
					Role:      inference.RoleAssistant,
					Content:   m.Content,
					ToolCalls: toolCalls,
				})
			} else {
				// XML mode: assistant message contains raw response with XML tool calls.
				result.History = append(result.History, inference.HistoryMessage{
					Role:    inference.RoleAssistant,
					Content: m.Content,
				})
			}
		case room.MessageToolResult:
			if m.ToolCallID != "" {
				// Native mode: tool result correlated by ID.
				result.History = append(result.History, inference.HistoryMessage{
					Role:       inference.RoleTool,
					ToolCallID: m.ToolCallID,
					Name:       m.ToolName,
					Content:    m.Content,
				})
			} else {
				// XML mode: tool result as user message with XML content.
				result.History = append(result.History, inference.HistoryMessage{
					Role:    inference.RoleUser,
					Name:    m.ToolName,
					Content: m.Content,
				})
			}
		default:
			role := inference.RoleUser
			if m.Sender.IsAgent() && m.Sender.ID == "agent:"+ag.Name() {
				role = inference.RoleAssistant
			}
			result.History = append(result.History, inference.HistoryMessage{
				Role:    role,
				Content: fmt.Sprintf("%s: %s", m.Sender.Name, m.Content),
				Name:    m.Sender.Name,
			})
		}
	}

	// 6. Build RFC: interjections + last message from room history.
	var rfcContent strings.Builder
	if len(req.Interjections) > 0 {
		rfcContent.WriteString("# Interjections (requests during non turns)\n\n")
		for _, m := range req.Interjections {
			rfcContent.WriteString(fmt.Sprintf("%s: %s\n\n", m.Sender.Name, m.Content))
		}
	}

	// Walk backwards to find the last non-tool message so the RFC doesn't
	// accidentally contain <tool_response> XML or native tool call JSON.
	for i := len(req.CurrentRoomHistory) - 1; i >= 0; i-- {
		m := req.CurrentRoomHistory[i]
		if m.Type == room.MessageToolCall || m.Type == room.MessageToolResult {
			continue
		}
		rfcContent.WriteString(fmt.Sprintf("# Request/Message Sent by %s:\n\n", m.Sender.Name))
		rfcContent.WriteString(m.Content)
		break
	}
	result.RFC = rfcContent.String()

	return result, nil
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

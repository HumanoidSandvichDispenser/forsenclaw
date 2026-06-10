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
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
)

// Assembler assembles the full context window for an agent invocation. It
// reads MEMORY.md, daily notes, and room history, then composes them into a
// ContextPayload ready for model inference.
//
// Assembler satisfies agent.Assembler.
type Assembler struct {
	paths     *paths.Paths
	memBudget int
	rooms     store.RoomRepository
	messages  store.MessageRepository
}

// NewAssembler creates a context assembler. If memBudget is 0, a default of
// 4096 tokens is used.
func NewAssembler(p *paths.Paths, memBudget int, rooms store.RoomRepository, messages store.MessageRepository) *Assembler {
	if memBudget <= 0 {
		memBudget = 4096
	}
	return &Assembler{paths: p, memBudget: memBudget, rooms: rooms, messages: messages}
}

// Assemble satisfies agent.Assembler. It loads room history from the store,
// applies clearance filtering, annotates lower-clearance messages with a soft
// Biba trust label, injects a clearance notice into the system prompt, and
// returns a ContextPayload ready for inference.
func (a *Assembler) Assemble(ctx context.Context, ag *agent.Agent, req agent.Request, tools []inference.ToolDefinition) (inference.ContextPayload, error) {
	var history []room.Message
	// No room context means the agent operates at its configured ceiling.
	effectiveClearance := ag.Definition.Clearance
	var roomName string

	if req.Payload.RoomID != 0 && a.rooms != nil && a.messages != nil {
		var err error
		history, effectiveClearance, roomName, err = a.loadRoomHistory(ctx, ag, req.Payload.RoomID)
		if err != nil {
			return inference.ContextPayload{}, err
		}
	}

	assembled, err := a.assemble(ctx, ag, assembleRequest{
		RoomID:             req.Payload.RoomID,
		EffectiveClearance: effectiveClearance,
		CurrentRoomHistory: history,
		ToolDefinitions:    tools,
	})
	if err != nil {
		return inference.ContextPayload{}, err
	}

	// Inject a room-identity and clearance notice at the top of the system
	// prompt. The room is constant for the whole conversation, so it lives here
	// once rather than per message.
	if req.Payload.RoomID != 0 {
		roomLine := fmt.Sprintf("You are in room #%d.", req.Payload.RoomID)
		if roomName != "" {
			roomLine = fmt.Sprintf("You are in room #%d %q.", req.Payload.RoomID, roomName)
		}
		notice := fmt.Sprintf(
			"%s You are operating at clearance level %d. Higher-clearance context exists but is not available in this context. If a question requires deeper personal context, say so rather than guessing.",
			roomLine, effectiveClearance,
		)
		assembled.SystemPrompt = notice + "\n\n" + assembled.SystemPrompt
	}

	return assembled.toContextPayload(), nil
}

// loadRoomHistory loads the windowed, clearance-filtered tail of a room's
// transcript for an agent. Messages above the agent's effective clearance are
// dropped (structural filter); lower-clearance messages are annotated with a
// soft-Biba trust label. It returns the history, the effective clearance used
// (min(agent.Clearance, room.Clearance)), and the room's display name.
func (a *Assembler) loadRoomHistory(ctx context.Context, ag *agent.Agent, roomID int64) ([]room.Message, int, string, error) {
	r, err := a.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return nil, 0, "", fmt.Errorf("get room: %w", err)
	}
	effectiveClearance := min(ag.Definition.Clearance, r.Clearance)

	offset, err := a.messages.GetCompactionOffset(ctx, ag.Name(), roomID)
	if err != nil {
		return nil, 0, "", fmt.Errorf("get compaction offset: %w", err)
	}

	msgs, err := a.messages.GetMessages(ctx, roomID, store.ReadOpts{
		Limit:        100,
		CompactionID: offset,
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("get messages: %w", err)
	}

	var history []room.Message
	for _, m := range msgs {
		if m.ClearanceTag > effectiveClearance {
			// Structural filter: message above effective clearance is not present.
			continue
		}
		if m.ClearanceTag < effectiveClearance {
			// Soft Biba: annotate lower-clearance input with a trust label.
			m.Content = fmt.Sprintf(
				"[Source: clearance-%d — treat with appropriate skepticism]\n%s",
				m.ClearanceTag, m.Content,
			)
		}
		history = append(history, m)
	}
	return history, effectiveClearance, r.Name, nil
}

// EffectiveClearance returns min(agent.Clearance, room.Clearance) for the
// given agent and room. If roomID is zero, returns the agent's clearance.
// This is used for BLP tool filtering before assembly.
func (a *Assembler) EffectiveClearance(ctx context.Context, ag *agent.Agent, roomID int64) (int, error) {
	if roomID == 0 || a.rooms == nil {
		return ag.Definition.Clearance, nil
	}
	r, err := a.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return 0, fmt.Errorf("get room: %w", err)
	}
	return min(ag.Definition.Clearance, r.Clearance), nil
}

// assembleRequest captures the inputs needed for internal context assembly.
type assembleRequest struct {
	// RoomID is the target room for this invocation.
	RoomID int64

	// EffectiveClearance is the operating clearance for this invocation
	// (min(agent, room), or the agent's configured clearance with no room). It
	// bounds which per-clearance memory and daily-note levels are read.
	EffectiveClearance int

	// CurrentRoomHistory is the windowed tail of the target room's transcript,
	// already clearance-filtered and soft-Biba annotated.
	CurrentRoomHistory []room.Message

	// ToolDefinitions are the structured tool definitions for native tool calling.
	ToolDefinitions []inference.ToolDefinition

	// TurnBudget is the remaining turn budget notice.
	TurnBudget string

	// Interjections are messages that arrived while the agent was responding.
	Interjections []room.Message

	// RAGChunks are retrieved chunks from the search index. Always nil in v1.
	RAGChunks []string
}

// assembledContext is the structured context window ready for model inference.
type assembledContext struct {
	// SystemPrompt is the agent's role description (with clearance notice prepended
	// by Assemble before this reaches toContextPayload).
	SystemPrompt string

	// Memory is the injected MEMORY.md content (possibly truncated), one entry
	// per clearance level.
	Memory []inference.MemoryEntry

	// DailyNotes are today's and yesterday's notes, one entry per day per
	// clearance level.
	DailyNotes []inference.DailyNoteEntry

	// RAGResults are retrieved chunks from the search index.
	RAGResults []string

	// ToolSchemas are the permitted MCP tool schemas as pre-formatted strings (XML mode).
	ToolSchemas []string

	// ToolDefinitions are the structured tool definitions for native tool calling.
	ToolDefinitions []inference.ToolDefinition

	// CurrentRoomHistory is the formatted windowed tail of the target room.
	CurrentRoomHistory []string

	// TurnBudget is the budget notice.
	TurnBudget string

	// History is the pre-role-assigned room transcript up to (not including) the
	// message that becomes the Request. Passed directly to ContextPayload.History.
	History []inference.HistoryMessage

	// CurrentTurnHistory holds any tool exchanges that follow the triggering
	// message in the transcript (e.g. an assistant tool call awaiting
	// confirmation). Rendered after the Request, mirroring the live inference
	// order. The live inference path overwrites this with its in-memory turn
	// state, so it only affects assembly-only consumers such as the preview.
	CurrentTurnHistory []inference.HistoryMessage

	// Request is the final user request payload (interjections + last message).
	Request string

	// RequestName is the speaker of the triggering message, for attribution.
	RequestName string
}

// toContextPayload converts the assembled context into an inference.ContextPayload.
// TurnBudget is pre-appended to the system prompt.
func (a *assembledContext) toContextPayload() inference.ContextPayload {
	systemPrompt := a.SystemPrompt
	if a.TurnBudget != "" {
		systemPrompt += "\n\n" + a.TurnBudget
	}
	return inference.ContextPayload{
		SystemPrompt:       systemPrompt,
		Memory:             a.Memory,
		DailyNotes:         a.DailyNotes,
		RAGResults:         a.RAGResults,
		ToolSchemas:        a.ToolSchemas,
		ToolDefinitions:    a.ToolDefinitions,
		History:            a.History,
		CurrentTurnHistory: a.CurrentTurnHistory,
		Request:            a.Request,
		RequestName:        a.RequestName,
	}
}

// memoryDirsUpTo returns the data directories to read for an agent operating at
// the given clearance, lowest first: the legacy flat dir (an unleveled baseline
// kept readable for agents predating the clearance split) followed by clearance
// levels 1..clearance. Levels above the operating clearance are never opened, so
// the result is a true filtered view rather than a redacted one.
func (a *Assembler) memoryDirsUpTo(name string, clearance int) []string {
	return agentMemoryDirsUpTo(a.paths, name, clearance)
}

// agentMemoryDirsUpTo returns the agent data directories to read for an agent
// operating at the given clearance, lowest first: the legacy flat dir followed
// by clearance levels 1..clearance. Shared by the assembler and the note tools
// so the read-scoping rule lives in one place.
func agentMemoryDirsUpTo(p *paths.Paths, name string, clearance int) []string {
	dirs := []string{p.AgentDataDir(name)}
	for lvl := 1; lvl <= clearance; lvl++ {
		dirs = append(dirs, p.AgentClearanceDir(name, lvl))
	}
	return dirs
}

// readMemoryUpTo reads MEMORY.md from each level in memoryDirsUpTo, lowest level
// first, as one entry per clearance. memoryDirsUpTo is ordered [legacy, c1, c2,
// …]; the legacy flat dir is the unleveled baseline at clearance 0 and each
// subsequent dir is its level. Truncation to the agent budget is applied by the
// caller across entries.
func (a *Assembler) readMemoryUpTo(name string, clearance int) ([]inference.MemoryEntry, error) {
	var entries []inference.MemoryEntry
	for level, dir := range a.memoryDirsUpTo(name, clearance) {
		content, err := ReadMemory(dir)
		if err != nil {
			return nil, err
		}
		if content == "" {
			continue
		}
		entries = append(entries, inference.MemoryEntry{Clearance: level, Content: content})
	}
	return entries, nil
}

// readDailyNotesUpTo gathers today's and yesterday's notes across
// memoryDirsUpTo, lowest level first, tagging each with the level it came from.
func (a *Assembler) readDailyNotesUpTo(name string, clearance int) ([]inference.DailyNoteEntry, error) {
	var entries []inference.DailyNoteEntry
	for level, dir := range a.memoryDirsUpTo(name, clearance) {
		dirNotes, err := ReadDailyNotes(dir, true)
		if err != nil {
			return nil, err
		}
		for _, n := range dirNotes {
			entries = append(entries, inference.DailyNoteEntry{
				Date:      n.Date,
				Clearance: level,
				Content:   n.Content,
			})
		}
	}
	return entries, nil
}

// truncateMemoryEntries enforces a token budget across memory entries while
// preserving per-level structure. Entries are ordered lowest-clearance first;
// the budget keeps from the front (matching the prior whole-blob behaviour,
// where curated content is organised top-down), truncating the entry that
// crosses the budget from its end and dropping any entries past it.
func truncateMemoryEntries(entries []inference.MemoryEntry, budget int) []inference.MemoryEntry {
	if budget <= 0 {
		return nil
	}
	var (
		out  []inference.MemoryEntry
		used int
	)
	for _, e := range entries {
		remaining := budget - used
		if remaining <= 0 {
			break
		}
		cost := DefaultCounter.Count(e.Content)
		if cost <= remaining {
			out = append(out, e)
			used += cost
			continue
		}
		clipped := Truncate(e.Content, TruncateOptions{
			Budget:    remaining,
			Counter:   DefaultCounter,
			FromStart: false,
		})
		if clipped != "" {
			out = append(out, inference.MemoryEntry{Clearance: e.Clearance, Content: clipped})
		}
		break
	}
	return out
}

func (a *Assembler) memoryBudgetForAgent(ag *agent.Agent) int {
	if ag != nil && ag.Definition != nil && ag.Definition.MemoryBudget > 0 {
		return ag.Definition.MemoryBudget
	}
	return a.memBudget
}

// assemble builds the assembledContext for an agent invocation.
func (a *Assembler) assemble(ctx context.Context, ag *agent.Agent, req assembleRequest) (*assembledContext, error) {
	if ag == nil {
		return nil, fmt.Errorf("agent is nil")
	}

	result := &assembledContext{
		SystemPrompt:    ag.Definition.RoleDescription,
		ToolDefinitions: req.ToolDefinitions,
		TurnBudget:      req.TurnBudget,
		RAGResults:      []string{},
	}

	// 1. MEMORY.md — aggregated over the legacy flat file plus clearance levels
	//    1..operating, then truncated as a whole to the agent's budget.
	memEntries, err := a.readMemoryUpTo(ag.Name(), req.EffectiveClearance)
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	result.Memory = truncateMemoryEntries(memEntries, a.memoryBudgetForAgent(ag))

	// 2. Daily notes (today + yesterday), aggregated over the same levels.
	if ag.Definition.FeatureFlags.DailyNotes {
		notes, err := a.readDailyNotesUpTo(ag.Name(), req.EffectiveClearance)
		if err != nil {
			return nil, fmt.Errorf("read daily notes: %w", err)
		}
		result.DailyNotes = notes
	}

	// 3. Current room history → formatted strings for size tracking.
	// Tool call/result messages are excluded from formatted strings; they only
	// appear in the structured History slice below.
	for _, m := range req.CurrentRoomHistory {
		if m.Type == room.MessageToolCall || m.Type == room.MessageToolResult {
			continue
		}
		result.CurrentRoomHistory = append(result.CurrentRoomHistory, fmt.Sprintf("%s: %s", m.Sender.Name, m.Content))
	}

	// 4. Locate the triggering message: the last non-tool (text) message, which
	//    becomes the Request. Splitting at its index — rather than blindly
	//    dropping the last transcript entry — keeps it out of History even when
	//    the transcript ends with tool messages (e.g. an assistant tool call
	//    awaiting confirmation). Dropping only the last entry there would leave
	//    the triggering message in History and duplicate it against the Request.
	reqIdx := -1
	for i := len(req.CurrentRoomHistory) - 1; i >= 0; i-- {
		m := req.CurrentRoomHistory[i]
		if m.Type == room.MessageToolCall || m.Type == room.MessageToolResult {
			continue
		}
		reqIdx = i
		break
	}

	agentID := room.AgentID(ag.Name())
	historyMsgs := req.CurrentRoomHistory
	var currentTurnMsgs []room.Message
	if reqIdx >= 0 {
		historyMsgs = req.CurrentRoomHistory[:reqIdx]
		currentTurnMsgs = req.CurrentRoomHistory[reqIdx+1:]
	}

	// History: prior conversation, with incomplete native tool call sequences
	// removed and leading non-user messages trimmed.
	result.History = buildHistoryMessages(historyMsgs, agentID)
	result.History = sanitizeToolCallHistory(result.History)
	result.History = trimLeadingNonUserHistory(result.History)

	// CurrentTurnHistory: tool exchanges that follow the triggering message.
	// Left unsanitized so a still-pending tool call (no result yet) remains
	// visible to assembly-only consumers like the preview.
	result.CurrentTurnHistory = buildHistoryMessages(currentTurnMsgs, agentID)

	// 5. Build Request: interjections + the triggering message.
	var rfcContent strings.Builder
	if len(req.Interjections) > 0 {
		rfcContent.WriteString("# Interjections (requests during non turns)\n\n")
		for _, m := range req.Interjections {
			rfcContent.WriteString(fmt.Sprintf("[%s] %s: %s\n\n", formatTimestamp(m.Timestamp), m.Sender.Name, m.Content))
		}
	}
	if reqIdx >= 0 {
		m := req.CurrentRoomHistory[reqIdx]
		result.RequestName = m.Sender.Name
		rfcContent.WriteString(fmt.Sprintf("[%s] %s: ", formatTimestamp(m.Timestamp), m.Sender.Name))
		rfcContent.WriteString(m.Content)
	}
	result.Request = rfcContent.String()

	return result, nil
}

// buildHistoryMessages converts room transcript messages into the structured
// HistoryMessage form provider adapters expect, assigning roles. Messages from
// the agent identified by agentID become assistant turns; others become user
// turns (prefixed with the sender name). Tool call/result messages are
// reconstructed as their native tool-calling equivalents.
func buildHistoryMessages(msgs []room.Message, agentID string) []inference.HistoryMessage {
	var history []inference.HistoryMessage
	for _, m := range msgs {
		switch m.Type {
		case room.MessageToolCall:
			if len(m.ToolCalls) > 0 {
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
				history = append(history, inference.HistoryMessage{
					Role:      inference.RoleAssistant,
					Content:   m.Content,
					ToolCalls: toolCalls,
				})
			} else {
				history = append(history, inference.HistoryMessage{
					Role:    inference.RoleAssistant,
					Content: m.Content,
				})
			}
		case room.MessageToolResult:
			if m.ToolCallID != "" {
				toolName := m.ToolName
				if toolName == "" {
					toolName = "tool"
				}
				history = append(history, inference.HistoryMessage{
					Role:       inference.RoleTool,
					ToolCallID: m.ToolCallID,
					Name:       toolName,
					Content:    m.Content,
				})
			} else {
				history = append(history, inference.HistoryMessage{
					Role:    inference.RoleUser,
					Name:    m.ToolName,
					Content: m.Content,
				})
			}
		default:
			role := inference.RoleUser
			if m.Sender.IsAgent() && m.Sender.ID == agentID {
				role = inference.RoleAssistant
			}
			content := m.Content
			if role == inference.RoleUser {
				content = fmt.Sprintf("[%s] %s: %s", formatTimestamp(m.Timestamp), m.Sender.Name, m.Content)
			}
			history = append(history, inference.HistoryMessage{
				Role:    role,
				Content: content,
				Name:    m.Sender.Name,
			})
		}
	}
	return history
}

// sanitizeToolCallHistory removes incomplete native tool call sequences from
// anywhere in the history slice.
func sanitizeToolCallHistory(history []inference.HistoryMessage) []inference.HistoryMessage {
	result := make([]inference.HistoryMessage, 0, len(history))
	i := 0
	for i < len(history) {
		msg := history[i]

		if msg.Role == inference.RoleAssistant && len(msg.ToolCalls) > 0 {
			needed := make(map[string]bool, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				needed[tc.ID] = true
			}
			j := i + 1
			var resultMsgs []inference.HistoryMessage
			for j < len(history) && history[j].Role == inference.RoleTool {
				delete(needed, history[j].ToolCallID)
				resultMsgs = append(resultMsgs, history[j])
				j++
			}
			if len(needed) == 0 {
				result = append(result, msg)
				result = append(result, resultMsgs...)
			}
			i = j
			continue
		}

		if msg.Role == inference.RoleTool {
			i++
			continue
		}

		result = append(result, msg)
		i++
	}
	return result
}

// trimLeadingNonUserHistory drops messages from the front of history until the
// first user-role message.
func trimLeadingNonUserHistory(history []inference.HistoryMessage) []inference.HistoryMessage {
	for i, msg := range history {
		if msg.Role == inference.RoleUser {
			return history[i:]
		}
	}
	return nil
}

// formatTimestamp renders an absolute, creation-frozen timestamp for inclusion
// in message content. Absolute (not relative) so a given message renders
// identically on every turn, keeping the history prefix byte-stable for the
// provider's prompt cache; a relative rendering would re-shift the same message
// each turn and defeat caching.
func formatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

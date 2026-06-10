package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	storedb "github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
)

// newTestAssembler creates an Assembler wired to a real SQLiteStore for tests.
func newTestAssembler(t *testing.T) (*Assembler, *storedb.SQLiteStore, *paths.Paths) {
	t.Helper()
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	store, err := storedb.NewSQLiteStore(filepath.Join(dir, "rooms.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return NewAssembler(p, 4096, store, store), store, p
}

// newTestAgent creates an Agent with the given name and clearance.
func newTestAgent(t *testing.T, p *paths.Paths, name string, clearance int) *agent.Agent {
	t.Helper()

	agentDir := p.AgentDataDir(name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, MemoryFileName), []byte("Key fact: user likes tea."), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	ag, err := agent.NewAgent(&config.AgentDefinition{
		Name:            name,
		RoleDescription: "You are a helpful assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{DailyNotes: false, IdentityContinuity: true},
		Clearance:       clearance,
		MemoryBudget:    4096,
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	return ag
}

// newTestRoom creates a room in the store and returns it.
func newTestRoom(t *testing.T, store *storedb.SQLiteStore, ceiling int, participants ...room.Actor) room.Room {
	t.Helper()
	r := room.Room{
		Clearance:    ceiling,
		Participants: participants,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := store.CreateRoom(context.Background(), &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	return r
}

func TestAssembler_Assemble_BasicContext(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}
	r := newTestRoom(t, store, 5, alice, housewife)

	ctx := context.Background()
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	req := agent.Request{
		ID:      "req-1",
		Target:  "housewife",
		Source:  agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}

	payload, err := assembler.Assemble(ctx, ag, req, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if !strings.Contains(payload.SystemPrompt, "helpful assistant") {
		t.Errorf("system prompt missing role description: %q", payload.SystemPrompt)
	}
	if !strings.Contains(payload.SystemPrompt, "clearance level 5") {
		t.Errorf("system prompt missing clearance notice: %q", payload.SystemPrompt)
	}
	if !strings.Contains(payload.SystemPrompt, "You are in room #") {
		t.Errorf("system prompt missing room identity: %q", payload.SystemPrompt)
	}
	if !strings.Contains(joinMemory(payload.Memory), "likes tea") {
		t.Errorf("memory missing expected content: %v", payload.Memory)
	}
}

// joinMemory flattens memory entry contents for substring assertions.
func joinMemory(entries []inference.MemoryEntry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// TestAssembler_Assemble_RoomName verifies a named room surfaces its display
// name (in addition to the ID) in the system-prompt identity line.
func TestAssembler_Assemble_RoomName(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(t, store, 5, alice)
	r.Name = "Kitchen"
	if err := store.UpdateRoom(context.Background(), &r); err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}

	payload, err := assembler.Assemble(context.Background(), ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if !strings.Contains(payload.SystemPrompt, `"Kitchen"`) {
		t.Errorf("system prompt missing room name: %q", payload.SystemPrompt)
	}
}

func TestAssembler_Assemble_ClearanceFilter(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	// Alice's clearance must be ≤ room ceiling to join.
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 2, Name: "Alice"}
	r := newTestRoom(t, store, 2, alice) // ceiling = 2

	ctx := context.Background()
	// Message at clearance 2 — within ceiling, should appear
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	// Message at clearance 4 — above ceiling, should be filtered out
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 4, Type: room.MessageText, Content: "Secret info",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	// Final message to trigger the request
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 2, Type: room.MessageText, Content: "Final question",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	payload, err := assembler.Assemble(ctx, ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if strings.Contains(payload.Request, "Secret info") {
		t.Error("Request contains above-ceiling message, should have been filtered")
	}
	if !strings.Contains(payload.Request, "Final question") {
		t.Errorf("Request missing expected content: %q", payload.Request)
	}
}

func TestAssembler_Assemble_SoftBiba(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(t, store, 5, alice) // ceiling = 5

	ctx := context.Background()
	// Message at clearance 2 in a clearance-5 room → should get Biba annotation
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 2, Type: room.MessageText, Content: "External source",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	// Final message (becomes Request, also clearance 2)
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 2, Type: room.MessageText, Content: "Follow-up",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	payload, err := assembler.Assemble(ctx, ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// History[0] should be the annotated msg-1
	if len(payload.History) < 1 {
		t.Fatalf("expected at least 1 history message, got %d", len(payload.History))
	}
	if !strings.Contains(payload.History[0].Content, "treat with appropriate skepticism") {
		t.Errorf("soft Biba annotation missing: %q", payload.History[0].Content)
	}
}

func TestAssembler_Assemble_RoomHistoryRoles(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}
	r := newTestRoom(t, store, 5, alice, housewife)

	ctx := context.Background()
	msgs := []room.Message{
		{Timestamp: time.Now(), RoomID: r.ID, Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello"},
		{Timestamp: time.Now(), RoomID: r.ID, Sender: housewife, ClearanceTag: 5, Type: room.MessageText, Content: "Hi there"},
		{Timestamp: time.Now(), RoomID: r.ID, Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "How are you?"},
	}
	for _, m := range msgs {
		if _, err := store.AppendMessage(ctx, r.ID, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	payload, err := assembler.Assemble(ctx, ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// History: msg-1 (user), msg-2 (assistant). msg-3 becomes the Request.
	if len(payload.History) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(payload.History))
	}
	if payload.History[0].Role != inference.RoleUser {
		t.Errorf("history[0] role: got %q, want user", payload.History[0].Role)
	}
	if payload.History[1].Role != inference.RoleAssistant {
		t.Errorf("history[1] role: got %q, want assistant", payload.History[1].Role)
	}
	// Incoming messages carry an absolute timestamp prefix; the agent's own
	// turns are left raw so it doesn't learn to imitate the prefix in replies.
	if !strings.HasPrefix(payload.History[0].Content, "[") || !strings.Contains(payload.History[0].Content, "Alice: Hello") {
		t.Errorf("user history missing timestamped prefix: %q", payload.History[0].Content)
	}
	if payload.History[1].Content != "Hi there" {
		t.Errorf("assistant history should be unprefixed: %q", payload.History[1].Content)
	}
	if !strings.HasPrefix(payload.Request, "[") || !strings.Contains(payload.Request, "Alice: How are you?") {
		t.Errorf("Request missing timestamped prefix or content: %q", payload.Request)
	}
	if payload.RequestName != "Alice" {
		t.Errorf("RequestName: got %q, want Alice", payload.RequestName)
	}
}

// TestAssembler_Assemble_PendingToolCall covers the assembly state while the
// agent waits on a confirmation: the transcript ends with the agent's persisted
// tool call, after the triggering user message. The triggering message must
// appear only as the Request (not duplicated into History), carry its speaker,
// and the pending tool call must follow it as current-turn history.
func TestAssembler_Assemble_PendingToolCall(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}
	r := newTestRoom(t, store, 5, alice, housewife)

	ctx := context.Background()
	msgs := []room.Message{
		{Timestamp: time.Now(), RoomID: r.ID, Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "search the docs"},
		{Timestamp: time.Now(), RoomID: r.ID, Sender: housewife, ClearanceTag: 5, Type: room.MessageToolCall,
			ToolCalls: []room.ToolCallRecord{{ID: "call_1", ToolName: "search", Arguments: "{}"}}},
	}
	for _, m := range msgs {
		if _, err := store.AppendMessage(ctx, r.ID, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	payload, err := assembler.Assemble(ctx, ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if !strings.Contains(payload.Request, "search the docs") {
		t.Errorf("Request missing triggering message: %q", payload.Request)
	}
	for _, h := range payload.History {
		if strings.Contains(h.Content, "search the docs") {
			t.Errorf("triggering message duplicated into History: %q", h.Content)
		}
	}
	if payload.RequestName != "Alice" {
		t.Errorf("RequestName: got %q, want Alice", payload.RequestName)
	}
	if len(payload.CurrentTurnHistory) != 1 || len(payload.CurrentTurnHistory[0].ToolCalls) != 1 {
		t.Errorf("expected pending tool call in CurrentTurnHistory, got %+v", payload.CurrentTurnHistory)
	}
}

func TestAssembler_Assemble_DailyNotes(t *testing.T) {
	assembler, store, p := newTestAssembler(t)

	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(filepath.Join(agentDir, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Now().UTC().Format("2006-01-02") + ".md"
	if err := os.WriteFile(filepath.Join(agentDir, "memory", today), []byte("Today's observation."), 0o644); err != nil {
		t.Fatalf("write daily note: %v", err)
	}

	ag, err := agent.NewAgent(&config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "Assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{DailyNotes: true, IdentityContinuity: true},
		Clearance:       5,
		MemoryBudget:    4096,
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(t, store, 5, alice)

	ctx := context.Background()
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	payload, err := assembler.Assemble(ctx, ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if len(payload.DailyNotes) != 1 {
		t.Fatalf("expected 1 daily note, got %d", len(payload.DailyNotes))
	}
	if !strings.Contains(payload.DailyNotes[0].Content, "Today's observation") {
		t.Errorf("daily note content: %q", payload.DailyNotes[0].Content)
	}
}

func TestAssembler_Assemble_MemoryTruncation(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	store, err := storedb.NewSQLiteStore(filepath.Join(dir, "rooms.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	longMemory := strings.Repeat("word ", 10000)
	if err := os.WriteFile(filepath.Join(agentDir, MemoryFileName), []byte(longMemory), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	ag, err := agent.NewAgent(&config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "Assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{IdentityContinuity: true},
		Clearance:       5,
		MemoryBudget:    10,
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(t, store, 5, alice)

	ctx := context.Background()
	if _, err := store.AppendMessage(ctx, r.ID, room.Message{
		Timestamp: time.Now(), RoomID: r.ID,
		Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	assembler := NewAssembler(p, 10, store, store)
	payload, err := assembler.Assemble(ctx, ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if n := len(joinMemory(payload.Memory)); n > 100 {
		t.Errorf("memory not truncated: got %d chars", n)
	}
}

func TestAssembler_Assemble_CompactionOffset(t *testing.T) {
	assembler, store, p := newTestAssembler(t)
	ag := newTestAgent(t, p, "housewife", 5)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	r := newTestRoom(t, store, 5, alice)

	ctx := context.Background()
	// Append 3 messages; set compaction offset to 2 so only msg-3 is visible.
	for _, content := range []string{"Compacted msg 1", "Compacted msg 2", "Visible msg 3"} {
		if _, err := store.AppendMessage(ctx, r.ID, room.Message{
			// number auto-assigned by DB

			Timestamp:    time.Now(),
			RoomID:       r.ID,
			Sender:       alice,
			ClearanceTag: 5,
			Type:         room.MessageText,
			Content:      content,
		}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	if err := store.SetCompactionOffset(ctx, "housewife", r.ID, 2); err != nil {
		t.Fatalf("SetCompactionOffset: %v", err)
	}

	payload, err := assembler.Assemble(ctx, ag, agent.Request{
		ID: "req-1", Target: "housewife", Source: agent.SourceRoom,
		Payload: agent.RequestPayload{RoomID: r.ID},
	}, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if strings.Contains(payload.Request, "Compacted") {
		t.Error("Request contains compacted messages, should have been skipped")
	}
	if !strings.Contains(payload.Request, "Visible msg 3") {
		t.Errorf("Request missing visible message: %q", payload.Request)
	}
}

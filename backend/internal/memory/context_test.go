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
)

func TestAssembler_Assemble(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	// Create agent data dir with MEMORY.md
	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, MemoryFileName), []byte("Key fact: user likes tea."), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	// Create agent definition
	def := &config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "You are a helpful assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{DailyNotes: false, IdentityContinuity: true},
		Clearance:       5,
		MemoryBudget:    4096,
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	assembler := NewAssembler(p, 4096)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	history := []room.Message{
		{ID: "msg_1", Timestamp: time.Now(), RoomID: "room_1", Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello"},
	}

	ctx := context.Background()
	result, err := assembler.Assemble(ctx, ag, AssembleRequest{
		RoomID:             "room_1",
		CurrentRoomHistory: history,
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	payload := result.ToContextPayload("test-model")

	if !strings.Contains(payload.SystemPrompt, "helpful assistant") {
		t.Errorf("system prompt missing role description: %q", payload.SystemPrompt)
	}
	if !strings.Contains(payload.Memory, "likes tea") {
		t.Errorf("memory missing expected content: %q", payload.Memory)
	}
}

func TestAssembler_Assemble_WithDailyNotes(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(filepath.Join(agentDir, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, MemoryFileName), []byte("Base memory."), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	// Write today's daily note
	today := time.Now().UTC().Format("2006-01-02") + ".md"
	if err := os.WriteFile(filepath.Join(agentDir, "memory", today), []byte("Today's observation."), 0o644); err != nil {
		t.Fatalf("write daily note: %v", err)
	}

	def := &config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "Assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{DailyNotes: true, IdentityContinuity: true},
		Clearance:       5,
		MemoryBudget:    4096,
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	assembler := NewAssembler(p, 4096)
	ctx := context.Background()
	result, err := assembler.Assemble(ctx, ag, AssembleRequest{
		RoomID:             "room_1",
		CurrentRoomHistory: []room.Message{},
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if len(result.DailyNotes) != 1 {
		t.Fatalf("expected 1 daily note, got %d", len(result.DailyNotes))
	}
	if !strings.Contains(result.DailyNotes[0], "Today's observation") {
		t.Errorf("daily note content: %q", result.DailyNotes[0])
	}
}

func TestAssembler_Assemble_NilAgent(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)
	assembler := NewAssembler(p, 4096)

	ctx := context.Background()
	_, err := assembler.Assemble(ctx, nil, AssembleRequest{})
	if err == nil {
		t.Fatal("expected error for nil agent, got nil")
	}
}

func TestAssembler_Assemble_Truncation(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a very long MEMORY.md
	longMemory := strings.Repeat("word ", 10000) // ~50k chars, way over budget
	if err := os.WriteFile(filepath.Join(agentDir, MemoryFileName), []byte(longMemory), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	def := &config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "Assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{IdentityContinuity: true},
		Clearance:       5,
		MemoryBudget:    10,
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	// Small budget: 10 tokens ≈ 40 chars
	assembler := NewAssembler(p, 10)
	ctx := context.Background()
	result, err := assembler.Assemble(ctx, ag, AssembleRequest{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// Memory should be truncated to roughly 40 chars
	if len(result.Memory) > 100 {
		t.Errorf("memory not truncated: got %d chars", len(result.Memory))
	}
}

func TestAssembler_RoomHistoryRoles(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, MemoryFileName), []byte("Memory."), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	def := &config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "Assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{IdentityContinuity: true},
		Clearance:       5,
		MemoryBudget:    4096,
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	assembler := NewAssembler(p, 4096)
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}

	history := []room.Message{
		{ID: "msg_1", Timestamp: time.Now(), RoomID: "room_1", Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello"},
		{ID: "msg_2", Timestamp: time.Now(), RoomID: "room_1", Sender: housewife, ClearanceTag: 5, Type: room.MessageText, Content: "Hi there"},
		{ID: "msg_3", Timestamp: time.Now(), RoomID: "room_1", Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "How are you?"},
	}

	ctx := context.Background()
	result, err := assembler.Assemble(ctx, ag, AssembleRequest{CurrentRoomHistory: history})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	payload := result.ToContextPayload("test-model")

	// History should have 2 entries (msg_1, msg_2); msg_3 becomes the RFC
	if len(payload.History) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(payload.History))
	}

	// msg_1 → user, msg_2 → assistant (same agent)
	if payload.History[0].Role != inference.RoleUser {
		t.Errorf("history[0] role: got %q, want user", payload.History[0].Role)
	}
	if payload.History[1].Role != inference.RoleAssistant {
		t.Errorf("history[1] role: got %q, want assistant", payload.History[1].Role)
	}

	// RFC should contain the last message content
	if !strings.Contains(payload.RFC, "How are you?") {
		t.Errorf("RFC missing last message content: %q", payload.RFC)
	}
}

func TestAssembler_Assemble_AgentMemoryBudgetOverride(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	longMemory := strings.Repeat("word ", 10000)
	if err := os.WriteFile(filepath.Join(agentDir, MemoryFileName), []byte(longMemory), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	def := &config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "Assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{IdentityContinuity: true},
		Clearance:       5,
		MemoryBudget:    10,
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	assembler := NewAssembler(p, 4096)
	result, err := assembler.Assemble(context.Background(), ag, AssembleRequest{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if len(result.Memory) > 100 {
		t.Fatalf("expected agent override to truncate memory, got %d chars", len(result.Memory))
	}
}

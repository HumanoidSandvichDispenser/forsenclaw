package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

func newTestDispatcher(t *testing.T) (*Dispatcher, *paths.Paths, func()) {
	t.Helper()

	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	// Create required directories
	for _, path := range []string{p.DBDir(), p.AgentsConfigDir(), p.AgentsDataDir(), p.RoomsDir()} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	// Create agent data dir with MEMORY.md
	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, memory.MemoryFileName), []byte("Key fact."), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	// Create SQLite store
	store, err := room.NewSQLiteStore(p.RoomsDBPath())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	// Create agent manager with one agent
	serverCfg := &config.ServerConfig{}
	mgr, err := agent.NewManager(p, serverCfg)
	if err != nil {
		store.Close()
		t.Fatalf("NewManager: %v", err)
	}

	// Manually add agent definition if not loaded
	if mgr.Get("housewife") == nil {
		def := &config.AgentDefinition{
			Name:            "housewife",
			RoleDescription: "You are a helpful assistant.",
			Models:          config.AgentModels{Primary: "test-model"},
			FeatureFlags:    config.FeatureFlags{IdentityContinuity: true, DailyNotes: false},
			Clearance:       5,
			RawPermissions:  []string{},
		}
		ag, _ := agent.NewAgent(def)
		// We can't add to manager directly, so we'll register with dispatcher
		// without going through the manager
		_ = ag
	}

	assembler := memory.NewAssembler(p, 4096)
	ctxCfg := config.DefaultContextConfig()

	// Create dispatcher
	d := NewDispatcher(mgr, nil, assembler, store, p, ctxCfg)

	cleanup := func() {
		d.mu.Lock()
		for name, entry := range d.agents {
			close(entry.queue)
			delete(d.agents, name)
		}
		d.mu.Unlock()
		// Wait for goroutines to finish
		time.Sleep(100 * time.Millisecond)
		store.Close()
		mgr.Close()
	}

	return d, p, cleanup
}

func TestDispatcher_RegisterUnregister(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	def := &config.AgentDefinition{
		Name:            "test-agent",
		RoleDescription: "Test.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{},
		Clearance:       5,
		RawPermissions:  []string{},
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	d.RegisterAgent(ag)

	// Verify agent is registered
	d.mu.RLock()
	_, ok := d.agents["test-agent"]
	d.mu.RUnlock()
	if !ok {
		t.Fatal("expected agent to be registered")
	}

	// Unregister
	d.UnregisterAgent("test-agent")

	d.mu.RLock()
	_, ok = d.agents["test-agent"]
	d.mu.RUnlock()
	if ok {
		t.Fatal("expected agent to be unregistered")
	}
}

func TestDispatcher_RegisterAgent_Twice(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	def := &config.AgentDefinition{
		Name:            "test-agent",
		RoleDescription: "Test.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{},
		Clearance:       5,
		RawPermissions:  []string{},
	}
	ag, _ := agent.NewAgent(def)

	d.RegisterAgent(ag)
	d.RegisterAgent(ag) // should be no-op

	d.mu.RLock()
	if len(d.agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(d.agents))
	}
	d.mu.RUnlock()

	d.UnregisterAgent("test-agent")
}

func TestDispatcher_IssueRFC_AgentNotRegistered(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	rfc := room.RFC{
		ID:     "rfc_1",
		RoomID: "room_1",
		Target: "agent:nonexistent",
	}

	err := d.IssueRFC(rfc)
	if err == nil {
		t.Fatal("expected error for unregistered agent, got nil")
	}
}

func TestDispatcher_IssueRFC_InvalidTarget(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	rfc := room.RFC{
		ID:     "rfc_1",
		RoomID: "room_1",
		Target: "user:alice", // not an agent
	}

	err := d.IssueRFC(rfc)
	if err == nil {
		t.Fatal("expected error for non-agent target, got nil")
	}
}

func TestDispatcher_IssueRFC_EmptyTarget(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	rfc := room.RFC{
		ID:     "rfc_1",
		RoomID: "room_1",
		Target: "",
	}

	err := d.IssueRFC(rfc)
	if err == nil {
		t.Fatal("expected error for empty target, got nil")
	}
}

func TestDispatcher_HandleUserMessage_InvalidSender(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}

	// Create room
	r := room.Room{
		ID:               uuid.New().String(),
		Participants:     []room.Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     room.ProtocolFreeForm,
	}
	ctx := context.Background()
	if err := d.store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Sender not in room
	bob := room.Actor{ID: "user:bob", Type: room.ActorUser, Clearance: 5, Name: "Bob"}
	msg := room.Message{
		ID: uuid.New().String(), Timestamp: time.Now().UTC(),
		RoomID: r.ID, Sender: bob, ClearanceTag: 5, Type: room.MessageText, Content: "Hello",
	}

	err := d.HandleUserMessage(ctx, r.ID, bob, msg)
	if err == nil {
		t.Fatal("expected error for invalid sender, got nil")
	}
}

func TestDispatcher_HandleUserMessage_RoomNotFound(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	msg := room.Message{
		ID: uuid.New().String(), Timestamp: time.Now().UTC(),
		RoomID: "nonexistent", Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello",
	}

	ctx := context.Background()
	err := d.HandleUserMessage(ctx, "nonexistent", alice, msg)
	if err == nil {
		t.Fatal("expected error for missing room, got nil")
	}
}

func TestDispatcher_StartProtocol(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}

	r := room.Room{
		ID:               uuid.New().String(),
		Participants:     []room.Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     room.ProtocolFreeForm,
	}

	if err := d.StartProtocol(&r); err != nil {
		t.Fatalf("StartProtocol: %v", err)
	}

	d.mu.RLock()
	_, ok := d.protocols[r.ID]
	d.mu.RUnlock()
	if !ok {
		t.Fatal("expected protocol to be started")
	}
}

func TestDispatcher_StartProtocol_Unsupported(t *testing.T) {
	d, _, cleanup := newTestDispatcher(t)
	defer cleanup()

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}

	r := room.Room{
		ID:               uuid.New().String(),
		Participants:     []room.Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     "roundrobin",
	}

	err := d.StartProtocol(&r)
	if err == nil {
		t.Fatal("expected error for unsupported protocol, got nil")
	}
}

// TestDispatcher_FullFlow tests the complete user message → RFC → agent response flow
// using a mock provider.
func TestDispatcher_FullFlow(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	// Create required directories
	for _, path := range []string{p.DBDir(), p.AgentsDataDir(), p.RoomsDir()} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	// Set up agent memory
	agentDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, memory.MemoryFileName), []byte("Key fact."), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	// Create store
	store, err := room.NewSQLiteStore(p.RoomsDBPath())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// Create agent
	def := &config.AgentDefinition{
		Name:            "housewife",
		RoleDescription: "You are a helpful assistant.",
		Models:          config.AgentModels{Primary: "test-model"},
		FeatureFlags:    config.FeatureFlags{IdentityContinuity: true, DailyNotes: false},
		Clearance:       5,
		RawPermissions:  []string{},
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	// Create mock provider and registry
	mockProv := newMockProvider("Hello, Alice! How can I help?", nil)
	mockReg := newMockRegistry(mockProv, "test-model")

	assembler := memory.NewAssembler(p, 4096)
	ctxCfg := config.DefaultContextConfig()
	d := NewDispatcher(nil, mockReg, assembler, store, p, ctxCfg)

	// Register agent
	d.RegisterAgent(ag)
	defer d.UnregisterAgent("housewife")

	// Create room
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	housewife := room.Actor{ID: "agent:housewife", Type: room.ActorAgent, Clearance: 5, Name: "Housewife"}

	r := room.Room{
		ID:               uuid.New().String(),
		Participants:     []room.Actor{alice, housewife},
		ClearanceCeiling: 5,
		ProtocolType:     room.ProtocolFreeForm,
	}
	ctx := context.Background()
	if err := store.CreateRoom(ctx, &r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Start protocol
	if err := d.StartProtocol(&r); err != nil {
		t.Fatalf("StartProtocol: %v", err)
	}

	// Send user message
	msg := room.Message{
		ID: uuid.New().String(), Timestamp: time.Now().UTC(),
		RoomID: r.ID, Sender: alice, ClearanceTag: 5, Type: room.MessageText, Content: "Hello!",
	}
	if err := d.HandleUserMessage(ctx, r.ID, alice, msg); err != nil {
		t.Fatalf("HandleUserMessage: %v", err)
	}

	// Wait for agent response (async)
	time.Sleep(500 * time.Millisecond)

	// Read transcript
	msgs, err := room.ReadMessages(ctx, p.RoomsDir(), r.ID, room.ReadOpts{})
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in transcript, got %d", len(msgs))
	}

	// First message is user
	if msgs[0].Content != "Hello!" {
		t.Errorf("first message: got %q, want Hello!", msgs[0].Content)
	}

	// Second message is agent response
	if msgs[1].Content != "Hello, Alice! How can I help?" {
		t.Errorf("second message: got %q, want agent response", msgs[1].Content)
	}
	if !msgs[1].Sender.IsAgent() {
		t.Errorf("expected sender to be agent, got %v", msgs[1].Sender.Type)
	}
}

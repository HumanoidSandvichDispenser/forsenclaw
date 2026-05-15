package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// mockBroadcaster captures broadcast events for testing.
type mockBroadcaster struct {
	events []dispatch.StreamEvent
}

func (m *mockBroadcaster) Broadcast(roomID string, event dispatch.StreamEvent) {
	m.events = append(m.events, event)
}

// mockProvider is a test provider that returns a pre-configured response.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Infer(ctx context.Context, req inference.InferRequest) (<-chan inference.StreamingChunk, error) {
	if m.err != nil {
		return nil, m.err
	}

	ch := make(chan inference.StreamingChunk, 1)
	go func() {
		defer close(ch)
		ch <- inference.StreamingChunk{
			Content:      m.response,
			FinishReason: "stop",
		}
	}()
	return ch, nil
}

// mockRegistry is a test ModelResolver that returns a mock provider.
type mockRegistry struct {
	provider inference.Provider
	modelID  string
}

func (m *mockRegistry) ResolveTier(agentDef *config.AgentDefinition, tier inference.ModelTier) (inference.Provider, string, error) {
	return m.provider, m.modelID, nil
}

// newTestService creates a fully wired API service for testing.
func newTestService(t *testing.T) (*Service, *paths.Paths, func()) {
	t.Helper()

	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	// Create required directories
	for _, path := range []string{p.DBDir(), p.AgentsConfigDir(), p.AgentsDataDir(), p.RoomsDir()} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	// Set up agent config and memory
	agentConfigDir := p.AgentConfigDir("housewife")
	if err := os.MkdirAll(agentConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	agentYAML := `name: housewife
role_description: "You are a helpful assistant."
models:
  primary: test-model
  routine: test-model
  sensitive: test-model
feature_flags:
  identity_continuity: true
  daily_notes: false
  proactive_triggers: false
  dreaming: false
clearance: 5
permissions: []
`
	if err := os.WriteFile(filepath.Join(agentConfigDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}

	agentDataDir := p.AgentDataDir("housewife")
	if err := os.MkdirAll(agentDataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDataDir, memory.MemoryFileName), []byte("Key fact."), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	// Create SQLite store
	store, err := room.NewSQLiteStore(p.RoomsDBPath())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	// Create agent manager with test model registered
	serverCfg := &config.ServerConfig{
		Models: map[string]config.Model{
			"test-model": {Provider: "test", ProviderModel: "test-model"},
		},
	}
	agentMgr, err := agent.NewManager(p, serverCfg)
	if err != nil {
		store.Close()
		t.Fatalf("NewManager: %v", err)
	}

	// Create mock provider and registry
	mockProv := &mockProvider{response: "Hello, Alice! How can I help?"}
	mockReg := &mockRegistry{provider: mockProv, modelID: "test-model"}

	assembler := memory.NewAssembler(p, 4096)
	dispatcher := dispatch.NewDispatcher(agentMgr, mockReg, assembler, store, p)

	// Register agents from manager
	for _, ag := range agentMgr.All() {
		dispatcher.RegisterAgent(ag)
	}

	hub := NewHub()
	go hub.Run()

	svc := NewService(dispatcher, store, agentMgr, hub, p)

	cleanup := func() {
		for name := range dispatcher.All() {
			dispatcher.UnregisterAgent(name)
		}
		store.Close()
		agentMgr.Close()
	}

	return svc, p, cleanup
}

func newTestRouter(t *testing.T, svc *Service) (chi.Router, huma.API) {
	t.Helper()
	router := chi.NewRouter()
	router.Use(AuthMiddleware())
	api := NewAPI(router, svc)
	return router, api
}

// TestCreateRoom tests creating a room via the API.
func TestCreateRoom(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	body, _ := json.Marshal(map[string]any{
		"participant_ids":   []string{"user:alice", "agent:housewife"},
		"clearance_ceiling": 5,
		"protocol_type":     "freeform",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var room RoomResponse
	if err := json.Unmarshal(w.Body.Bytes(), &room); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, w.Body.String())
	}

	if room.ID == "" {
		t.Fatalf("expected room ID to be set, got empty. body: %s", w.Body.String())
	}
	if len(room.Participants) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(room.Participants))
	}
	if room.ProtocolType != "freeform" {
		t.Fatalf("expected protocol freeform, got %s", room.ProtocolType)
	}
}

// TestCreateRoomValidation tests validation errors on room creation.
func TestCreateRoomValidation(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	// Missing participant_ids
	body, _ := json.Marshal(map[string]any{
		"protocol_type": "freeform",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetRoom tests fetching a room by ID.
func TestGetRoom(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	// Create a room first
	rm := createTestRoom(t, svc, router)

	// Fetch it
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/"+rm.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var room RoomResponse
	if err := json.Unmarshal(w.Body.Bytes(), &room); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if room.ID != rm.ID {
		t.Fatalf("expected room ID %s, got %s", rm.ID, room.ID)
	}
}

// TestListRooms tests listing rooms.
func TestListRooms(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	// Create two rooms
	_ = createTestRoom(t, svc, router)
	_ = createTestRoom(t, svc, router)

	req := httptest.NewRequest(http.MethodGet, "/api/rooms", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Rooms []RoomResponse `json:"rooms"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(body.Rooms) < 2 {
		t.Fatalf("expected at least 2 rooms, got %d", len(body.Rooms))
	}
}

// TestSendMessage tests sending a message and receiving an agent response.
func TestSendMessage(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	// Create a room
	rm := createTestRoom(t, svc, router)

	// Send a message
	body, _ := json.Marshal(map[string]any{
		"sender":  "user:alice",
		"content": "Hello!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/"+rm.ID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var msg MessageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if msg.Content != "Hello!" {
		t.Fatalf("expected message content 'Hello!', got %q", msg.Content)
	}
	if msg.Sender.ID != "user:alice" {
		t.Fatalf("expected sender user:alice, got %s", msg.Sender.ID)
	}

	// Wait for agent response (async)
	time.Sleep(500 * time.Millisecond)

	// List messages
	req = httptest.NewRequest(http.MethodGet, "/api/rooms/"+rm.ID+"/messages", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listBody struct {
		Messages []MessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(listBody.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(listBody.Messages))
	}

	// Second message is agent response
	agentMsg := listBody.Messages[1]
	if !strings.Contains(agentMsg.Content, "Hello, Alice!") {
		t.Fatalf("expected agent response to contain 'Hello, Alice!', got %q", agentMsg.Content)
	}
	if agentMsg.Sender.Type != "agent" {
		t.Fatalf("expected sender type agent, got %s", agentMsg.Sender.Type)
	}
}

// TestListMessages tests listing messages in a room.
func TestListMessages(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	rm := createTestRoom(t, svc, router)

	// Send two messages
	sendTestMessage(t, router, rm.ID, "user:alice", "First")
	sendTestMessage(t, router, rm.ID, "user:alice", "Second")

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/"+rm.ID+"/messages?limit=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Messages []MessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(body.Messages) != 1 {
		t.Fatalf("expected 1 message (limited), got %d", len(body.Messages))
	}
}

// TestListAgents tests listing loaded agents.
func TestListAgents(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Agents []AgentResponse `json:"agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(body.Agents) == 0 {
		t.Fatal("expected at least one agent")
	}
}

// TestGetAgent tests getting a specific agent.
func TestGetAgent(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/housewife", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var agent AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &agent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if agent.Name != "housewife" {
		t.Fatalf("expected agent name housewife, got %s", agent.Name)
	}
}

// TestGetMe tests the /api/me endpoint.
func TestGetMe(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var user UserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if user.ID != "local" {
		t.Fatalf("expected user ID local, got %s", user.ID)
	}
	if user.Role != "owner" {
		t.Fatalf("expected role owner, got %s", user.Role)
	}
}

// TestSendMessageInvalidSender tests sending a message from a non-participant.
func TestSendMessageInvalidSender(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	rm := createTestRoom(t, svc, router)

	body, _ := json.Marshal(map[string]any{
		"sender":  "user:bob", // not a participant
		"content": "Hello!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/"+rm.ID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetRoomNotFound tests fetching a non-existent room.
func TestGetRoomNotFound(t *testing.T) {
	svc, _, cleanup := newTestService(t)
	defer cleanup()

	router, _ := newTestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Helpers ---

func createTestRoom(t *testing.T, svc *Service, router chi.Router) room.Room {
	t.Helper()

	body, _ := json.Marshal(map[string]any{
		"participant_ids":   []string{"user:alice", "agent:housewife"},
		"clearance_ceiling": 5,
		"protocol_type":     "freeform",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("create room: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var room RoomResponse
	if err := json.Unmarshal(w.Body.Bytes(), &room); err != nil {
		t.Fatalf("unmarshal create room response: %v", err)
	}

	// Return the created room (we need to look it up in the store)
	ctx := context.Background()
	r, err := svc.store.GetRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("get created room: %v", err)
	}
	return *r
}

func sendTestMessage(t *testing.T, router chi.Router, roomID, sender, content string) {
	t.Helper()

	body, _ := json.Marshal(map[string]any{
		"sender":  sender,
		"content": content,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("send message: expected 200, got %d (roomID=%s): %s", w.Code, roomID, w.Body.String())
	}
}

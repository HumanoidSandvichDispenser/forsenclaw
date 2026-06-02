package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/api"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/audit"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
	mcpTools "github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp/tools"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	storedb "github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
)

// ---------------------------------------------------------------------------
// Mock inference server
// ---------------------------------------------------------------------------

type mockInferenceServer struct {
	mu    sync.Mutex
	queue []string // complete SSE bodies, served FIFO
	srv   *httptest.Server
}

func newMockInferenceServer(t *testing.T) *mockInferenceServer {
	t.Helper()
	m := &mockInferenceServer{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		m.mu.Lock()
		var body string
		if len(m.queue) > 0 {
			body = m.queue[0]
			m.queue = m.queue[1:]
		} else {
			body = sseTextResponse("(mock: no response queued)")
		}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockInferenceServer) enqueue(body string) {
	m.mu.Lock()
	m.queue = append(m.queue, body)
	m.mu.Unlock()
}

// sseTextResponse returns a complete SSE stream for a simple text reply.
func sseTextResponse(content string) string {
	b1, _ := json.Marshal(map[string]any{
		"id": "1", "object": "chat.completion.chunk", "model": "test",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"role": "assistant", "content": content},
			"finish_reason": nil,
		}},
	})
	b2, _ := json.Marshal(map[string]any{
		"id": "1", "object": "chat.completion.chunk", "model": "test",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	return "data: " + string(b1) + "\n\ndata: " + string(b2) + "\n\ndata: [DONE]\n\n"
}

// sseToolCallResponse returns a complete SSE stream for a single tool call.
func sseToolCallResponse(toolName, callID, argsJSON string) string {
	b1, _ := json.Marshal(map[string]any{
		"id": "1", "object": "chat.completion.chunk", "model": "test",
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"index": 0, "id": callID, "type": "function",
					"function": map[string]any{"name": toolName, "arguments": ""},
				}},
			},
			"finish_reason": nil,
		}},
	})
	b2, _ := json.Marshal(map[string]any{
		"id": "1", "object": "chat.completion.chunk", "model": "test",
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    0,
					"function": map[string]any{"arguments": argsJSON},
				}},
			},
			"finish_reason": nil,
		}},
	})
	b3, _ := json.Marshal(map[string]any{
		"id": "1", "object": "chat.completion.chunk", "model": "test",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "tool_calls",
		}},
	})
	return "data: " + string(b1) + "\n\ndata: " + string(b2) + "\n\ndata: " + string(b3) + "\n\ndata: [DONE]\n\n"
}

// ---------------------------------------------------------------------------
// Mock MCP client
// ---------------------------------------------------------------------------

type mockMCPClient struct {
	toolID    string
	clearance int
	result    string
}

func (m *mockMCPClient) Call(_ context.Context, _ string, _ map[string]string) (string, error) {
	return m.result, nil
}
func (m *mockMCPClient) ToolIDs() []string { return []string{m.toolID} }
func (m *mockMCPClient) Healthy() bool     { return true }

// XMLSchemas satisfies mcp.ToolDescriber (XML mode — unused in native mode).
func (m *mockMCPClient) XMLSchemas() []string { return nil }

// NativeDefinitions satisfies mcp.ToolDescriber for native tool calling.
func (m *mockMCPClient) NativeDefinitions() []inference.ToolDefinition {
	return []inference.ToolDefinition{{
		Name:        m.toolID,
		Description: "A test tool",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"type": "string"},
			},
		},
		Clearance: m.clearance,
	}}
}

// ---------------------------------------------------------------------------
// E2E environment
// ---------------------------------------------------------------------------

type e2eEnv struct {
	serverURL string
	infer     *mockInferenceServer
}

func newE2EEnv(t *testing.T, mcpClients []mcp.NamedMCPClient, mcpClearances map[string]int) *e2eEnv {
	t.Helper()

	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	for _, d := range p.AllDirs() {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Agent config
	agentDir := p.AgentConfigDir("testbot")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent config dir: %v", err)
	}
	const agentYAML = `name: testbot
role_description: "Test agent for E2E tests"
models:
  primary: test-model
  routine: test-model
  sensitive: test-model
feature_flags:
  identity_continuity: false
  daily_notes: false
  proactive_triggers: false
  dreaming: false
clearance: 5
permissions:
  - "tool:invoke/**"
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}

	// Agent data dir (memory file is optional — ReadMemory returns "" if absent)
	if err := os.MkdirAll(p.AgentDataDir("testbot"), 0o755); err != nil {
		t.Fatalf("mkdir agent data dir: %v", err)
	}

	// Mock inference server
	inferSrv := newMockInferenceServer(t)

	// ServerConfig pointing at the mock server
	serverCfg := &config.ServerConfig{
		Listen: "localhost:0",
		Providers: []config.Provider{{
			Name:     "mock",
			Protocol: "openai_compatible",
			BaseURL:  inferSrv.srv.URL,
		}},
		Models: map[string]config.Model{
			"test-model": {Provider: "mock", ProviderModel: "test"},
		},
		Tools: config.ToolsConfig{MaxToolIterations: 10},
		Context: config.ContextConfig{
			CurrentRoomWindow: 50,
			OtherRoomWindow:   10,
			CompactionTrigger: 524288,
			CompactionTarget:  262144,
			MinimumGuaranteed: 20,
		},
	}

	// SQLite store
	sqliteStore, err := storedb.NewSQLiteStore(p.RoomsDBPath())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	// Inference registry
	registry, err := inference.NewRegistry(serverCfg)
	if err != nil {
		sqliteStore.Close()
		t.Fatalf("inference.NewRegistry: %v", err)
	}

	// MCP — always wire the built-in create_room tool (as production does); its
	// actor resolver is set after the agent manager exists (it backs the resolver).
	userActor := room.Actor{ID: "user:test", Name: "test", Type: room.ActorUser, Clearance: 5}
	createRoomTool := mcpTools.NewCreateRoom(sqliteStore, sqliteStore, userActor)
	clients := append([]mcp.NamedMCPClient{{Name: "builtin", Client: createRoomTool}}, mcpClients...)
	clearances := map[string]int{"create_room": 0}
	for k, v := range mcpClearances {
		clearances[k] = v
	}
	mcpReg := mcp.NewRegistry(clients, clearances)
	mcpExec := mcp.NewExecutor(mcpReg, audit.Nop())

	// Hub
	hub := api.NewHub()
	go hub.Run()

	// Assembler + response writer
	assembler := memory.NewAssembler(p, 0, sqliteStore, sqliteStore)

	// Agent manager
	agentMgr, err := agent.NewManager(p, serverCfg, agent.ManagerDeps{
		Registry:       registry,
		Assembler:      assembler,
		Executor:       mcpExec,
		Notifier:       api.NewHubNotifier(hub),
		ResponseWriter: api.NewAgentResponseWriter(sqliteStore, sqliteStore, hub),
	})
	if err != nil {
		sqliteStore.Close()
		t.Fatalf("agent.NewManager: %v", err)
	}

	// Wire the create_room actor resolver now that the agent manager exists.
	createRoomTool.SetResolver(&e2eActorResolver{mgr: agentMgr, user: userActor})

	// Dispatcher
	dispatcher := dispatch.NewDispatcher(agentMgr)

	// API service + router + real HTTP server
	svc := api.NewService(dispatcher, sqliteStore, sqliteStore, agentMgr, assembler, mcpExec, hub, userActor)
	router := chi.NewRouter()
	router.Use(api.AuthMiddleware("test"))
	api.NewAPI(router, svc)

	srv := httptest.NewServer(router)

	t.Cleanup(func() {
		srv.Close()
		agentMgr.Close()
		sqliteStore.Close()
	})

	return &e2eEnv{
		serverURL: srv.URL,
		infer:     inferSrv,
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func createRoom(t *testing.T, serverURL string) int64 {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"participant_ids": []string{"user:alice", "agent:testbot"},
		"clearance":       5,
	})
	resp, err := http.Post(serverURL+"/api/rooms", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create room: status %d: %s", resp.StatusCode, b)
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode room response: %v", err)
	}
	return result.ID
}

func sendMessage(t *testing.T, serverURL string, roomID int64, sender, content string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"sender":  sender,
		"content": content,
	})
	url := serverURL + "/api/rooms/" + strconv.FormatInt(roomID, 10) + "/messages"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("send message: status %d: %s", resp.StatusCode, b)
	}
}

// ---------------------------------------------------------------------------
// WebSocket helpers
// ---------------------------------------------------------------------------

func connectWS(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/api/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func subscribeRoom(t *testing.T, conn *websocket.Conn, roomID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, _ := json.Marshal(map[string]any{"action": "subscribe", "room_id": roomID})
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("subscribe room %d: %v", roomID, err)
	}
}

// awaitEvent reads WebSocket messages until it receives one with the given
// event type, then returns its payload. Times out after 10 seconds.
func awaitEvent(t *testing.T, conn *websocket.Conn, eventType string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("reading ws waiting for %q: %v", eventType, err)
		}
		var evt struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(data, &evt); err != nil {
			continue
		}
		if evt.Type == eventType {
			return evt.Payload
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestE2E_BasicMessageFlow sends a user message and verifies the agent reply
// is written to the transcript and broadcast as a message.created event.
func TestE2E_BasicMessageFlow(t *testing.T) {
	env := newE2EEnv(t, nil, nil)

	roomID := createRoom(t, env.serverURL)

	conn := connectWS(t, env.serverURL)
	subscribeRoom(t, conn, roomID)

	env.infer.enqueue(sseTextResponse("Hello from the agent!"))
	sendMessage(t, env.serverURL, roomID, "user:alice", "Hi agent")

	payload := awaitEvent(t, conn, "message.created")

	content, _ := payload["content"].(string)
	if content != "Hello from the agent!" {
		t.Errorf("expected agent content %q, got %q", "Hello from the agent!", content)
	}

	sender, _ := payload["sender"].(map[string]any)
	senderID, _ := sender["id"].(string)
	if senderID != "agent:testbot" {
		t.Errorf("expected sender agent:testbot, got %q", senderID)
	}
}

// TestE2E_ToolCallRoundTrip verifies the full agent tool call loop:
// agent requests a tool → tool executes → agent produces a final reply.
func TestE2E_ToolCallRoundTrip(t *testing.T) {
	echoTool := &mockMCPClient{
		toolID:    "echo",
		clearance: 5,
		result:    "echoed: hello",
	}
	env := newE2EEnv(t, []mcp.NamedMCPClient{{Name: "test", Client: echoTool}}, map[string]int{"echo": 5})

	roomID := createRoom(t, env.serverURL)

	conn := connectWS(t, env.serverURL)
	subscribeRoom(t, conn, roomID)

	// First inference call: agent requests the echo tool.
	env.infer.enqueue(sseToolCallResponse("echo", "call_1", `{"input":"hello"}`))
	// Second inference call (after tool result): agent returns its final reply.
	env.infer.enqueue(sseTextResponse("Done! Tool said: echoed: hello"))

	sendMessage(t, env.serverURL, roomID, "user:alice", "Use the echo tool please")

	// The round trip produces multiple message.created events (tool call, tool
	// result, final reply). Skip intermediates and wait for a text message.
	var payload map[string]any
	for {
		payload = awaitEvent(t, conn, "message.created")
		if msgType, _ := payload["type"].(string); msgType == "message" {
			break
		}
	}

	content, _ := payload["content"].(string)
	if content != "Done! Tool said: echoed: hello" {
		t.Errorf("expected final agent reply, got %q", content)
	}
}

// e2eActorResolver resolves actor IDs to room.Actor values for the create_room
// tool, backed by the agent manager (mirrors main.agentActorResolver).
type e2eActorResolver struct {
	mgr  *agent.Manager
	user room.Actor
}

func (r *e2eActorResolver) ResolveActor(id string) (room.Actor, error) {
	switch {
	case strings.HasPrefix(id, "user:"):
		return r.user, nil
	case strings.HasPrefix(id, "agent:"):
		name := id[len("agent:"):]
		ag := r.mgr.Get(name)
		if ag == nil {
			return room.Actor{}, fmt.Errorf("agent %q not found", name)
		}
		return room.Actor{
			ID: id, Type: room.ActorAgent,
			Clearance: ag.Definition.Clearance, Name: ag.Definition.Name,
		}, nil
	default:
		return room.Actor{}, fmt.Errorf("invalid actor ID %q", id)
	}
}

// respondConfirmation POSTs a decision to a pending confirmation node.
func respondConfirmation(t *testing.T, serverURL string, roomID int64, nodeID, action string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"action": action})
	url := fmt.Sprintf("%s/api/rooms/%d/confirmations/%s", serverURL, roomID, nodeID)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("respond confirmation: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("respond confirmation: status %d: %s", resp.StatusCode, b)
	}
}

// listRooms fetches all rooms.
func listRooms(t *testing.T, serverURL string) []map[string]any {
	t.Helper()
	resp, err := http.Get(serverURL + "/api/rooms")
	if err != nil {
		t.Fatalf("list rooms: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Rooms []map[string]any `json:"rooms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode rooms: %v", err)
	}
	return result.Rooms
}

// listMessages fetches a room's messages.
func listMessages(t *testing.T, serverURL string, roomID int64) []map[string]any {
	t.Helper()
	url := fmt.Sprintf("%s/api/rooms/%d/messages", serverURL, roomID)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	return result.Messages
}

// TestE2E_CreateRoomDeclassification drives the full write-down declassification
// flow: an agent in a clearance-5 room calls create_room targeting clearance 2,
// which is a Bell-LaPadula write-down. That must yield a confirmation tagged
// blp_write_down; once the user approves, a new clearance-2 room is created and
// seeded with the provenance-tagged summary.
func TestE2E_CreateRoomDeclassification(t *testing.T) {
	env := newE2EEnv(t, nil, nil)

	roomID := createRoom(t, env.serverURL) // clearance 5

	conn := connectWS(t, env.serverURL)
	subscribeRoom(t, conn, roomID)

	// Agent requests create_room at a lower clearance (write-down 5 -> 2).
	env.infer.enqueue(sseToolCallResponse("create_room", "call_1",
		`{"name":"low room","clearance_ceiling":"2","context_summary":"the distilled gist"}`))
	// After the tool runs (post-approval), the agent gives a final reply.
	env.infer.enqueue(sseTextResponse("Spun up the room."))

	sendMessage(t, env.serverURL, roomID, "user:alice", "make a lower-clearance room")

	// The write-down must surface as a confirmation tagged blp_write_down.
	pending := awaitEvent(t, conn, "confirmation.pending")
	if got, _ := pending["tool_name"].(string); got != "create_room" {
		t.Errorf("confirmation tool_name = %q, want create_room", got)
	}
	if got, _ := pending["reason"].(string); got != "blp_write_down" {
		t.Errorf("confirmation reason = %q, want blp_write_down", got)
	}
	nodeID, _ := pending["node_id"].(string)
	if nodeID == "" {
		t.Fatal("confirmation missing node_id")
	}

	// Approve the write-down.
	respondConfirmation(t, env.serverURL, roomID, nodeID, "allow")

	// Wait for the agent's final reply, confirming the loop completed.
	for {
		p := awaitEvent(t, conn, "message.created")
		if mt, _ := p["type"].(string); mt == "message" {
			if c, _ := p["content"].(string); c == "Spun up the room." {
				break
			}
		}
	}

	// A new clearance-2 room named "low room" must now exist.
	var newRoomID int64
	for _, r := range listRooms(t, env.serverURL) {
		if name, _ := r["name"].(string); name == "low room" {
			if cl, _ := r["clearance"].(float64); int(cl) != 2 {
				t.Errorf("new room clearance = %v, want 2", r["clearance"])
			}
			newRoomID = int64(r["id"].(float64))
		}
	}
	if newRoomID == 0 {
		t.Fatal("new declassified room was not created")
	}

	// The new room is seeded with the provenance-tagged summary, classified at 2.
	msgs := listMessages(t, env.serverURL, newRoomID)
	if len(msgs) == 0 {
		t.Fatal("new room has no seed message")
	}
	seed := msgs[0]
	content, _ := seed["content"].(string)
	if !strings.Contains(content, "Declassified into clearance-2 room") {
		t.Errorf("seed missing provenance tag: %q", content)
	}
	if !strings.Contains(content, "the distilled gist") {
		t.Errorf("seed missing summary: %q", content)
	}
	if tag, _ := seed["clearance_tag"].(float64); int(tag) != 2 {
		t.Errorf("seed clearance_tag = %v, want 2", seed["clearance_tag"])
	}
}

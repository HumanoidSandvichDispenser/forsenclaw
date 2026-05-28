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
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
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
func (m *mockMCPClient) Healthy() bool      { return true }

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

func newE2EEnv(t *testing.T, mcpClients []mcp.MCPClient, mcpClearances map[string]int) *e2eEnv {
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
  - "tool:invoke[*]"
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

	// MCP
	mcpReg := mcp.NewRegistry(mcpClients, mcpClearances)
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

	// Dispatcher
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := dispatch.NewDispatcher(agentMgr)
	go dispatcher.Run(ctx)

	// API service + router + real HTTP server
	svc := api.NewService(dispatcher, sqliteStore, sqliteStore, agentMgr, hub)
	router := chi.NewRouter()
	router.Use(api.AuthMiddleware())
	api.NewAPI(router, svc)

	srv := httptest.NewServer(router)

	t.Cleanup(func() {
		srv.Close()
		cancel()
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
	env := newE2EEnv(t, []mcp.MCPClient{echoTool}, map[string]int{"echo": 5})

	roomID := createRoom(t, env.serverURL)

	conn := connectWS(t, env.serverURL)
	subscribeRoom(t, conn, roomID)

	// First inference call: agent requests the echo tool.
	env.infer.enqueue(sseToolCallResponse("echo", "call_1", `{"input":"hello"}`))
	// Second inference call (after tool result): agent returns its final reply.
	env.infer.enqueue(sseTextResponse("Done! Tool said: echoed: hello"))

	sendMessage(t, env.serverURL, roomID, "user:alice", "Use the echo tool please")

	payload := awaitEvent(t, conn, "message.created")

	content, _ := payload["content"].(string)
	if content != "Done! Tool said: echoed: hello" {
		t.Errorf("expected final agent reply, got %q", content)
	}
}

package dispatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// multiResponseProvider returns a different response for each Infer call.
type multiResponseProvider struct {
	mu               sync.Mutex
	responses        []string
	callCount        int
	receivedPayloads []inference.ContextPayload
	usage            inference.Usage
}

func newMultiResponseProvider(responses ...string) *multiResponseProvider {
	return &multiResponseProvider{
		responses: responses,
		usage: inference.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}
}

func (m *multiResponseProvider) Infer(_ context.Context, payload inference.ContextPayload) (<-chan inference.StreamingChunk, error) {
	m.mu.Lock()
	m.callCount++
	m.receivedPayloads = append(m.receivedPayloads, payload)
	idx := m.callCount - 1
	m.mu.Unlock()

	var resp string
	if idx < len(m.responses) {
		resp = m.responses[idx]
	} else {
		resp = m.responses[len(m.responses)-1]
	}

	ch := make(chan inference.StreamingChunk, 1)
	go func() {
		defer close(ch)
		ch <- inference.StreamingChunk{
			Content:      resp,
			FinishReason: "stop",
			Usage:        m.usage,
		}
	}()
	return ch, nil
}

// nativeToolResponseProvider returns responses with native ToolCalls in the final chunk.
type nativeToolResponseProvider struct {
	mu               sync.Mutex
	responses        [][]inference.ToolCallWire
	callCount        int
	receivedPayloads []inference.ContextPayload
	usage            inference.Usage
}

func newNativeToolResponseProvider(calls ...[]inference.ToolCallWire) *nativeToolResponseProvider {
	return &nativeToolResponseProvider{
		responses: calls,
		usage: inference.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}
}

func (m *nativeToolResponseProvider) Infer(_ context.Context, payload inference.ContextPayload) (<-chan inference.StreamingChunk, error) {
	m.mu.Lock()
	m.callCount++
	m.receivedPayloads = append(m.receivedPayloads, payload)
	idx := m.callCount - 1
	m.mu.Unlock()

	var toolCalls []inference.ToolCallWire
	if idx < len(m.responses) {
		toolCalls = m.responses[idx]
	}

	ch := make(chan inference.StreamingChunk, 1)
	go func() {
		defer close(ch)
		ch <- inference.StreamingChunk{
			Content:      "",
			FinishReason: "tool_calls",
			Usage:        m.usage,
			ToolCalls:    toolCalls,
		}
	}()
	return ch, nil
}

type nativeThenTextProvider struct {
	inferFn func(ctx context.Context, payload inference.ContextPayload) (<-chan inference.StreamingChunk, error)
}

func (m *nativeThenTextProvider) Infer(ctx context.Context, payload inference.ContextPayload) (<-chan inference.StreamingChunk, error) {
	return m.inferFn(ctx, payload)
}

// --- mock MCP types for tool loop tests ---

type mockLoopMCPClient struct {
	toolIDs   []string
	response  string
	callCount int
}

func (m *mockLoopMCPClient) Call(_ context.Context, _ string, _ map[string]string) (string, error) {
	m.callCount++
	return m.response, nil
}
func (m *mockLoopMCPClient) ToolIDs() []string { return m.toolIDs }
func (m *mockLoopMCPClient) Healthy() bool     { return true }

// --- helpers ---

func newToolLoopDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	p := paths.NewPathsFromRoots(t.TempDir(), t.TempDir(), t.TempDir())
	return &Dispatcher{hub: nil, paths: p, transcripts: make(map[string]*room.TranscriptWriter)}
}

func newTestAgent(t *testing.T, perms []string) *agent.Agent {
	t.Helper()
	def := &config.AgentDefinition{
		Name:            "test-agent",
		RoleDescription: "Test.",
		Models:          config.AgentModels{Primary: "test-model"},
		Clearance:       5,
		RawPermissions:  perms,
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	return ag
}

func newTestAssembled() *memory.AssembledContext {
	return &memory.AssembledContext{
		SystemPrompt: "You are a test agent.",
		RFC:          "What is the weather?",
	}
}

func toolCallXML(toolID string) string {
	return fmt.Sprintf(`<tool_call><tool_id>%s</tool_id><parameters><q>test</q></parameters></tool_call>`, toolID)
}

func newAllowExecutor(toolID, mcpResponse string, maxIter int) (*mcp.Executor, *mockLoopMCPClient) {
	client := &mockLoopMCPClient{toolIDs: []string{toolID}, response: mcpResponse}
	reg := mcp.NewRegistry([]mcp.MCPClient{client})
	perms := []config.Permission{
		{Action: "tool:invoke", Scope: toolID, Effect: "allow"},
	}
	exec := mcp.NewExecutor(reg, perms, nil, nil, mcp.ExecutorConfig{MaxIterations: maxIter})
	return exec, client
}

// --- AC-12: Loop — single tool call resolved, final answer returned ---

func TestRunToolLoop_SingleToolCall(t *testing.T) {
	d := newToolLoopDispatcher(t)
	ag := newTestAgent(t, []string{"tool:invoke[web_search]:allow"})
	assembled := newTestAssembled()

	toolResp := toolCallXML("web_search")
	provider := newMultiResponseProvider(toolResp, "Here is the weather: sunny.")

	exec, client := newAllowExecutor("web_search", "sunny day", 10)

	prose, _, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, provider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantProse := "<tool_use>{\"args\":{\"q\":\"test\"},\"name\":\"web_search\",\"result\":\"sunny day\"}</tool_use>\n\nHere is the weather: sunny."
	if prose != wantProse {
		t.Errorf("expected final prose, got %q", prose)
	}
	if provider.callCount != 2 {
		t.Errorf("expected provider called 2 times, got %d", provider.callCount)
	}
	if client.callCount != 1 {
		t.Errorf("expected MCP client called 1 time, got %d", client.callCount)
	}
}

// --- AC-12b: Loop — single native tool call resolved, final answer returned ---

func TestRunToolLoop_SingleNativeToolCall(t *testing.T) {
	d := newToolLoopDispatcher(t)
	ag := newTestAgent(t, []string{"tool:invoke[web_search]:allow"})
	assembled := newTestAssembled()

	exec, client := newAllowExecutor("web_search", "sunny day", 10)

	// Custom provider that returns native tool calls on first call, prose on second.
	callCount := 0
	var receivedPayloads []inference.ContextPayload
	customProvider := &nativeThenTextProvider{
		inferFn: func(_ context.Context, payload inference.ContextPayload) (<-chan inference.StreamingChunk, error) {
			callCount++
			receivedPayloads = append(receivedPayloads, payload)
			ch := make(chan inference.StreamingChunk, 1)
			go func() {
				defer close(ch)
				if callCount == 1 {
					ch <- inference.StreamingChunk{
						Content:      "",
						FinishReason: "tool_calls",
						Usage:        inference.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
						ToolCalls: []inference.ToolCallWire{
							{ID: "call_1", Type: "function", Function: inference.ToolFunctionWire{Name: "web_search", Arguments: `{"q":"weather"}`}},
						},
					}
				} else {
					ch <- inference.StreamingChunk{
						Content:      "The weather is sunny.",
						FinishReason: "stop",
						Usage:        inference.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
					}
				}
			}()
			return ch, nil
		},
	}

	prose, _, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, customProvider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantProse := "<tool_use>{\"args\":{\"q\":\"weather\"},\"name\":\"web_search\",\"result\":\"sunny day\"}</tool_use>\n\nThe weather is sunny."
	if prose != wantProse {
		t.Errorf("expected final prose, got %q", prose)
	}
	if callCount != 2 {
		t.Errorf("expected provider called 2 times, got %d", callCount)
	}
	if client.callCount != 1 {
		t.Errorf("expected MCP client called 1 time, got %d", client.callCount)
	}

	// Verify native tool call turn was injected into history for the second call.
	if len(receivedPayloads) < 2 {
		t.Fatalf("expected at least 2 payloads, got %d", len(receivedPayloads))
	}
	secondHistory := receivedPayloads[1].History
	if len(secondHistory) < 2 {
		t.Fatalf("expected at least 2 history messages in second call, got %d", len(secondHistory))
	}
	assistantTurn := secondHistory[len(secondHistory)-2]
	if assistantTurn.Role != inference.RoleAssistant {
		t.Errorf("expected assistant role, got %q", assistantTurn.Role)
	}
	if len(assistantTurn.ToolCalls) != 1 || assistantTurn.ToolCalls[0].Function.Name != "web_search" {
		t.Errorf("expected native tool call in assistant turn")
	}
	toolTurn := secondHistory[len(secondHistory)-1]
	if toolTurn.Role != inference.RoleTool {
		t.Errorf("expected tool role, got %q", toolTurn.Role)
	}
	if toolTurn.ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %q", toolTurn.ToolCallID)
	}
	if toolTurn.Content != "sunny day" {
		t.Errorf("expected tool turn content 'sunny day', got %q", toolTurn.Content)
	}
}

// --- AC-13: Loop — no tool calls, returns immediately ---

func TestRunToolLoop_NoToolCalls(t *testing.T) {
	d := newToolLoopDispatcher(t)
	ag := newTestAgent(t, nil)
	assembled := newTestAssembled()

	provider := newMultiResponseProvider("Plain answer, no tools needed.")
	exec, _ := newAllowExecutor("unused", "unused", 10)

	prose, _, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, provider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prose != "Plain answer, no tools needed." {
		t.Errorf("expected plain response, got %q", prose)
	}
	if provider.callCount != 1 {
		t.Errorf("expected provider called 1 time, got %d", provider.callCount)
	}
}

// --- AC-14: Loop — iteration cap terminates the loop ---

func TestRunToolLoop_IterationCap(t *testing.T) {
	d := newToolLoopDispatcher(t)
	ag := newTestAgent(t, []string{"tool:invoke[web_search]:allow"})
	assembled := newTestAssembled()

	// Always return a tool call — loop should terminate at cap.
	provider := newMultiResponseProvider(toolCallXML("web_search"))
	exec, _ := newAllowExecutor("web_search", "result", 3)

	_, _, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, provider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.callCount != 3 {
		t.Errorf("expected provider called 3 times (cap), got %d", provider.callCount)
	}
}

// --- AC-15: Loop — usage is accumulated across iterations ---

func TestRunToolLoop_UsageAccumulated(t *testing.T) {
	d := newToolLoopDispatcher(t)
	ag := newTestAgent(t, []string{"tool:invoke[web_search]:allow"})
	assembled := newTestAssembled()

	provider := newMultiResponseProvider(toolCallXML("web_search"), "Final answer.")
	exec, _ := newAllowExecutor("web_search", "result", 10)

	_, usage, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, provider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Each iteration contributes TotalTokens=15; 2 iterations = 30.
	if usage.TotalTokens != 30 {
		t.Errorf("expected TotalTokens=30, got %d", usage.TotalTokens)
	}
}

// --- AC-16: Loop — tool result injected into history on next call ---

func TestRunToolLoop_ToolResultInjectedInHistory(t *testing.T) {
	d := newToolLoopDispatcher(t)
	ag := newTestAgent(t, []string{"tool:invoke[web_search]:allow"})
	assembled := newTestAssembled()

	provider := newMultiResponseProvider(toolCallXML("web_search"), "Done.")
	exec, _ := newAllowExecutor("web_search", "search results", 10)

	_, _, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, provider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The second Infer call should have received the tool call + result in History.
	if len(provider.receivedPayloads) < 2 {
		t.Fatalf("expected at least 2 payloads received, got %d", len(provider.receivedPayloads))
	}
	secondHistory := provider.receivedPayloads[1].History

	// Should have: assistant tool-call turn + tool result turn.
	if len(secondHistory) < 2 {
		t.Fatalf("expected at least 2 history messages in second call, got %d", len(secondHistory))
	}

	assistantTurn := secondHistory[len(secondHistory)-2]
	if assistantTurn.Role != inference.RoleAssistant {
		t.Errorf("expected assistant role, got %q", assistantTurn.Role)
	}
	// XML branch: assistant turn contains the raw response with tool_call XML.
	if !strings.Contains(assistantTurn.Content, "<tool_call>") {
		t.Errorf("expected assistant content to contain XML tool_call, got %q", assistantTurn.Content)
	}

	toolTurn := secondHistory[len(secondHistory)-1]
	// XML branch: tool results are injected as user-role messages.
	if toolTurn.Role != inference.RoleUser {
		t.Errorf("expected user role for XML tool result, got %q", toolTurn.Role)
	}
	if !strings.Contains(toolTurn.Content, "<tool_response") {
		t.Errorf("expected XML tool_response in content, got %q", toolTurn.Content)
	}
	if !strings.Contains(toolTurn.Content, "search results") {
		t.Errorf("expected tool result in content, got %q", toolTurn.Content)
	}
}

// --- AC-17: Loop — deny result injected and loop continues ---

func TestRunToolLoop_DenyResultContinues(t *testing.T) {
	d := newToolLoopDispatcher(t)
	// Agent has no permission for web_search — all calls will be denied.
	ag := newTestAgent(t, nil)
	assembled := newTestAssembled()

	provider := newMultiResponseProvider(toolCallXML("web_search"), "Final after deny.")
	// Use a nil-registry executor (deny via permissions).
	perms := []config.Permission{}
	exec := mcp.NewExecutor(nil, perms, nil, nil, mcp.ExecutorConfig{MaxIterations: 10})

	prose, _, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, provider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("expected no error even on deny, got: %v", err)
	}
	wantProse := "<tool_use>{\"args\":{\"q\":\"test\"},\"name\":\"web_search\",\"result\":\"error: permission denied: tool:invoke[web_search]\"}</tool_use>\n\nFinal after deny."
	if prose != wantProse {
		t.Errorf("expected final prose after denied tool, got %q", prose)
	}
	if provider.callCount != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.callCount)
	}

	// The second call should contain the deny error in history.
	if len(provider.receivedPayloads) >= 2 {
		secondHistory := provider.receivedPayloads[1].History
		found := false
		for _, h := range secondHistory {
			// XML branch: deny errors are injected as user-role messages.
			if h.Role == inference.RoleUser && strings.Contains(h.Content, "permission denied") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected permission denied error in history for second call")
		}
	}
}

// --- AC-18: processRFC integration — tool calls do not reach transcript ---

func newIntegrationDispatcher(t *testing.T) (*Dispatcher, *paths.Paths, *agent.Agent, *room.Room, *room.SQLiteStore) {
	t.Helper()

	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)

	for _, path := range []string{p.DBDir(), p.AgentsDataDir(), p.RoomsDir()} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	agentDir := p.AgentDataDir("test-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, memory.MemoryFileName), []byte(""), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	store, err := room.NewSQLiteStore(p.RoomsDBPath())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	def := &config.AgentDefinition{
		Name:            "test-agent",
		RoleDescription: "You are a test agent.",
		Models:          config.AgentModels{Primary: "test-model"},
		Clearance:       5,
		RawPermissions:  []string{"tool:invoke[web_search]:allow"},
	}
	ag, err := agent.NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	assembler := memory.NewAssembler(p, 4096)
	ctxCfg := config.DefaultContextConfig()
	d := NewDispatcher(nil, nil, assembler, store, p, ctxCfg)

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	agentActor := room.Actor{ID: "agent:test-agent", Type: room.ActorAgent, Clearance: 5, Name: "TestAgent"}
	r := &room.Room{
		ID:               uuid.New().String(),
		Participants:     []room.Actor{alice, agentActor},
		ClearanceCeiling: 5,
		ProtocolType:     room.ProtocolFreeForm,
	}
	ctx := context.Background()
	if err := store.CreateRoom(ctx, r); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	return d, p, ag, r, store
}

func TestProcessRFC_ToolCallNotInTranscript(t *testing.T) {
	d, p, ag, r, store := newIntegrationDispatcher(t)
	defer store.Close()

	toolResp := toolCallXML("web_search")
	finalResp := "The weather is sunny."

	mcpClient := &mockLoopMCPClient{toolIDs: []string{"web_search"}, response: "sunny"}
	mcpReg := mcp.NewRegistry([]mcp.MCPClient{mcpClient})
	d.SetMCPRegistry(mcpReg)

	provider := newMultiResponseProvider(toolResp, finalResp)
	d.registry = newMockRegistry(provider, "test-model")

	// Write a user message to the room transcript first.
	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	ctx := context.Background()
	userMsg := room.Message{
		ID:           uuid.New().String(),
		Timestamp:    time.Now().UTC(),
		RoomID:       r.ID,
		Sender:       alice,
		ClearanceTag: 5,
		Type:         room.MessageText,
		Content:      "What's the weather?",
	}
	writer, err := room.NewTranscriptWriter(p.RoomsDir(), r.ID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	if err := writer.Append(ctx, userMsg); err != nil {
		t.Fatalf("Append: %v", err)
	}
	writer.Close()

	rfc := room.RFC{
		ID:     uuid.New().String(),
		RoomID: r.ID,
		Target: "agent:test-agent",
	}

	if err := d.processRFC(ctx, ag, rfc); err != nil {
		t.Fatalf("processRFC: %v", err)
	}

	msgs, err := room.ReadMessages(ctx, p.RoomsDir(), r.ID, room.ReadOpts{})
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}

	// Should have: user message + tool_call + tool_result + final agent response.
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages in transcript, got %d", len(msgs))
	}
	if msgs[1].Type != room.MessageToolCall {
		t.Errorf("expected message 1 to be tool_call, got %q", msgs[1].Type)
	}
	if msgs[2].Type != room.MessageToolResult {
		t.Errorf("expected message 2 to be tool_result, got %q", msgs[2].Type)
	}
	wantFinal := "<tool_use>{\"args\":{\"q\":\"test\"},\"name\":\"web_search\",\"result\":\"sunny\"}</tool_use>\n\nThe weather is sunny."
	if msgs[3].Content != wantFinal {
		t.Errorf("expected final prose in transcript, got %q", msgs[3].Content)
	}
	if strings.Contains(msgs[3].Content, "<tool_call>") {
		t.Error("final transcript message should not contain raw tool_call XML")
	}
}

// --- AC-19: processRFC integration — final message usage reflects all iterations ---

func TestProcessRFC_UsageReflectsAllIterations(t *testing.T) {
	d, p, ag, r, store := newIntegrationDispatcher(t)
	defer store.Close()

	toolResp := toolCallXML("web_search")
	finalResp := "Result."

	mcpClient := &mockLoopMCPClient{toolIDs: []string{"web_search"}, response: "data"}
	mcpReg := mcp.NewRegistry([]mcp.MCPClient{mcpClient})
	d.SetMCPRegistry(mcpReg)

	provider := newMultiResponseProvider(toolResp, finalResp)
	d.registry = newMockRegistry(provider, "test-model")

	alice := room.Actor{ID: "user:alice", Type: room.ActorUser, Clearance: 5, Name: "Alice"}
	ctx := context.Background()
	userMsg := room.Message{
		ID:           uuid.New().String(),
		Timestamp:    time.Now().UTC(),
		RoomID:       r.ID,
		Sender:       alice,
		ClearanceTag: 5,
		Type:         room.MessageText,
		Content:      "Give me data.",
	}
	writer, err := room.NewTranscriptWriter(p.RoomsDir(), r.ID)
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	if err := writer.Append(ctx, userMsg); err != nil {
		t.Fatalf("Append: %v", err)
	}
	writer.Close()

	rfc := room.RFC{
		ID:     uuid.New().String(),
		RoomID: r.ID,
		Target: "agent:test-agent",
	}

	if err := d.processRFC(ctx, ag, rfc); err != nil {
		t.Fatalf("processRFC: %v", err)
	}

	msgs, err := room.ReadMessages(ctx, p.RoomsDir(), r.ID, room.ReadOpts{})
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}

	agentMsg := msgs[len(msgs)-1]
	if agentMsg.Usage == nil {
		t.Fatal("expected usage on agent message")
	}
	// 2 iterations × 15 tokens each = 30 total.
	if agentMsg.Usage.InputTokens+agentMsg.Usage.OutputTokens != 30 {
		t.Errorf("expected cumulative usage of 30 tokens, got %d+%d=%d",
			agentMsg.Usage.InputTokens, agentMsg.Usage.OutputTokens,
			agentMsg.Usage.InputTokens+agentMsg.Usage.OutputTokens)
	}
}

// --- AC-20: Loop — prose is not broadcast twice to WebSocket clients ---

type recordingBroadcaster struct {
	mu     sync.Mutex
	events []StreamEvent
}

func (r *recordingBroadcaster) Broadcast(roomID string, event StreamEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingBroadcaster) chunkEvents() []StreamEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var chunks []StreamEvent
	for _, ev := range r.events {
		if ev.Type == "chunk" {
			chunks = append(chunks, ev)
		}
	}
	return chunks
}

func TestRunToolLoop_NoDuplicateProseBroadcast(t *testing.T) {
	rec := &recordingBroadcaster{}
	p := paths.NewPathsFromRoots(t.TempDir(), t.TempDir(), t.TempDir())
	d := &Dispatcher{hub: rec, paths: p, transcripts: make(map[string]*room.TranscriptWriter)}
	ag := newTestAgent(t, []string{"tool:invoke[web_search]:allow"})
	assembled := newTestAssembled()

	// Response with prose before the tool call.
	resp := `Thinking about it... ` + toolCallXML("web_search")
	provider := newMultiResponseProvider(resp, "Final answer.")
	exec, _ := newAllowExecutor("web_search", "result", 10)

	_, _, err := d.runToolLoop(context.Background(), ag, room.RFC{ID: "rfc1", RoomID: "room1"}, provider, "test-model", assembled, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// broadcastAndCollect already broadcast the raw response as a chunk.
	// runToolLoop must NOT broadcast the parsed prose a second time.
	chunks := rec.chunkEvents()
	var proseChunkCount int
	for _, ev := range chunks {
		if ev.Content == "Thinking about it..." {
			proseChunkCount++
		}
	}
	if proseChunkCount != 0 {
		t.Errorf("expected prose chunk broadcast 0 times (broadcastAndCollect already sent it), got %d", proseChunkCount)
	}
}

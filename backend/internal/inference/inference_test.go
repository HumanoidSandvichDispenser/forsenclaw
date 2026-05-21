package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
)

// --- mock provider ---

type mockProvider struct {
	mu       sync.Mutex
	calls    int
	response []StreamingChunk
	err      error
}

func (m *mockProvider) Infer(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()

	if m.err != nil {
		return nil, m.err
	}

	ch := make(chan StreamingChunk, len(m.response))
	for _, c := range m.response {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// --- tests for core types ---

func TestValidateContextPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload ContextPayload
		wantErr string
	}{
		{
			name:    "missing model",
			payload: ContextPayload{RFC: "hi"},
			wantErr: "model is required",
		},
		{
			name:    "empty payload",
			payload: ContextPayload{Model: "test"},
			wantErr: "at least one content field is required",
		},
		{
			name:    "valid with RFC only",
			payload: ContextPayload{Model: "test", RFC: "hi"},
			wantErr: "",
		},
		{
			name:    "valid with system prompt only",
			payload: ContextPayload{Model: "test", SystemPrompt: "You are helpful."},
			wantErr: "",
		},
		{
			name:    "valid with memory only",
			payload: ContextPayload{Model: "test", Memory: "Some memory."},
			wantErr: "",
		},
		{
			name:    "valid with daily notes only",
			payload: ContextPayload{Model: "test", DailyNotes: []string{"note"}},
			wantErr: "",
		},
		{
			name:    "valid with RAG only",
			payload: ContextPayload{Model: "test", RAGResults: []string{"result"}},
			wantErr: "",
		},
		{
			name:    "valid with tools only",
			payload: ContextPayload{Model: "test", ToolSchemas: []string{"tool"}},
			wantErr: "",
		},
		{
			name:    "valid with cross-room feed only",
			payload: ContextPayload{Model: "test", CrossRoomFeed: []string{"feed"}},
			wantErr: "",
		},
		{
			name:    "valid with history only",
			payload: ContextPayload{Model: "test", History: []HistoryMessage{{Role: RoleUser, Content: "hi"}}},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContextPayload(tt.payload)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     InferRequest
		wantErr string
	}{
		{
			name:    "missing model",
			req:     InferRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}},
			wantErr: "model is required",
		},
		{
			name:    "missing messages",
			req:     InferRequest{Model: "test"},
			wantErr: "at least one message is required",
		},
		{
			name:    "missing role",
			req:     InferRequest{Model: "test", Messages: []Message{{Content: "hi"}}},
			wantErr: "role is required",
		},
		{
			name: "valid",
			req: InferRequest{
				Model:    "test",
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequest(tt.req)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestInferSync(t *testing.T) {
	mock := &mockProvider{
		response: []StreamingChunk{
			{Content: "Hello"},
			{Content: " world"},
			{Content: "!", FinishReason: "stop", Usage: Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13}},
		},
	}

	content, usage, err := InferSync(context.Background(), mock, ContextPayload{
		Model: "test",
		RFC:   "hi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "Hello world!" {
		t.Fatalf("expected 'Hello world!', got %q", content)
	}
	if usage.TotalTokens != 13 {
		t.Fatalf("expected 13 total tokens, got %d", usage.TotalTokens)
	}
}

// --- tests for registry ---

func TestRegistryResolve(t *testing.T) {
	cfg := &config.ServerConfig{
		Providers: []config.Provider{
			{Name: "ollama", Protocol: "openai_compatible", BaseURL: "http://localhost:11434"},
			{
				Name:     "anthropic",
				Protocol: "anthropic",
				BaseURL:  "https://api.anthropic.com",
				APIKey:   config.EnvString("${ANTHROPIC_API_KEY}"),
			},
		},
		Models: map[string]config.Model{
			"gemma-local":   {Provider: "ollama", ProviderModel: "gemma3:12b"},
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
		},
	}

	// Set the API key env var for the test
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry failed: %v", err)
	}

	// Resolve by model key
	prov, modelID, err := reg.Resolve("gemma-local")
	if err != nil {
		t.Fatalf("Resolve gemma-local failed: %v", err)
	}
	if prov == nil {
		t.Fatal("expected provider, got nil")
	}
	if modelID != "gemma3:12b" {
		t.Fatalf("expected model ID gemma3:12b, got %q", modelID)
	}

	// Resolve unknown model
	_, _, err = reg.Resolve("unknown")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestRegistryResolveTier(t *testing.T) {
	cfg := &config.ServerConfig{
		Providers: []config.Provider{
			{Name: "ollama", Protocol: "openai_compatible", BaseURL: "http://localhost:11434"},
		},
		Models: map[string]config.Model{
			"gemma": {Provider: "ollama", ProviderModel: "gemma3:12b"},
		},
	}

	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry failed: %v", err)
	}

	agent := &config.AgentDefinition{
		Name: "test",
		Models: config.AgentModels{
			Primary:   "gemma",
			Routine:   "gemma",
			Sensitive: "gemma",
		},
	}

	_, modelID, err := reg.ResolveTier(agent, TierPrimary)
	if err != nil {
		t.Fatalf("ResolveTier failed: %v", err)
	}
	if modelID != "gemma3:12b" {
		t.Fatalf("expected model ID gemma3:12b, got %q", modelID)
	}
}

func TestRegistryMissingProvider(t *testing.T) {
	cfg := &config.ServerConfig{
		Providers: []config.Provider{
			{Name: "ollama", Protocol: "openai_compatible", BaseURL: "http://localhost:11434"},
		},
		Models: map[string]config.Model{
			"bad": {Provider: "missing", ProviderModel: "x"},
		},
	}

	_, err := NewRegistry(cfg)
	if err == nil {
		t.Fatal("expected error for model referencing missing provider")
	}
}

// --- tests for retry ---

func TestRetryingProviderSuccess(t *testing.T) {
	mock := &mockProvider{
		response: []StreamingChunk{{Content: "ok", FinishReason: "stop"}},
	}

	rp := NewRetryingProvider(mock, RetryConfig{MaxRetries: 2, BaseDelay: 10 * time.Millisecond})
	ch, err := rp.Infer(context.Background(), ContextPayload{Model: "test", RFC: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []StreamingChunk
	for c := range ch {
		chunks = append(chunks, c)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if mock.CallCount() != 1 {
		t.Fatalf("expected 1 call, got %d", mock.CallCount())
	}
}

func TestRetryingProviderRetriesThenSucceeds(t *testing.T) {
	failures := 2

	// We'll override the Infer method to fail N times then succeed
	callCount := 0
	customProvider := &mockProviderWithFunc{
		inferFn: func(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
			callCount++
			if callCount <= failures {
				return nil, &httpError{StatusCode: 503, Message: "service unavailable"}
			}
			ch := make(chan StreamingChunk, 1)
			ch <- StreamingChunk{Content: "ok", FinishReason: "stop"}
			close(ch)
			return ch, nil
		},
	}

	rp := NewRetryingProvider(customProvider, RetryConfig{MaxRetries: 3, BaseDelay: 10 * time.Millisecond})
	ch, err := rp.Infer(context.Background(), ContextPayload{Model: "test", RFC: "hi"})
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}

	var chunks []StreamingChunk
	for c := range ch {
		chunks = append(chunks, c)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if callCount != failures+1 {
		t.Fatalf("expected %d calls, got %d", failures+1, callCount)
	}
}

func TestRetryingProviderExhausted(t *testing.T) {
	customProvider := &mockProviderWithFunc{
		inferFn: func(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
			return nil, &httpError{StatusCode: 503, Message: "service unavailable"}
		},
	}

	rp := NewRetryingProvider(customProvider, RetryConfig{MaxRetries: 2, BaseDelay: 10 * time.Millisecond})
	_, err := rp.Infer(context.Background(), ContextPayload{Model: "test", RFC: "hi"})
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
}

func TestRetryingProviderNonRetryable(t *testing.T) {
	customProvider := &mockProviderWithFunc{
		inferFn: func(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
			return nil, &httpError{StatusCode: 400, Message: "bad request"}
		},
	}

	rp := NewRetryingProvider(customProvider, RetryConfig{MaxRetries: 3, BaseDelay: 10 * time.Millisecond})
	_, err := rp.Infer(context.Background(), ContextPayload{Model: "test", RFC: "hi"})
	if err == nil {
		t.Fatal("expected error for non-retryable status")
	}
}

type mockProviderWithFunc struct {
	inferFn func(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error)
}

func (m *mockProviderWithFunc) Infer(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
	return m.inferFn(ctx, payload)
}

// --- tests for OpenAI-compatible adapter ---

func TestOpenAICompatibleAdapterInfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hello"}}]}`,
			`{"id":"2","object":"chat.completion.chunk","choices":[{"delta":{"content":" world"}}]}`,
			`{"id":"3","object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
		}

		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "", "native")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter failed: %v", err)
	}

	ch, err := adapter.Infer(context.Background(), ContextPayload{
		Model: "gemma",
		RFC:   "hi",
	})
	if err != nil {
		t.Fatalf("Infer failed: %v", err)
	}

	var content string
	var final StreamingChunk
	for chunk := range ch {
		content += chunk.Content
		if chunk.FinishReason != "" {
			final = chunk
		}
	}

	if content != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", content)
	}
	if final.FinishReason != "stop" {
		t.Fatalf("expected finish_reason 'stop', got %q", final.FinishReason)
	}
	if final.Usage.TotalTokens != 7 {
		t.Fatalf("expected 7 total tokens, got %d", final.Usage.TotalTokens)
	}
}

func TestOpenAICompatibleAdapterError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid key"})
	}))
	defer server.Close()

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "bad-key", "native")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter failed: %v", err)
	}

	_, err = adapter.Infer(context.Background(), ContextPayload{
		Model: "gemma",
		RFC:   "hi",
	})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

// --- tests for Anthropic adapter ---

func TestAnthropicAdapterInfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key 'test-key', got %q", r.Header.Get("x-api-key"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\"}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}

		for _, e := range events {
			fmt.Fprint(w, e)
		}
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	adapter, err := NewAnthropicAdapter(server.URL, "test-key")
	if err != nil {
		t.Fatalf("NewAnthropicAdapter failed: %v", err)
	}

	ch, err := adapter.Infer(context.Background(), ContextPayload{
		Model:        "claude-sonnet",
		SystemPrompt: "You are a helpful assistant.",
		RFC:          "hi",
	})
	if err != nil {
		t.Fatalf("Infer failed: %v", err)
	}

	var content string
	var final StreamingChunk
	for chunk := range ch {
		content += chunk.Content
		if chunk.FinishReason != "" {
			final = chunk
		}
	}

	if content != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", content)
	}
	if final.FinishReason != "end_turn" {
		t.Fatalf("expected finish_reason 'end_turn', got %q", final.FinishReason)
	}
	if final.Usage.TotalTokens != 7 {
		t.Fatalf("expected 7 total tokens, got %d", final.Usage.TotalTokens)
	}
}

func TestAnthropicAdapterMissingKey(t *testing.T) {
	_, err := NewAnthropicAdapter("https://api.anthropic.com", "")
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

// --- AC-20: Provider adapters — RoleTool history message serialised correctly ---

func TestOpenAICompatibleAdapter_RoleToolHistory(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "", "native")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}

	payload := ContextPayload{
		Model:        "gemma",
		SystemPrompt: "You are a test agent.",
		History: []HistoryMessage{
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCallWire{{
					ID:   "call_1",
					Type: "function",
					Function: ToolFunctionWire{
						Name:      "some_tool",
						Arguments: `{"query":"test"}`,
					},
				}},
			},
			{Role: RoleTool, Name: "some_tool", ToolCallID: "call_1", Content: "result"},
		},
		RFC: "hi",
	}

	ch, err := adapter.Infer(context.Background(), payload)
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	for range ch {
	}

	var req openaiCompatRequest
	if err := json.Unmarshal([]byte(capturedBody), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	// Expect: system, assistant (history with tool_calls), tool (history), RFC user.
	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(req.Messages))
	}

	assistantMsg := req.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("expected assistant role, got %q", assistantMsg.Role)
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].Function.Name != "some_tool" {
		t.Errorf("expected tool call name 'some_tool', got %q", assistantMsg.ToolCalls[0].Function.Name)
	}

	toolMsg := req.Messages[2]
	if toolMsg.Role != "tool" {
		t.Errorf("expected role 'tool', got %q", toolMsg.Role)
	}
	if toolMsg.Content == nil || *toolMsg.Content != "result" {
		t.Errorf("expected content 'result', got %v", toolMsg.Content)
	}
	if toolMsg.ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %q", toolMsg.ToolCallID)
	}
	if toolMsg.Name != "some_tool" {
		t.Errorf("expected tool name 'some_tool', got %q", toolMsg.Name)
	}
}

func TestAnthropicAdapter_RoleToolHistory(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		events := []string{
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, e := range events {
			fmt.Fprint(w, e)
		}
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	adapter, err := NewAnthropicAdapter(server.URL, "test-key")
	if err != nil {
		t.Fatalf("NewAnthropicAdapter: %v", err)
	}

	payload := ContextPayload{
		Model:        "claude-sonnet",
		SystemPrompt: "You are a test agent.",
		History: []HistoryMessage{
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCallWire{{
					ID:   "call_1",
					Type: "function",
					Function: ToolFunctionWire{
						Name:      "some_tool",
						Arguments: `{"query":"test"}`,
					},
				}},
			},
			{Role: RoleTool, Name: "some_tool", ToolCallID: "call_1", Content: "result"},
		},
		RFC: "hi",
	}

	ch, err := adapter.Infer(context.Background(), payload)
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	for range ch {
	}

	var body anthropicRequestBody
	if err := json.Unmarshal([]byte(capturedBody), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	// Native Anthropic protocol:
	// - assistant message with tool_use content block
	// - user message with tool_result content block (merged with RFC user message)
	if len(body.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(body.Messages))
	}

	// First message should be assistant with tool_use block.
	if body.Messages[0].Role != "assistant" {
		t.Errorf("expected first message role 'assistant', got %q", body.Messages[0].Role)
	}
	blocks0, ok := body.Messages[0].Content.([]interface{})
	if !ok || len(blocks0) == 0 {
		t.Fatalf("expected assistant message to have content blocks, got %+v", body.Messages[0].Content)
	}
	firstBlock := blocks0[0].(map[string]interface{})
	if firstBlock["type"] != "tool_use" {
		t.Errorf("expected first block type 'tool_use', got %v", firstBlock["type"])
	}
	if firstBlock["id"] != "call_1" {
		t.Errorf("expected tool_use id 'call_1', got %v", firstBlock["id"])
	}
	if firstBlock["name"] != "some_tool" {
		t.Errorf("expected tool_use name 'some_tool', got %v", firstBlock["name"])
	}

	// Find the user message that contains the tool_result block.
	foundToolResult := false
	for _, msg := range body.Messages {
		if msg.Role == "user" {
			blocks, ok := msg.Content.([]interface{})
			if ok {
				for _, b := range blocks {
					block := b.(map[string]interface{})
					if block["type"] == "tool_result" && block["content"] == "result" {
						foundToolResult = true
						break
					}
				}
			}
		}
	}
	if !foundToolResult {
		t.Error("expected tool_result content block in a user-role message in Anthropic request")
	}

	// No message should have role "tool".
	for _, msg := range body.Messages {
		if msg.Role == "tool" {
			t.Errorf("Anthropic adapter should not emit role 'tool', got message: %+v", msg)
		}
	}
}

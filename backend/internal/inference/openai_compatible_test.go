package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatibleAdapter_SystemPromptWithContext(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		capturedBody = string(bodyBytes)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "", "xml")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter failed: %v", err)
	}

	payload := ContextPayload{
		Model:        "gemma",
		SystemPrompt: "You are an assistant. Use <thinking> tags.",
		Memory:       "User said: 5 < 10 && 10 > 5",
		Request:      "What about <script>alert(1)</script>?",
	}

	ch, err := adapter.Infer(context.Background(), payload)
	if err != nil {
		t.Fatalf("Infer failed: %v", err)
	}
	for range ch {
	}

	// Verify the captured request body by unmarshaling the JSON request.
	var req openaiCompatRequest
	if err := json.Unmarshal([]byte(capturedBody), &req); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	// Expect 2 messages: system (with context), user Request.
	// History is empty in this test, so no history messages are added.
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(req.Messages))
	}

	// Verify system prompt is sent as the dedicated system message.
	if req.Messages[0].Role != "system" {
		t.Errorf("expected first message role to be system, got %q", req.Messages[0].Role)
	}
	if req.Messages[0].Content == nil {
		t.Fatal("expected system message content")
	}
	sysContent := *req.Messages[0].Content
	if !strings.Contains(sysContent, "You are an assistant. Use <thinking> tags.") {
		t.Errorf("system prompt missing base text")
	}
	if !strings.Contains(sysContent, "## Memory") {
		t.Errorf("memory section missing in system prompt")
	}
	if !strings.Contains(sysContent, "User said: 5 < 10 && 10 > 5") {
		t.Errorf("memory content missing in system prompt")
	}

	// messages[1] is the Request as a plain user message.
	if req.Messages[1].Role != "user" {
		t.Errorf("expected Request message role to be user, got %q", req.Messages[1].Role)
	}
	if req.Messages[1].Content == nil || *req.Messages[1].Content != "What about <script>alert(1)</script>?" {
		t.Errorf("Request message content mismatch: got %v", req.Messages[1].Content)
	}
}

func TestOpenAICompatibleAdapter_SystemPromptNotDuplicated(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		capturedBody = string(bodyBytes)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "", "xml")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter failed: %v", err)
	}

	payload := ContextPayload{
		Model:        "gemma",
		SystemPrompt: "You are a helpful assistant.",
		Request:      "hi",
	}

	ch, err := adapter.Infer(context.Background(), payload)
	if err != nil {
		t.Fatalf("Infer failed: %v", err)
	}
	for range ch {
	}

	// Count occurrences of the system prompt text
	count := strings.Count(capturedBody, "You are a helpful assistant.")
	if count != 1 {
		t.Errorf("system prompt appears %d times in request, expected exactly 1", count)
	}
}

func TestOpenAICompatibleAdapter_XMLToolModeHistory(t *testing.T) {
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

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "", "xml")
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
		Request: "hi",
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

	// Expect: system, assistant (with XML tool calls), user (tool response), Request user.
	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(req.Messages))
	}

	assistantMsg := req.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("expected assistant role, got %q", assistantMsg.Role)
	}
	if assistantMsg.Content == nil || !strings.Contains(*assistantMsg.Content, "<tool_call>") {
		t.Errorf("expected assistant content to contain XML tool_call, got %v", assistantMsg.Content)
	}

	toolMsg := req.Messages[2]
	if toolMsg.Role != "user" {
		t.Errorf("expected role 'user' for XML tool result, got %q", toolMsg.Role)
	}
	if toolMsg.Content == nil || !strings.Contains(*toolMsg.Content, "<tool_response") {
		t.Errorf("expected XML tool_response in content, got %v", toolMsg.Content)
	}
}

func TestOpenAICompatibleAdapter_Validation(t *testing.T) {
	tests := []struct {
		name    string
		payload ContextPayload
		wantErr string
	}{
		{
			name:    "missing model",
			payload: ContextPayload{Request: "hi"},
			wantErr: "model is required",
		},
		{
			name:    "empty payload",
			payload: ContextPayload{Model: "test"},
			wantErr: "at least one content field is required",
		},
		{
			name:    "valid with Request only",
			payload: ContextPayload{Model: "test", Request: "hi"},
			wantErr: "",
		},
		{
			name:    "valid with system prompt only",
			payload: ContextPayload{Model: "test", SystemPrompt: "You are helpful."},
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

package inference

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatibleAdapter_XMLEscaping(t *testing.T) {
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

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter failed: %v", err)
	}

	payload := ContextPayload{
		Model:        "gemma",
		SystemPrompt: "You are an assistant. Use <thinking> tags.",
		Memory:       "User said: 5 < 10 && 10 > 5",
		RFC:          "What about <script>alert(1)</script>?",
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
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(req.Messages))
	}

	// Verify system prompt is sent as the dedicated system message.
	if req.Messages[0].Role != "system" {
		t.Errorf("expected first message role to be system, got %q", req.Messages[0].Role)
	}
	if req.Messages[0].Content != "You are an assistant. Use <thinking> tags." {
		t.Errorf("system message content mismatch: got %q", req.Messages[0].Content)
	}

	userContent := req.Messages[1].Content

	// Verify XML metacharacters are escaped in the user message content.
	if !strings.Contains(userContent, "<memory>") {
		t.Errorf("memory tag missing in user message")
	}
	if !strings.Contains(userContent, "5 &lt; 10 &amp;&amp; 10 &gt; 5") {
		t.Errorf("memory content not properly escaped in user message")
	}
	if !strings.Contains(userContent, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("RFC content not properly escaped in user message")
	}

	// Verify the user message content is valid XML by wrapping it in a root
	// element and parsing it.
	wrapped := "<root>" + userContent + "</root>"
	type System struct {
		Memory     string `xml:"memory"`
		DailyNotes string `xml:"daily_notes"`
		RAGResults string `xml:"rag_results"`
		Tools      string `xml:"tools"`
	}
	type Root struct {
		System            System `xml:"system"`
		CrossRoomActivity string `xml:"cross_room_activity"`
		History           string `xml:"history"`
		RequestForComment string `xml:"request_for_comment"`
	}

	var root Root
	if err := xml.Unmarshal([]byte(wrapped), &root); err != nil {
		t.Errorf("user message content is not valid XML: %v", err)
	}

	// Verify that the parsed content matches the original (unescaped) values.
	if root.System.Memory != "User said: 5 < 10 && 10 > 5" {
		t.Errorf("parsed memory mismatch: got %q", root.System.Memory)
	}
	if strings.TrimSpace(root.RequestForComment) != "What about <script>alert(1)</script>?" {
		t.Errorf("parsed RFC mismatch: got %q", root.RequestForComment)
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

	adapter, err := NewOpenAICompatibleAdapter(server.URL, "")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter failed: %v", err)
	}

	payload := ContextPayload{
		Model:        "gemma",
		SystemPrompt: "You are a helpful assistant.",
		RFC:          "hi",
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

func TestOpenAICompatibleAdapter_Validation(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't actually call Infer without a server, but we can test validation
			// by adding a validate method or testing indirectly.
			// For now, test that the adapter's Infer returns an error for invalid payloads.
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

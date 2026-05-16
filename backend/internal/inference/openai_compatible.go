package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
)

// OpenAICompatibleAdapter implements Provider for OpenAI-compatible APIs (including Ollama).
type OpenAICompatibleAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAICompatibleAdapter creates a new OpenAI-compatible provider adapter.
// apiKey may be empty for local instances.
func NewOpenAICompatibleAdapter(baseURL, apiKey string) (*OpenAICompatibleAdapter, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is required")
	}
	return &OpenAICompatibleAdapter{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{},
	}, nil
}

// Infer implements Provider for OpenAI-compatible APIs.
// It serialises the full context as an XML user message, with the
// system prompt sent as the system role message.
func (o *OpenAICompatibleAdapter) Infer(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
	if err := ValidateContextPayload(payload); err != nil {
		return nil, err
	}

	// Build XML user message containing all structured context sections.
	var xml strings.Builder
	xml.WriteString("<system>\n")
	if payload.Memory != "" {
		xml.WriteString("<memory>")
		xml.WriteString(html.EscapeString(payload.Memory))
		xml.WriteString("</memory>\n")
	}
	if len(payload.DailyNotes) > 0 {
		xml.WriteString("<daily_notes>")
		xml.WriteString(html.EscapeString(strings.Join(payload.DailyNotes, "\n\n")))
		xml.WriteString("</daily_notes>\n")
	}
	if len(payload.RAGResults) > 0 {
		xml.WriteString("<rag_results>")
		xml.WriteString(html.EscapeString(strings.Join(payload.RAGResults, "\n\n")))
		xml.WriteString("</rag_results>\n")
	}
	if len(payload.ToolSchemas) > 0 {
		xml.WriteString("<tools>")
		xml.WriteString(html.EscapeString(strings.Join(payload.ToolSchemas, "\n\n")))
		xml.WriteString("</tools>\n")
	}
	xml.WriteString("</system>\n")

	if len(payload.CrossRoomFeed) > 0 {
		xml.WriteString("<cross_room_activity>\n")
		for _, line := range payload.CrossRoomFeed {
			xml.WriteString(html.EscapeString(line))
			xml.WriteString("\n")
		}
		xml.WriteString("</cross_room_activity>\n")
	}

	if len(payload.History) > 0 {
		xml.WriteString("<history>\n")
		for _, h := range payload.History {
			xml.WriteString(html.EscapeString(h.Content))
			xml.WriteString("\n")
		}
		xml.WriteString("</history>\n")
	}

	if payload.RFC != "" {
		xml.WriteString("<request_for_comment>\n")
		xml.WriteString(html.EscapeString(payload.RFC))
		xml.WriteString("\n</request_for_comment>")
	}

	body := openaiCompatRequest{
		Model:  payload.Model,
		Stream: true,
		Messages: []openaiCompatMessage{
			{Role: "system", Content: payload.SystemPrompt},
			{Role: "user", Content: xml.String()},
		},
	}
	if payload.Temperature != nil {
		body.Temperature = payload.Temperature
	}
	if payload.MaxTokens != nil {
		body.MaxTokens = payload.MaxTokens
	}
	if len(payload.Stop) > 0 {
		body.Stop = payload.Stop
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, newHTTPError(resp.StatusCode, string(bodyBytes))
	}

	ch := make(chan StreamingChunk)
	go o.readStream(resp.Body, ch)
	return ch, nil
}

func (o *OpenAICompatibleAdapter) readStream(body io.ReadCloser, ch chan<- StreamingChunk) {
	defer body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(body)
	var usage Usage
	var sentFinal bool

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openaiCompatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Malformed chunk — log and continue
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Track usage from the final message if present
		if chunk.Usage != nil {
			usage.PromptTokens = chunk.Usage.PromptTokens
			usage.CompletionTokens = chunk.Usage.CompletionTokens
			usage.TotalTokens = chunk.Usage.TotalTokens
		}

		out := StreamingChunk{
			Content: choice.Delta.Content,
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			out.FinishReason = *choice.FinishReason
			out.Usage = usage
			sentFinal = true
		}

		if out.Content != "" || out.FinishReason != "" {
			ch <- out
		}
	}

	// If we have usage but never sent a final chunk (e.g. stream ended without finish_reason),
	// send one now.
	if !sentFinal && usage.TotalTokens > 0 {
		ch <- StreamingChunk{Usage: usage}
	}
}

// --- request/response types ---

type openaiCompatRequest struct {
	Model       string                `json:"model"`
	Messages    []openaiCompatMessage `json:"messages"`
	Stream      bool                  `json:"stream"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
	Stop        []string              `json:"stop,omitempty"`
}

type openaiCompatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type openaiCompatStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []openaiCompatChoice `json:"choices"`
	Usage   *openaiCompatUsage   `json:"usage,omitempty"`
}

type openaiCompatChoice struct {
	Index        int                `json:"index"`
	Delta        openaiCompatDelta  `json:"delta"`
	FinishReason *string            `json:"finish_reason"`
}

type openaiCompatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openaiCompatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

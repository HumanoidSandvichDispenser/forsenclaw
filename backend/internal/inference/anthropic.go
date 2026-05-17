package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnthropicAdapter implements Provider for Anthropic's Messages API.
type AnthropicAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewAnthropicAdapter creates a new Anthropic provider adapter.
func NewAnthropicAdapter(baseURL, apiKey string) (*AnthropicAdapter, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("apiKey is required for Anthropic")
	}
	return &AnthropicAdapter{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{},
	}, nil
}

// Infer implements Provider for Anthropic.
func (a *AnthropicAdapter) Infer(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
	if err := ValidateContextPayload(payload); err != nil {
		return nil, err
	}

	// Build full system content from structured payload fields.
	var systemContent strings.Builder
	systemContent.WriteString(payload.SystemPrompt)
	if payload.Memory != "" {
		systemContent.WriteString("\n\n## Memory\n\n")
		systemContent.WriteString(payload.Memory)
	}
	if len(payload.DailyNotes) > 0 {
		systemContent.WriteString("\n\n## Daily Notes\n\n")
		for _, note := range payload.DailyNotes {
			systemContent.WriteString(note)
			systemContent.WriteString("\n\n")
		}
	}
	if len(payload.RAGResults) > 0 {
		systemContent.WriteString("\n\n## Relevant Context\n\n")
		for _, r := range payload.RAGResults {
			systemContent.WriteString(r)
			systemContent.WriteString("\n\n")
		}
	}
	if len(payload.ToolSchemas) > 0 {
		systemContent.WriteString("\n\n## Available Tools\n\n")
		for _, tool := range payload.ToolSchemas {
			systemContent.WriteString(tool)
			systemContent.WriteString("\n\n")
		}
	}

	body := anthropicRequestBody{
		Model:     payload.Model,
		System:    systemContent.String(),
		Messages:  make([]anthropicMessage, 0),
		Stream:    true,
		MaxTokens: 4096,
	}

	if payload.MaxTokens != nil {
		body.MaxTokens = *payload.MaxTokens
	}
	if payload.Temperature != nil {
		body.Temperature = payload.Temperature
	}
	if len(payload.Stop) > 0 {
		body.StopSequences = payload.Stop
	}

	// Cross-room feed as first user message.
	if len(payload.CrossRoomFeed) > 0 {
		var sb strings.Builder
		sb.WriteString("## Cross-room activity\n\n")
		for _, line := range payload.CrossRoomFeed {
			sb.WriteString(line)
			sb.WriteString("\n\n")
		}
		anthropicAppendMerge(&body.Messages, "user", sb.String())
	}

	// History: add pre-role-assigned messages, merging consecutive same-role
	// pairs to satisfy Anthropic's strict alternating constraint.
	// Tool-role messages are rendered as user-role since this adapter uses the
	// text-based protocol (not Anthropic's native tool_result blocks).
	for _, h := range payload.History {
		role := string(h.Role)
		if h.Role == RoleTool {
			role = "user"
		}
		anthropicAppendMerge(&body.Messages, role, h.Content)
	}

	// RFC as final user message.
	if payload.RFC != "" {
		anthropicAppendMerge(&body.Messages, "user", payload.RFC)
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, newHTTPError(resp.StatusCode, string(bodyBytes))
	}

	ch := make(chan StreamingChunk)
	go a.readStream(resp.Body, ch)
	return ch, nil
}

// anthropicAppendMerge appends a message to msgs. If the last message has the
// same role, the content is merged with "\n\n" to satisfy Anthropic's strict
// alternating user/assistant constraint.
func anthropicAppendMerge(msgs *[]anthropicMessage, role, content string) {
	if len(*msgs) > 0 && (*msgs)[len(*msgs)-1].Role == role {
		(*msgs)[len(*msgs)-1].Content += "\n\n" + content
	} else {
		*msgs = append(*msgs, anthropicMessage{Role: role, Content: content})
	}
}

func (a *AnthropicAdapter) readStream(body io.ReadCloser, ch chan<- StreamingChunk) {
	defer body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(body)
	var content string
	var usage Usage
	var finishReason string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// SSE format: "event: <type>" followed by "data: <json>"
		if !strings.HasPrefix(line, "event: ") && !strings.HasPrefix(line, "data: ") {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			// Next line should be data. We handle both in the loop.
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var event anthropicEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta != nil && event.Delta.Type == "text_delta" {
				content += event.Delta.Text
				ch <- StreamingChunk{Content: event.Delta.Text}
			}

		case "message_delta":
			if event.Delta != nil {
				if event.Delta.StopReason != "" {
					finishReason = event.Delta.StopReason
				}
			}
			if event.Usage != nil {
				usage.PromptTokens = event.Usage.InputTokens
				usage.CompletionTokens = event.Usage.OutputTokens
				usage.TotalTokens = event.Usage.InputTokens + event.Usage.OutputTokens
			}

		case "message_stop":
			// Stream complete — send final chunk with finish reason and usage
			ch <- StreamingChunk{
				Content:      "",
				FinishReason: finishReason,
				Usage:        usage,
			}
			return
		}
	}

	// If scanner ended without message_stop, still try to emit what we have
	if content != "" || finishReason != "" {
		ch <- StreamingChunk{
			Content:      "",
			FinishReason: finishReason,
			Usage:        usage,
		}
	}
}

// --- request/response types ---

type anthropicRequestBody struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicEvent struct {
	Type  string          `json:"type"`
	Delta *anthropicDelta `json:"delta,omitempty"`
	Usage *anthropicUsage `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

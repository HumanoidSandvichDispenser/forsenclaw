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
func (a *AnthropicAdapter) Infer(ctx context.Context, req InferRequest) (<-chan StreamingChunk, error) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}

	body := anthropicRequestBody{
		Model:     req.Model,
		Messages:  make([]anthropicMessage, 0, len(req.Messages)),
		Stream:    true,
		MaxTokens: 4096, // default; overridden below if set
	}

	// Anthropic separates system prompt from messages
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			body.System = m.Content
			continue
		}
		body.Messages = append(body.Messages, anthropicMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	if req.MaxTokens != nil {
		body.MaxTokens = *req.MaxTokens
	}
	if req.Temperature != nil {
		body.Temperature = req.Temperature
	}
	if len(req.Stop) > 0 {
		body.StopSequences = req.Stop
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

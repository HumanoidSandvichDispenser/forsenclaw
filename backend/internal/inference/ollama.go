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

// OllamaAdapter implements Provider for Ollama's OpenAI-compatible API.
type OllamaAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOllamaAdapter creates a new Ollama provider adapter.
// apiKey may be empty for local Ollama instances.
func NewOllamaAdapter(baseURL, apiKey string) (*OllamaAdapter, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is required")
	}
	return &OllamaAdapter{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{},
	}, nil
}

// Infer implements Provider for Ollama.
func (o *OllamaAdapter) Infer(ctx context.Context, req InferRequest) (<-chan StreamingChunk, error) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}

	body := ollamaRequestBody{
		Model:    req.Model,
		Messages: make([]ollamaMessage, len(req.Messages)),
		Stream:   true,
	}
	for i, m := range req.Messages {
		body.Messages[i] = ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
		if m.Name != "" {
			body.Messages[i].Name = m.Name
		}
	}
	if req.Temperature != nil {
		body.Temperature = req.Temperature
	}
	if req.MaxTokens != nil {
		body.MaxTokens = req.MaxTokens
	}
	if len(req.Stop) > 0 {
		body.Stop = req.Stop
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

func (o *OllamaAdapter) readStream(body io.ReadCloser, ch chan<- StreamingChunk) {
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

		var chunk ollamaStreamChunk
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

type ollamaRequestBody struct {
	Model       string          `json:"model"`
	Messages    []ollamaMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type ollamaStreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []ollamaChoice `json:"choices"`
	Usage   *ollamaUsage   `json:"usage,omitempty"`
}

type ollamaChoice struct {
	Index        int         `json:"index"`
	Delta        ollamaDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type ollamaDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type ollamaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

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

// OpenAICompatibleAdapter implements Provider for OpenAI-compatible APIs (including Ollama).
type OpenAICompatibleAdapter struct {
	baseURL  string
	apiKey   string
	toolMode string // "native" or "xml"
	client   *http.Client
}

// NewOpenAICompatibleAdapter creates a new OpenAI-compatible provider adapter.
// apiKey may be empty for local instances.
// toolMode is "native" (default) or "xml".
func NewOpenAICompatibleAdapter(baseURL, apiKey string, toolMode string) (*OpenAICompatibleAdapter, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is required")
	}
	if toolMode == "" {
		toolMode = "native"
	}
	return &OpenAICompatibleAdapter{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		toolMode: toolMode,
		client:   &http.Client{},
	}, nil
}

// Infer implements Provider for OpenAI-compatible APIs.
func (o *OpenAICompatibleAdapter) Infer(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
	if err := ValidateContextPayload(payload); err != nil {
		return nil, err
	}

	body := openaiCompatRequest{
		Model:    payload.Model,
		Stream:   true,
		Messages: make([]openaiCompatMessage, 0),
	}

	// System prompt.
	systemPrompt := BuildSystemPrompt(payload, o.toolMode)
	body.Messages = append(body.Messages, openaiCompatMessage{
		Role:    "system",
		Content: &systemPrompt,
	})

	// Native tools: add tools array from ToolDefinitions.
	if o.toolMode == "native" && len(payload.ToolDefinitions) > 0 {
		body.Tools = make([]openaiTool, 0, len(payload.ToolDefinitions))
		for _, def := range payload.ToolDefinitions {
			body.Tools = append(body.Tools, openaiTool{
				Type: "function",
				Function: openaiToolFunctionDef{
					Name:        def.Name,
					Description: def.Description,
					Parameters:  def.Parameters,
				},
			})
		}
	}

	// Build message sequence.
	msgSeq := BuildMessageSequence(payload)
	for _, msg := range msgSeq {
		body.Messages = append(body.Messages, o.serializeContextMessage(msg))
	}

	if body.Stream {
		body.StreamOptions = &openaiStreamOptions{IncludeUsage: true}
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

func (o *OpenAICompatibleAdapter) serializeContextMessage(msg ContextMessage) openaiCompatMessage {
	if o.toolMode == "xml" {
		// XML mode: render tool calls as XML text, tool results as user messages.
		if msg.Role == string(RoleAssistant) && len(msg.ToolCalls) > 0 {
			return openaiCompatMessage{
				Role:    "assistant",
				Content: stringPtr(RenderToolCallsXML(msg.ToolCalls)),
			}
		}
		if msg.Role == string(RoleTool) {
			return openaiCompatMessage{
				Role:    "user",
				Content: stringPtr(fmt.Sprintf("<tool_response tool_id=\"%s\">%s</tool_response>", msg.Name, msg.Content)),
			}
		}
		return openaiCompatMessage{
			Role:    msg.Role,
			Content: &msg.Content,
		}
	}

	// Native mode.
	if msg.Role == string(RoleAssistant) && len(msg.ToolCalls) > 0 {
		calls := make([]openaiToolCall, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			calls = append(calls, openaiToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: openaiToolFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		return openaiCompatMessage{
			Role:      "assistant",
			ToolCalls: calls,
		}
	}
	if msg.Role == string(RoleTool) {
		return openaiCompatMessage{
			Role:       "tool",
			ToolCallID: msg.ToolCallID,
			Content:    &msg.Content,
		}
	}
	return openaiCompatMessage{
		Role:    msg.Role,
		Content: &msg.Content,
	}
}

func stringPtr(s string) *string { return &s }

func (o *OpenAICompatibleAdapter) readStream(body io.ReadCloser, ch chan<- StreamingChunk) {
	defer body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(body)
	var usage Usage
	var sentFinal bool

	// Native mode: accumulate streaming tool calls per index.
	accumulator := make(map[int]*openaiToolCall)

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
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		if chunk.Usage != nil {
			usage.PromptTokens = chunk.Usage.PromptTokens
			usage.CompletionTokens = chunk.Usage.CompletionTokens
			usage.TotalTokens = chunk.Usage.TotalTokens
		}

		out := StreamingChunk{
			Content: choice.Delta.Content,
		}

		// Accumulate native tool call deltas.
		if len(choice.Delta.ToolCalls) > 0 {
			for _, delta := range choice.Delta.ToolCalls {
				call, ok := accumulator[delta.Index]
				if !ok {
					call = &openaiToolCall{Index: delta.Index, Type: "function"}
					accumulator[delta.Index] = call
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				if delta.Function.Name != "" {
					call.Function.Name += delta.Function.Name
				}
				if delta.Function.Arguments != "" {
					call.Function.Arguments += delta.Function.Arguments
				}
			}
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			out.FinishReason = *choice.FinishReason
			out.Usage = usage

			// Populate ToolCalls from the accumulator whenever there are
			// accumulated entries, regardless of finish_reason. Some providers
			// (e.g. Gemini via OpenAI-compat) return finish_reason="stop"
			// instead of "tool_calls" even when making tool calls.
			if len(accumulator) > 0 {
				indices := make([]int, 0, len(accumulator))
				for idx := range accumulator {
					indices = append(indices, idx)
				}
				// Tool call indices may arrive out of order; sort them.
				for i := 0; i < len(indices); i++ {
					for j := i + 1; j < len(indices); j++ {
						if indices[j] < indices[i] {
							indices[i], indices[j] = indices[j], indices[i]
						}
					}
				}
				wires := make([]ToolCallWire, 0, len(indices))
				for _, idx := range indices {
					acc := accumulator[idx]
					wires = append(wires, ToolCallWire{
						ID:   acc.ID,
						Type: acc.Type,
						Function: ToolFunctionWire{
							Name:      acc.Function.Name,
							Arguments: acc.Function.Arguments,
						},
					})
				}
				out.ToolCalls = wires
			}

			sentFinal = true
		}

		if out.Content != "" || out.FinishReason != "" {
			ch <- out
		}
	}

	if !sentFinal && usage.TotalTokens > 0 {
		ch <- StreamingChunk{Usage: usage}
	}
}

// --- request/response types ---

type openaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type openaiCompatRequest struct {
	Model         string                `json:"model"`
	Messages      []openaiCompatMessage `json:"messages"`
	Stream        bool                  `json:"stream"`
	Temperature   *float64              `json:"temperature,omitempty"`
	MaxTokens     *int                  `json:"max_tokens,omitempty"`
	Stop          []string              `json:"stop,omitempty"`
	StreamOptions *openaiStreamOptions  `json:"stream_options,omitempty"`
	Tools         []openaiTool          `json:"tools,omitempty"`
}

type openaiCompatMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiTool struct {
	Type     string               `json:"type"`
	Function openaiToolFunctionDef `json:"function"`
}

type openaiToolFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type openaiToolCall struct {
	Index    int                `json:"index,omitempty"`
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	Delta        openaiCompatDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openaiCompatDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
}

type openaiCompatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

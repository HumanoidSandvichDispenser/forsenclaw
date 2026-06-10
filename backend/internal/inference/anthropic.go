package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// AnthropicAdapter implements Provider for Anthropic's Messages API.
// It always uses Anthropic's native tool_use / tool_result protocol.
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

	body := anthropicRequestBody{
		Model:     payload.Model,
		System:    BuildSystemPrompt(payload, "native"),
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

	// Add native tool definitions if available.
	if len(payload.ToolDefinitions) > 0 {
		body.Tools = make([]anthropicTool, 0, len(payload.ToolDefinitions))
		for _, def := range payload.ToolDefinitions {
			body.Tools = append(body.Tools, anthropicTool{
				Name:        def.Name,
				Description: def.Description,
				InputSchema: def.Parameters,
			})
		}
	}

	// Build message sequence from payload history and Request.
	msgSeq := BuildMessageSequence(payload)
	for _, msg := range msgSeq {
		anthMsg := a.serializeContextMessage(msg)
		anthropicAppendMerge(&body.Messages, anthMsg)
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

// serializeContextMessage converts a ContextMessage to Anthropic's wire format.
func (a *AnthropicAdapter) serializeContextMessage(msg ContextMessage) anthropicMessage {
	switch msg.Role {
	case string(RoleAssistant):
		if len(msg.ToolCalls) > 0 {
			blocks := make([]anthropicContentBlock, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				var input map[string]interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			return anthropicMessage{Role: "assistant", Content: blocks}
		}
		return anthropicMessage{Role: "assistant", Content: msg.Content}

	case string(RoleTool):
		blocks := []anthropicContentBlock{
			{
				Type:        "tool_result",
				ToolUseID:   msg.ToolCallID,
				ToolContent: msg.Content,
			},
		}
		return anthropicMessage{Role: "user", Content: blocks}

	default:
		return anthropicMessage{Role: msg.Role, Content: msg.Content}
	}
}

// anthropicAppendMerge appends a message to msgs. If the last message has the
// same role, and both are plain strings, the content is merged with "\n\n" to
// satisfy Anthropic's strict alternating user/assistant constraint.
func anthropicAppendMerge(msgs *[]anthropicMessage, msg anthropicMessage) {
	if len(*msgs) > 0 && (*msgs)[len(*msgs)-1].Role == msg.Role {
		last := &(*msgs)[len(*msgs)-1]
		lastStr, lastIsStr := last.Content.(string)
		msgStr, msgIsStr := msg.Content.(string)
		if lastIsStr && msgIsStr {
			last.Content = lastStr + "\n\n" + msgStr
			return
		}
	}
	*msgs = append(*msgs, msg)
}

func (a *AnthropicAdapter) readStream(body io.ReadCloser, ch chan<- StreamingChunk) {
	defer body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(body)
	var content strings.Builder
	var usage Usage
	var finishReason string

	// Tool-use streaming accumulation.
	var toolUseAccumulating bool
	var toolCallID string
	var toolName string
	var toolArgs strings.Builder
	var toolCalls []ToolCallWire

	debug := os.Getenv("HEARTH_DEBUG_INFERENCE") == "1"

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if debug {
			log.Printf("inference stream <= %s", line)
		}

		// SSE format: "event: <type>" followed by "data: <json>"
		if !strings.HasPrefix(line, "event: ") && !strings.HasPrefix(line, "data: ") {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
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
		case "message_start":
			// Cache-read tokens and the authoritative input count arrive here;
			// message_delta only carries the running output count afterward.
			if event.Message != nil && event.Message.Usage != nil {
				usage.PromptTokens = event.Message.Usage.InputTokens
				usage.CachedTokens = event.Message.Usage.CacheReadInputTokens
			}

		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				toolUseAccumulating = true
				toolCallID = event.ContentBlock.ID
				toolName = event.ContentBlock.Name
				toolArgs.Reset()
				if event.ContentBlock.Input != nil {
					raw, _ := json.Marshal(event.ContentBlock.Input)
					toolArgs.Write(raw)
				}
			}

		case "content_block_delta":
			if event.Delta != nil {
				if event.Delta.Type == "text_delta" {
					content.WriteString(event.Delta.Text)
					ch <- StreamingChunk{Content: event.Delta.Text}
				} else if event.Delta.Type == "input_json_delta" {
					toolArgs.WriteString(event.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			if toolUseAccumulating {
				toolCalls = append(toolCalls, ToolCallWire{
					ID:   toolCallID,
					Type: "function",
					Function: ToolFunctionWire{
						Name:      toolName,
						Arguments: toolArgs.String(),
					},
				})
				toolUseAccumulating = false
			}

		case "message_delta":
			if event.Delta != nil {
				if event.Delta.StopReason != "" {
					finishReason = event.Delta.StopReason
				}
			}
			if event.Usage != nil {
				// message_delta repeats only the running output count; the input
				// and cache-read counts were set at message_start, so don't let a
				// zero here overwrite them.
				if event.Usage.InputTokens > 0 {
					usage.PromptTokens = event.Usage.InputTokens
				}
				usage.CompletionTokens = event.Usage.OutputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}

		case "message_stop":
			// Stream complete — send final chunk with finish reason, usage, and any tool calls.
			ch <- StreamingChunk{
				Content:      "",
				FinishReason: finishReason,
				Usage:        usage,
				ToolCalls:    toolCalls,
			}
			return
		}
	}

	if err := scanner.Err(); err != nil && debug {
		log.Printf("inference stream scanner error: %v", err)
	}

	// If scanner ended without message_stop, still try to emit what we have.
	if content.Len() > 0 || finishReason != "" {
		ch <- StreamingChunk{
			Content:      "",
			FinishReason: finishReason,
			Usage:        usage,
			ToolCalls:    toolCalls,
		}
	}
}

// --- request/response types ---

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type anthropicRequestBody struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type        string                 `json:"type"`
	Text        string                 `json:"text,omitempty"`
	ID          string                 `json:"id,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Input       map[string]interface{} `json:"input,omitempty"`
	ToolUseID   string                 `json:"tool_use_id,omitempty"`
	ToolContent string                 `json:"content,omitempty"`
}

type anthropicEvent struct {
	Type         string                 `json:"type"`
	Index        int                    `json:"index,omitempty"`
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	Delta        *anthropicDelta        `json:"delta,omitempty"`
	Usage        *anthropicUsage        `json:"usage,omitempty"`
	Message      *anthropicStreamStart  `json:"message,omitempty"`
}

// anthropicStreamStart is the message envelope on a message_start event. Its
// usage carries the initial input-token count and the cache-read hit, which the
// per-token deltas (message_delta) never repeat.
type anthropicStreamStart struct {
	Usage *anthropicUsage `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type anthropicUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

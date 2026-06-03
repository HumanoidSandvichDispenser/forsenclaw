package inference

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// Role represents the role of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single message in a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// Name is used for tool messages to identify the tool.
	Name string `json:"name,omitempty"`
}

// ModelTier represents the three model assignment tiers for an agent.
type ModelTier string

const (
	TierPrimary   ModelTier = "primary"
	TierRoutine   ModelTier = "routine"
	TierSensitive ModelTier = "sensitive"
)

// InferRequest is kept for internal use (e.g. validation helpers).
type InferRequest struct {
	Model       string
	Messages    []Message
	Temperature *float64
	MaxTokens   *int
	Stop        []string
}

// Usage tracks token consumption for a single inference call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamingChunk represents a single chunk from a streaming inference response.
// The caller ranges over the channel returned by Provider.Infer until it closes.
// Usage metadata is only set on the final chunk (when FinishReason is non-empty).
// If the stream encounters an error, the channel is closed without a final chunk.
// Callers should check if the last chunk had a non-empty FinishReason to determine
// whether the stream completed normally.
type StreamingChunk struct {
	Content      string         `json:"content"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Usage        Usage          `json:"usage,omitempty"`
	ToolCalls    []ToolCallWire `json:"tool_calls,omitempty"` // populated by adapters using native tool calling
}

// HistoryMessage is a pre-role-assigned message in the conversation history.
type HistoryMessage struct {
	Role       Role
	Content    string
	Name       string
	ToolCallID string
	ToolCalls  []ToolCallWire
}

// ToolCallWire is the structured tool-call wire format used by OpenAI-compatible APIs.
type ToolCallWire struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolFunctionWire `json:"function"`
}

// ToolFunctionWire is the function payload inside a tool call.
type ToolFunctionWire struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// RenderToolCallsXML serialises tool calls into the XML form used by text-based providers.
func RenderToolCallsXML(toolCalls []ToolCallWire) string {
	if len(toolCalls) == 0 {
		return ""
	}

	var b strings.Builder
	for _, tc := range toolCalls {
		b.WriteString("<tool_call><tool_id>")
		b.WriteString(tc.Function.Name)
		b.WriteString("</tool_id><parameters>")

		var params map[string]string
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err == nil {
			keys := make([]string, 0, len(params))
			for k := range params {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				b.WriteString("<")
				b.WriteString(k)
				b.WriteString(">")
				_ = xml.EscapeText(&b, []byte(params[k]))
				b.WriteString("</")
				b.WriteString(k)
				b.WriteString(">")
			}
		}

		b.WriteString("</parameters></tool_call>")
	}
	return b.String()
}

// ToolDefinition is a structured tool definition for native tool calling adapters.
type ToolDefinition struct {
	Name        string
	Description string
	Resource    string // FRSN (frsn:tool/{server}/{tool}) for policy evaluation
	// DataActions are the data-level actions this tool performs (e.g.
	// "data:read", "data:write"), evaluated alongside "tool:invoke" for
	// two-tier authorization. Empty means no data-tier check applies.
	DataActions []string
	Parameters  map[string]interface{} // JSON Schema object
	// Clearance is the data classification tier for this tool.
	// Populated by the registry at construction from config.
	Clearance int
}

// ContextMessage is a message in the conversation sequence built from a ContextPayload.
type ContextMessage struct {
	Role       string // "user", "assistant", "tool", "system"
	Content    string
	ToolCalls  []ToolCallWire
	ToolCallID string
	Name       string
}

// ContextPayload is the structured context for a model invocation.
// Providers receive this and render it natively for their API.
type ContextPayload struct {
	Model              string
	SystemPrompt       string
	Memory             string
	DailyNotes         []string
	RAGResults         []string
	ToolSchemas        []string         // for XML tool-mode fallback
	ToolDefinitions    []ToolDefinition // for native tool calling adapters
	CrossRoomFeed      []string         // pre-formatted strings, one per message
	History            []HistoryMessage // room conversation history, static for the request
	CurrentTurnHistory []HistoryMessage // tool exchanges accumulated this inference turn
	Request            string
	RequestName        string // speaker of the triggering message, for attribution
	Temperature        *float64
	MaxTokens          *int
	Stop               []string
}

// Provider is the interface for all model provider adapters.
type Provider interface {
	// Infer streams response chunks through the returned channel.
	// The channel is closed when the stream completes or encounters an error.
	// Setup errors (authentication, invalid request) are returned as the error value.
	Infer(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error)
}

// InferSync is a convenience wrapper that collects all chunks from a stream
// into a single response. It returns an error if the stream fails or if the
// context is cancelled.
func InferSync(ctx context.Context, p Provider, payload ContextPayload) (string, Usage, error) {
	ch, err := p.Infer(ctx, payload)
	if err != nil {
		return "", Usage{}, err
	}

	var contentBuilder strings.Builder
	var usage Usage

	for chunk := range ch {
		select {
		case <-ctx.Done():
			return "", Usage{}, ctx.Err()
		default:
		}

		contentBuilder.WriteString(chunk.Content)
		if chunk.FinishReason != "" {
			usage = chunk.Usage
		}
	}

	return contentBuilder.String(), usage, nil
}

// ValidateRequest checks that an InferRequest is well-formed before sending
// it to a provider.
func ValidateRequest(req InferRequest) error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("at least one message is required")
	}
	for i, m := range req.Messages {
		if m.Role == "" {
			return fmt.Errorf("message %d: role is required", i)
		}
		if m.Content == "" && m.Role != RoleTool {
			return fmt.Errorf("message %d: content is required", i)
		}
	}
	return nil
}

// ValidateContextPayload checks that a ContextPayload is well-formed before
// sending it to a provider.
func ValidateContextPayload(payload ContextPayload) error {
	if payload.Model == "" {
		return fmt.Errorf("model is required")
	}
	hasContent := payload.SystemPrompt != "" ||
		payload.Memory != "" ||
		len(payload.DailyNotes) > 0 ||
		len(payload.RAGResults) > 0 ||
		len(payload.ToolSchemas) > 0 ||
		len(payload.CrossRoomFeed) > 0 ||
		len(payload.History) > 0 ||
		payload.Request != ""
	if !hasContent {
		return fmt.Errorf("at least one content field is required")
	}
	return nil
}

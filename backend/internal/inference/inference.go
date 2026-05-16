package inference

import (
	"context"
	"fmt"
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
	Content      string `json:"content"`
	FinishReason string `json:"finish_reason,omitempty"`
	Usage        Usage  `json:"usage,omitempty"`
}

// HistoryMessage is a pre-role-assigned message in the conversation history.
type HistoryMessage struct {
	Role    Role
	Content string
	Name    string
}

// ContextPayload is the structured context for a model invocation.
// Providers receive this and render it natively for their API.
type ContextPayload struct {
	Model         string
	SystemPrompt  string
	Memory        string
	DailyNotes    []string
	RAGResults    []string
	ToolSchemas   []string
	CrossRoomFeed []string // pre-formatted strings, one per message
	History       []HistoryMessage
	RFC           string
	Temperature   *float64
	MaxTokens     *int
	Stop          []string
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

	var content string
	var usage Usage

	for chunk := range ch {
		select {
		case <-ctx.Done():
			return "", Usage{}, ctx.Err()
		default:
		}

		content += chunk.Content
		if chunk.FinishReason != "" {
			usage = chunk.Usage
		}
	}

	return content, usage, nil
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
		payload.RFC != ""
	if !hasContent {
		return fmt.Errorf("at least one content field is required")
	}
	return nil
}

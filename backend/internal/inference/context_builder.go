package inference

import "strings"

// BuildSystemPrompt assembles the full system prompt string from the payload.
// It includes the system prompt, memory, daily notes, RAG results, cross-room
// feed, and tool schemas (only when toolMode is "xml").
func BuildSystemPrompt(payload ContextPayload, toolMode string) string {
	if toolMode == "" {
		toolMode = "native"
	}

	var b strings.Builder
	b.WriteString(payload.SystemPrompt)

	if payload.Memory != "" {
		b.WriteString("\n\n## Memory\n\n")
		b.WriteString(payload.Memory)
	}
	if len(payload.DailyNotes) > 0 {
		b.WriteString("\n\n## Daily Notes\n\n")
		for i, note := range payload.DailyNotes {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(note)
		}
	}
	if len(payload.RAGResults) > 0 {
		b.WriteString("\n\n## Relevant Context\n\n")
		for i, r := range payload.RAGResults {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(r)
		}
	}
	if len(payload.CrossRoomFeed) > 0 {
		b.WriteString("\n\n## Cross-room activity\n\n")
		for _, line := range payload.CrossRoomFeed {
			b.WriteString(line)
			b.WriteString("\n\n")
		}
	}
	if toolMode == "xml" && len(payload.ToolSchemas) > 0 {
		b.WriteString("\n\n## Available Tools\n\n")
		for i, tool := range payload.ToolSchemas {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(tool)
		}
	}

	return b.String()
}

// BuildMessageSequence returns the ordered message sequence (not including
// system) from the payload: prior history, then the Request (the triggering user
// message), then the current turn's in-progress tool exchanges. Native tool call
// fields are preserved.
func BuildMessageSequence(payload ContextPayload) []ContextMessage {
	msgs := make([]ContextMessage, 0, len(payload.History)+len(payload.CurrentTurnHistory)+1)

	for _, h := range payload.History {
		msgs = append(msgs, ContextMessage{
			Role:       string(h.Role),
			Content:    h.Content,
			ToolCalls:  h.ToolCalls,
			ToolCallID: h.ToolCallID,
			Name:       h.Name,
		})
	}

	// The Request is the triggering user message and must precede the current
	// turn's tool exchanges. CurrentTurnHistory holds the assistant's in-progress
	// response to the Request (tool calls and their results), so it belongs after it.
	// Appending the Request last would place a stale copy of the user's request after
	// the tool results on every continuation turn, which the model reads as a
	// fresh request and answers by re-calling the tool — an infinite loop.
	if payload.Request != "" {
		msgs = append(msgs, ContextMessage{
			Role:    string(RoleUser),
			Content: payload.Request,
		})
	}

	for _, h := range payload.CurrentTurnHistory {
		msgs = append(msgs, ContextMessage{
			Role:       string(h.Role),
			Content:    h.Content,
			ToolCalls:  h.ToolCalls,
			ToolCallID: h.ToolCallID,
			Name:       h.Name,
		})
	}

	return msgs
}

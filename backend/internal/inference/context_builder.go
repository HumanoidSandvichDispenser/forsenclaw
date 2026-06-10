package inference

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// BuildSystemPrompt assembles the full system prompt string from the payload.
// The agent's own instructions (payload.SystemPrompt) lead as a plain preamble
// so the cacheable prefix stays stable; the variable, data-classified payload
// (memory, daily notes, RAG, and — in "xml" tool mode — tool schemas) follows
// as a nested <context> tree. Each section is an explicitly closed tag, so the
// hierarchy is unambiguous and user-authored content (which may itself contain
// "##" headers or stray "<"/"&") cannot forge or break out of a level: all
// payload text is XML-escaped.
func BuildSystemPrompt(payload ContextPayload, toolMode string) string {
	if toolMode == "" {
		toolMode = "native"
	}

	var b strings.Builder
	b.WriteString(payload.SystemPrompt)

	includeTools := toolMode == "xml" && len(payload.ToolSchemas) > 0
	if len(payload.Memory) == 0 && len(payload.DailyNotes) == 0 &&
		len(payload.RAGResults) == 0 && !includeTools {
		return b.String()
	}

	b.WriteString("\n\n<context>")

	if len(payload.Memory) > 0 {
		b.WriteString("\n<memory>")
		for _, m := range payload.Memory {
			b.WriteString("\n<entry clearance=\"")
			b.WriteString(strconv.Itoa(m.Clearance))
			b.WriteString("\">")
			writeEscaped(&b, m.Content)
			b.WriteString("</entry>")
		}
		b.WriteString("\n</memory>")
	}
	if len(payload.DailyNotes) > 0 {
		b.WriteString("\n<daily_notes>")
		for _, note := range payload.DailyNotes {
			b.WriteString("\n<note date=\"")
			b.WriteString(note.Date.Format("2006-01-02"))
			b.WriteString("\" clearance=\"")
			b.WriteString(strconv.Itoa(note.Clearance))
			b.WriteString("\">")
			writeEscaped(&b, note.Content)
			b.WriteString("</note>")
		}
		b.WriteString("\n</daily_notes>")
	}
	if len(payload.RAGResults) > 0 {
		b.WriteString("\n<relevant_context>")
		for _, r := range payload.RAGResults {
			b.WriteString("\n<result>")
			writeEscaped(&b, r)
			b.WriteString("</result>")
		}
		b.WriteString("\n</relevant_context>")
	}
	if includeTools {
		b.WriteString("\n<available_tools>")
		for _, tool := range payload.ToolSchemas {
			b.WriteString("\n<tool>")
			writeEscaped(&b, tool)
			b.WriteString("</tool>")
		}
		b.WriteString("\n</available_tools>")
	}

	b.WriteString("\n</context>")
	return b.String()
}

// writeEscaped writes s into b with XML metacharacters escaped, so payload
// content cannot break out of its enclosing tag.
func writeEscaped(b *strings.Builder, s string) {
	_ = xml.EscapeText(b, []byte(s))
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
			Name:    payload.RequestName,
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

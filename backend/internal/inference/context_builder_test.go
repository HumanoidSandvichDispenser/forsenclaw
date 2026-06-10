package inference

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt(t *testing.T) {
	payload := ContextPayload{
		SystemPrompt: "You are helpful.",
		Memory:       "Key fact.",
		DailyNotes:   []string{"Note one.", "Note two."},
		RAGResults:   []string{"Result A.", "Result B."},
		ToolSchemas:  []string{"tool1 schema", "tool2 schema"},
	}

	// Native mode: no tool schemas in system prompt.
	sys := BuildSystemPrompt(payload, "native")
	if !strings.Contains(sys, "You are helpful.") {
		t.Error("missing base system prompt")
	}
	if !strings.Contains(sys, "## Memory") {
		t.Error("missing memory section")
	}
	if !strings.Contains(sys, "## Daily Notes") {
		t.Error("missing daily notes section")
	}
	if !strings.Contains(sys, "## Relevant Context") {
		t.Error("missing RAG section")
	}
	if strings.Contains(sys, "## Available Tools") {
		t.Error("native mode should not include tool schemas in system prompt")
	}

	// XML mode: tool schemas included.
	sysXML := BuildSystemPrompt(payload, "xml")
	if !strings.Contains(sysXML, "## Available Tools") {
		t.Error("xml mode should include tool schemas in system prompt")
	}
	if !strings.Contains(sysXML, "tool1 schema") {
		t.Error("missing tool schema content")
	}
}

func TestBuildSystemPrompt_EmptyFields(t *testing.T) {
	payload := ContextPayload{
		SystemPrompt: "Base.",
	}

	sys := BuildSystemPrompt(payload, "native")
	if sys != "Base." {
		t.Errorf("expected only base prompt, got %q", sys)
	}
}

func TestBuildMessageSequence(t *testing.T) {
	payload := ContextPayload{
		History: []HistoryMessage{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "Hi there"},
		},
		Request: "How are you?",
	}

	msgs := BuildMessageSequence(payload)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Hi there" {
		t.Errorf("unexpected second message: %+v", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "How are you?" {
		t.Errorf("unexpected third message: %+v", msgs[2])
	}
}

func TestBuildMessageSequence_ToolCalls(t *testing.T) {
	payload := ContextPayload{
		History: []HistoryMessage{
			{
				Role:    RoleAssistant,
				Content: "",
				ToolCalls: []ToolCallWire{
					{ID: "call_1", Type: "function", Function: ToolFunctionWire{Name: "search", Arguments: `{}`}},
				},
			},
			{
				Role:       RoleTool,
				ToolCallID: "call_1",
				Name:       "search",
				Content:    "results",
			},
		},
		Request: "Thanks.",
	}

	msgs := BuildMessageSequence(payload)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if len(msgs[0].ToolCalls) != 1 || msgs[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("expected tool call preserved in message sequence")
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "call_1" {
		t.Errorf("expected tool message preserved: %+v", msgs[1])
	}
}

// TestBuildMessageSequence_RequestBeforeCurrentTurn guards the continuation-turn
// ordering: the Request (triggering user message) must come before the current
// turn's tool exchanges, not after. If it landed after the tool result, the
// model would read it as a fresh request and re-call the tool indefinitely.
func TestBuildMessageSequence_RequestBeforeCurrentTurn(t *testing.T) {
	payload := ContextPayload{
		History: []HistoryMessage{
			{Role: RoleUser, Content: "earlier"},
		},
		Request: "search the docs",
		CurrentTurnHistory: []HistoryMessage{
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCallWire{
					{ID: "call_1", Type: "function", Function: ToolFunctionWire{Name: "search", Arguments: `{}`}},
				},
			},
			{Role: RoleTool, ToolCallID: "call_1", Name: "search", Content: "results"},
		},
	}

	msgs := BuildMessageSequence(payload)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	// history, Request (user), assistant tool_call, tool result — in that order.
	if msgs[1].Role != "user" || msgs[1].Content != "search the docs" {
		t.Fatalf("Request must precede current-turn tool exchanges, got msgs[1]=%+v", msgs[1])
	}
	if len(msgs[2].ToolCalls) != 1 {
		t.Errorf("expected assistant tool call after Request, got %+v", msgs[2])
	}
	if msgs[3].Role != "tool" {
		t.Errorf("expected tool result last, got %+v", msgs[3])
	}
}

func TestBuildMessageSequence_NoRequest(t *testing.T) {
	payload := ContextPayload{
		History: []HistoryMessage{
			{Role: RoleUser, Content: "Hello"},
		},
	}

	msgs := BuildMessageSequence(payload)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

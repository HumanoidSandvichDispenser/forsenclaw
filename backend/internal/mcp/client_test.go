package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/audit"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// --- mock types ---

type mockMCPClient struct {
	toolIDs   []string
	response  string
	err       error
	callCount int
	lastParams map[string]string
}

func (m *mockMCPClient) Call(_ context.Context, _ string, params map[string]string) (string, error) {
	m.callCount++
	m.lastParams = params
	return m.response, m.err
}

func (m *mockMCPClient) ToolIDs() []string { return m.toolIDs }
func (m *mockMCPClient) Healthy() bool     { return true }

// recordingSink captures audit events for test assertions.
type recordingSink struct {
	events []audit.Event
}

func (s *recordingSink) Write(e audit.Event) error {
	s.events = append(s.events, e)
	return nil
}

// --- helper ---

func wireCall(toolName, argsJSON string) inference.ToolCallWire {
	return inference.ToolCallWire{
		ID:   "test-call-id",
		Type: "function",
		Function: inference.ToolFunctionWire{
			Name:      toolName,
			Arguments: argsJSON,
		},
	}
}

// --- AC-1: Parser — happy path ---

func TestParseToolCalls_SingleCall(t *testing.T) {
	response := `Some text before.
<tool_call>
  <tool_id>web_search</tool_id>
  <parameters>
    <query>current weather in Reno NV</query>
  </parameters>
</tool_call>
Some text after.`

	calls, prose, err := ParseToolCalls(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ToolID != "web_search" {
		t.Errorf("expected tool_id 'web_search', got %q", calls[0].ToolID)
	}
	if calls[0].Parameters["query"] != "current weather in Reno NV" {
		t.Errorf("expected query param, got %q", calls[0].Parameters["query"])
	}
	if calls[0].ID == "" {
		t.Error("expected non-empty call ID")
	}
	if !strings.Contains(prose, "Some text before") {
		t.Errorf("prose should contain surrounding text, got %q", prose)
	}
	if !strings.Contains(prose, "Some text after") {
		t.Errorf("prose should contain surrounding text, got %q", prose)
	}
	if strings.Contains(prose, "<tool_call>") {
		t.Error("prose should not contain tool_call XML")
	}
}

// --- AC-2: Parser — multiple calls ---

func TestParseToolCalls_MultipleCalls(t *testing.T) {
	response := `<tool_call>
  <tool_id>search</tool_id>
  <parameters><q>foo</q></parameters>
</tool_call>
<tool_call>
  <tool_id>fetch</tool_id>
  <parameters><url>http://example.com</url></parameters>
</tool_call>`

	calls, prose, err := ParseToolCalls(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].ToolID != "search" {
		t.Errorf("first call: expected 'search', got %q", calls[0].ToolID)
	}
	if calls[1].ToolID != "fetch" {
		t.Errorf("second call: expected 'fetch', got %q", calls[1].ToolID)
	}
	if prose != "" {
		t.Errorf("expected empty prose, got %q", prose)
	}
}

func TestParseToolCalls_IgnoreToolCallsInsideThoughtBlocks(t *testing.T) {
	response := `<think>
  considering options...
  <tool_call>
    <tool_id>web_search</tool_id>
    <parameters><query>hidden</query></parameters>
  </tool_call>
</think>
<tool_call>
  <tool_id>web_search</tool_id>
  <parameters><query>visible</query></parameters>
</tool_call>`

	calls, prose, err := ParseToolCalls(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 parsed call, got %d", len(calls))
	}
	if calls[0].Parameters["query"] != "visible" {
		t.Fatalf("expected visible query, got %q", calls[0].Parameters["query"])
	}
	if !strings.Contains(prose, "hidden") {
		t.Fatalf("expected thought text to remain in prose, got %q", prose)
	}
	if !strings.Contains(prose, "<tool_call>") {
		t.Fatalf("expected tool call inside thought to remain in prose, got %q", prose)
	}
}

func TestParseToolCalls_ThoughtAliases(t *testing.T) {
	response := `<thinking>
  <tool_call>
    <tool_id>ignored</tool_id>
    <parameters><query>inside</query></parameters>
  </tool_call>
</thinking>
<thought>
  still thinking
</thought>`

	calls, prose, err := ParseToolCalls(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no parsed calls, got %d", len(calls))
	}
	if !strings.Contains(prose, "still thinking") {
		t.Fatalf("expected thought text in prose, got %q", prose)
	}
}

// --- AC-3: Parser — malformed block is skipped ---

func TestParseToolCalls_MalformedBlockSkipped(t *testing.T) {
	response := `<tool_call>
  <tool_id>good_tool</tool_id>
  <parameters><key>value</key></parameters>
</tool_call>
<tool_call>
  NOT VALID XML <<<
</tool_call>`

	calls, _, err := ParseToolCalls(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 valid call, got %d", len(calls))
	}
	if calls[0].ToolID != "good_tool" {
		t.Errorf("expected 'good_tool', got %q", calls[0].ToolID)
	}
}

// --- AC-4: Parser — no tool calls ---

func TestParseToolCalls_NoToolCalls(t *testing.T) {
	response := "This is a plain text response with no tool calls."

	calls, prose, err := ParseToolCalls(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
	if prose != response {
		t.Errorf("expected prose to equal original response, got %q", prose)
	}
}

// --- AC-5: Executor — calls MCP and returns result ---

func TestExecutor_CallsMCP(t *testing.T) {
	client := &mockMCPClient{
		toolIDs:  []string{"web_search"},
		response: "search results",
	}
	reg := NewRegistry([]MCPClient{client}, nil)
	exec := NewExecutor(reg, audit.Nop())

	result, err := exec.Execute(context.Background(), wireCall("web_search", `{"q":"golang"}`))
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result != "search results" {
		t.Errorf("expected 'search results', got %q", result)
	}
	if client.callCount != 1 {
		t.Errorf("expected MCP client called once, got %d", client.callCount)
	}
	if client.lastParams["q"] != "golang" {
		t.Errorf("expected param q='golang', got %q", client.lastParams["q"])
	}
}

// --- AC-6: Executor — unknown tool returns error ---

func TestExecutor_UnknownTool_ReturnsError(t *testing.T) {
	reg := NewRegistry([]MCPClient{}, nil)
	exec := NewExecutor(reg, audit.Nop())

	_, err := exec.Execute(context.Background(), wireCall("nonexistent", `{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// --- AC-7: Executor — MCP client error propagates ---

func TestExecutor_MCPError_Propagates(t *testing.T) {
	client := &mockMCPClient{
		toolIDs: []string{"web_search"},
		err:     fmt.Errorf("connection refused"),
	}
	reg := NewRegistry([]MCPClient{client}, nil)
	exec := NewExecutor(reg, audit.Nop())

	_, err := exec.Execute(context.Background(), wireCall("web_search", `{}`))
	if err == nil {
		t.Fatal("expected error when MCP call fails")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected error to contain 'connection refused', got %q", err.Error())
	}
}

// --- AC-8: Executor — audit events written ---

func TestExecutor_AuditEvents(t *testing.T) {
	sink := &recordingSink{}
	logger := audit.NewLogger([]audit.SinkConfig{{Sink: sink, MinLevel: audit.LevelDebug}})

	t.Run("success logs KindToolInvoked", func(t *testing.T) {
		client := &mockMCPClient{toolIDs: []string{"web_search"}, response: "ok"}
		exec := NewExecutor(NewRegistry([]MCPClient{client}, nil), logger)
		ctx := audit.WithAgentID(context.Background(), "agent42")

		exec.Execute(ctx, wireCall("web_search", `{}`)) //nolint:errcheck
		logger.Close()

		if len(sink.events) == 0 {
			t.Fatal("expected audit event, got none")
		}
		ev := sink.events[len(sink.events)-1]
		if ev.Kind != audit.KindToolInvoked {
			t.Errorf("expected KindToolInvoked, got %q", ev.Kind)
		}
		if ev.AgentID != "agent42" {
			t.Errorf("expected agentID 'agent42', got %q", ev.AgentID)
		}
	})

	t.Run("failure logs KindToolFailed", func(t *testing.T) {
		sink2 := &recordingSink{}
		logger2 := audit.NewLogger([]audit.SinkConfig{{Sink: sink2, MinLevel: audit.LevelDebug}})
		client := &mockMCPClient{toolIDs: []string{"web_search"}, err: fmt.Errorf("boom")}
		exec := NewExecutor(NewRegistry([]MCPClient{client}, nil), logger2)

		exec.Execute(context.Background(), wireCall("web_search", `{}`)) //nolint:errcheck
		logger2.Close()

		if len(sink2.events) == 0 {
			t.Fatal("expected audit event, got none")
		}
		ev := sink2.events[len(sink2.events)-1]
		if ev.Kind != audit.KindToolFailed {
			t.Errorf("expected KindToolFailed, got %q", ev.Kind)
		}
	})
}

package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
)

// --- mock types ---

type mockMCPClient struct {
	toolIDs   []string
	response  string
	err       error
	callCount int
}

func (m *mockMCPClient) Call(_ context.Context, _ string, _ map[string]string) (string, error) {
	m.callCount++
	return m.response, m.err
}

func (m *mockMCPClient) ToolIDs() []string { return m.toolIDs }
func (m *mockMCPClient) Healthy() bool      { return true }

type mockAuditLogger struct {
	entries []ToolAuditEntry
}

func (m *mockAuditLogger) LogToolCall(entry ToolAuditEntry) {
	m.entries = append(m.entries, entry)
}

// --- helper ---

func singleCallToolCall(toolID string) ToolCall {
	return ToolCall{
		ID:         "test-call-id",
		ToolID:     toolID,
		Parameters: map[string]string{"q": "test"},
		RawXML:     fmt.Sprintf(`<tool_call><tool_id>%s</tool_id><parameters><q>test</q></parameters></tool_call>`, toolID),
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

// --- AC-5: Executor — allow effect calls MCP ---

func TestExecutor_Allow_CallsMCP(t *testing.T) {
	client := &mockMCPClient{
		toolIDs:  []string{"web_search"},
		response: "search results",
	}
	reg := NewRegistry([]MCPClient{client})
	perms := []config.Permission{
		{Action: "tool:invoke", Scope: "web_search", Effect: "allow"},
	}
	audit := &mockAuditLogger{}
	exec := NewExecutor(reg, perms, nil, audit, ExecutorConfig{MaxIterations: 10})

	call := singleCallToolCall("web_search")
	result := exec.Execute(context.Background(), "agent1", call)

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Content != "search results" {
		t.Errorf("expected 'search results', got %q", result.Content)
	}
	if client.callCount != 1 {
		t.Errorf("expected MCP client called once, got %d", client.callCount)
	}
}

// --- AC-6: Executor — deny effect returns error result without calling MCP ---

func TestExecutor_Deny_DoesNotCallMCP(t *testing.T) {
	client := &mockMCPClient{
		toolIDs:  []string{"email:send"},
		response: "sent",
	}
	reg := NewRegistry([]MCPClient{client})
	perms := []config.Permission{} // no permissions for email:send
	audit := &mockAuditLogger{}
	exec := NewExecutor(reg, perms, nil, audit, ExecutorConfig{MaxIterations: 10})

	call := singleCallToolCall("email:send")
	result := exec.Execute(context.Background(), "agent1", call)

	if !result.IsError {
		t.Fatal("expected error result for denied tool")
	}
	if client.callCount != 0 {
		t.Errorf("expected MCP client never called, got %d calls", client.callCount)
	}
}

// --- AC-7: Executor — require_confirmation with nil channel auto-denies ---

func TestExecutor_RequireConfirmation_NilChannel_AutoDenies(t *testing.T) {
	client := &mockMCPClient{
		toolIDs:  []string{"email:send"},
		response: "sent",
	}
	reg := NewRegistry([]MCPClient{client})
	perms := []config.Permission{
		{Action: "tool:invoke", Scope: "email:send", Effect: "require_confirmation"},
	}
	exec := NewExecutor(reg, perms, nil, nil, ExecutorConfig{MaxIterations: 10})

	call := singleCallToolCall("email:send")
	result := exec.Execute(context.Background(), "agent1", call)

	if !result.IsError {
		t.Fatal("expected error result for require_confirmation with nil channel")
	}
	if !strings.Contains(result.Error, "require_confirmation") {
		t.Errorf("expected error to mention require_confirmation, got %q", result.Error)
	}
	if client.callCount != 0 {
		t.Errorf("expected MCP client never called, got %d calls", client.callCount)
	}
}

// --- AC-8: Executor — require_confirmation approved ---

func TestExecutor_RequireConfirmation_Approved(t *testing.T) {
	client := &mockMCPClient{
		toolIDs:  []string{"email:send"},
		response: "sent",
	}
	reg := NewRegistry([]MCPClient{client})
	perms := []config.Permission{
		{Action: "tool:invoke", Scope: "email:send", Effect: "require_confirmation"},
	}

	confirmCh := make(chan ConfirmationRequest, 1)
	exec := NewExecutor(reg, perms, confirmCh, nil, ExecutorConfig{MaxIterations: 10})

	// Approve the confirmation in a goroutine.
	go func() {
		req := <-confirmCh
		req.Response <- true
	}()

	call := singleCallToolCall("email:send")
	result := exec.Execute(context.Background(), "agent1", call)

	if result.IsError {
		t.Fatalf("expected success after approval, got error: %s", result.Error)
	}
	if result.Content != "sent" {
		t.Errorf("expected 'sent', got %q", result.Content)
	}
	if client.callCount != 1 {
		t.Errorf("expected MCP client called once, got %d", client.callCount)
	}
}

// --- AC-9: Executor — require_confirmation rejected ---

func TestExecutor_RequireConfirmation_Rejected(t *testing.T) {
	client := &mockMCPClient{
		toolIDs:  []string{"email:send"},
		response: "sent",
	}
	reg := NewRegistry([]MCPClient{client})
	perms := []config.Permission{
		{Action: "tool:invoke", Scope: "email:send", Effect: "require_confirmation"},
	}

	confirmCh := make(chan ConfirmationRequest, 1)
	exec := NewExecutor(reg, perms, confirmCh, nil, ExecutorConfig{MaxIterations: 10})

	// Reject the confirmation in a goroutine.
	go func() {
		req := <-confirmCh
		req.Response <- false
	}()

	call := singleCallToolCall("email:send")
	result := exec.Execute(context.Background(), "agent1", call)

	if !result.IsError {
		t.Fatal("expected error result for rejected confirmation")
	}
	if client.callCount != 0 {
		t.Errorf("expected MCP client never called after rejection, got %d calls", client.callCount)
	}
}

// --- AC-10: Executor — unreachable MCP server returns error result ---

func TestExecutor_MCPCallError_ReturnsErrorResult(t *testing.T) {
	client := &mockMCPClient{
		toolIDs: []string{"web_search"},
		err:     fmt.Errorf("connection refused"),
	}
	reg := NewRegistry([]MCPClient{client})
	perms := []config.Permission{
		{Action: "tool:invoke", Scope: "web_search", Effect: "allow"},
	}
	exec := NewExecutor(reg, perms, nil, nil, ExecutorConfig{MaxIterations: 10})

	call := singleCallToolCall("web_search")
	result := exec.Execute(context.Background(), "agent1", call)

	if !result.IsError {
		t.Fatal("expected error result when MCP call fails")
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// --- AC-11: Executor — audit log entry written for every call ---

func TestExecutor_AuditLogWritten(t *testing.T) {
	client := &mockMCPClient{
		toolIDs:  []string{"web_search"},
		response: "results",
	}
	reg := NewRegistry([]MCPClient{client})
	perms := []config.Permission{
		{Action: "tool:invoke", Scope: "web_search", Effect: "allow"},
	}
	audit := &mockAuditLogger{}
	exec := NewExecutor(reg, perms, nil, audit, ExecutorConfig{MaxIterations: 10})

	call := singleCallToolCall("web_search")
	exec.Execute(context.Background(), "agent42", call)

	if len(audit.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(audit.entries))
	}
	entry := audit.entries[0]
	if entry.ToolID != "web_search" {
		t.Errorf("expected tool_id 'web_search', got %q", entry.ToolID)
	}
	if entry.AgentID != "agent42" {
		t.Errorf("expected agent_id 'agent42', got %q", entry.AgentID)
	}
	if entry.Effect != "allow" {
		t.Errorf("expected effect 'allow', got %q", entry.Effect)
	}
	if entry.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if entry.Timestamp.After(time.Now().Add(time.Second)) {
		t.Error("timestamp is in the future")
	}
}

// --- Scope matching ---

func TestMatchScope(t *testing.T) {
	tests := []struct {
		scope  string
		toolID string
		want   bool
	}{
		{"*", "anything", true},
		{"web_search", "web_search", true},
		{"web_search", "email:send", false},
		{"email:*", "email:send", true},
		{"email:*", "email:reply", true},
		{"email:*", "web_search", false},
		{"email:send", "email:send", true},
		{"email:send", "email:reply", false},
	}
	for _, tt := range tests {
		got := matchScope(tt.scope, tt.toolID)
		if got != tt.want {
			t.Errorf("matchScope(%q, %q) = %v, want %v", tt.scope, tt.toolID, got, tt.want)
		}
	}
}

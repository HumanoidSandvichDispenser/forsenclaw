package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/policy"
)

// recordingExecutor records Execute calls and can supply a per-call resource
// clearance, so it doubles as a ToolExecutor and a ResourceClearanceResolver.
type recordingExecutor struct {
	executed   []inference.ToolCallWire
	result     string
	resolveClr func(inference.ToolCallWire) (int, bool)
}

func (e *recordingExecutor) AllDefinitions() []inference.ToolDefinition { return nil }

func (e *recordingExecutor) Execute(_ context.Context, call inference.ToolCallWire) (string, error) {
	e.executed = append(e.executed, call)
	if e.result == "" {
		return "ok", nil
	}
	return e.result, nil
}

func (e *recordingExecutor) ResolveResourceClearance(call inference.ToolCallWire) (int, bool) {
	if e.resolveClr == nil {
		return 0, false
	}
	return e.resolveClr(call)
}

// callOf builds a minimal tool call for toolEffect tests.
func callOf(name string) inference.ToolCallWire {
	return inference.ToolCallWire{Function: inference.ToolFunctionWire{Name: name}}
}

// --- mocks for InferenceHandler tests ---

type mockInferenceAssembler struct {
	calls   int32
	payload inference.ContextPayload
}

func (m *mockInferenceAssembler) Assemble(_ context.Context, _ *Agent, _ Request, tools []inference.ToolDefinition) (inference.ContextPayload, error) {
	atomic.AddInt32(&m.calls, 1)
	p := m.payload
	p.ToolDefinitions = tools
	return p, nil
}

func (m *mockInferenceAssembler) EffectiveClearance(_ context.Context, _ *Agent, _ int64) (int, error) {
	return 5, nil
}

type mockInferenceExecutor struct{}

func (m *mockInferenceExecutor) AllDefinitions() []inference.ToolDefinition {
	return []inference.ToolDefinition{
		{Name: "test_tool", Resource: "frsn:tool/test/test_tool", Clearance: 5},
	}
}

func (m *mockInferenceExecutor) Execute(_ context.Context, _ inference.ToolCallWire) (string, error) {
	return "tool_result_content", nil
}

// sseChunk returns a single-line SSE data frame.
func sseChunk(data string) string {
	return fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", data)
}

// TestInferenceLoop_AssembleCalledOnce verifies that Assemble is called exactly
// once across a two-turn inference loop (tool call then text response). Without
// the basePayload cache, Assemble would be called again on the second turn,
// re-reading room history from the DB (which by then includes the tool
// call/result), causing duplicates and re-appending the Request.
func TestInferenceLoop_AssembleCalledOnce(t *testing.T) {
	var inferCalls int32
	var secondReqBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inferCalls, 1)
		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if n == 1 {
			// First call: tool call response.
			fmt.Fprint(w, sseChunk(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc1","type":"function","function":{"name":"test_tool","arguments":"{}"}}]},"finish_reason":"stop"}]}`))
		} else {
			secondReqBody = body
			// Second call: final text response.
			fmt.Fprint(w, sseChunk(`{"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`))
		}
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	reg, err := inference.NewRegistry(&config.ServerConfig{
		Providers: []config.Provider{
			{Name: "test", Protocol: "openai_compatible", BaseURL: server.URL},
		},
		Models: map[string]config.Model{
			"test-model": {Provider: "test", ProviderModel: "test-model"},
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	ag, _ := NewAgent(&config.AgentDefinition{
		Name:   "test",
		Models: config.AgentModels{Primary: "test-model"},
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"},
		},
	})

	asm := &mockInferenceAssembler{
		payload: inference.ContextPayload{Request: "get me a forsen line"},
	}
	exec := &mockInferenceExecutor{}

	h := &InferenceHandler{
		agent:     ag,
		registry:  reg,
		assembler: asm,
		executor:  exec,
	}

	deps, result, err := h.Handle(context.Background(), map[string]dag.Result{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if deps != nil {
		t.Fatalf("expected no deps, got %v", deps)
	}
	if result == nil || result.Content != "done" {
		t.Fatalf("expected result 'done', got %v", result)
	}

	if got := atomic.LoadInt32(&asm.calls); got != 1 {
		t.Errorf("Assemble called %d times, want 1", got)
	}

	if atomic.LoadInt32(&inferCalls) != 2 {
		t.Errorf("inference calls = %d, want 2", inferCalls)
	}

	// Verify the second request has exactly one assistant message with tool calls
	// (i.e. no duplicate from a stale DB re-read).
	var reqBody struct {
		Messages []struct {
			Role      string `json:"role"`
			ToolCalls []any  `json:"tool_calls,omitempty"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(secondReqBody, &reqBody); err != nil {
		t.Fatalf("unmarshal second request body: %v", err)
	}
	toolCallMsgs := 0
	for _, m := range reqBody.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			toolCallMsgs++
		}
	}
	if toolCallMsgs != 1 {
		t.Errorf("second request has %d assistant tool-call messages, want exactly 1", toolCallMsgs)
	}
}

func TestFilterToolsByClearance_NoReadUp(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "email_send", Clearance: 2},
		{Name: "finances_read", Clearance: 5},
	}
	filtered, clearances := filterToolsByClearance(tools, 3)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered tool, got %d", len(filtered))
	}
	if filtered[0].Name != "email_send" {
		t.Errorf("expected email_send, got %q", filtered[0].Name)
	}
	if _, ok := clearances["finances_read"]; !ok {
		t.Error("expected finances_read in clearances map")
	}
}

func TestFilterToolsByClearance_NoWriteDown(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "email_send", Description: "Send an email.", Clearance: 2},
	}
	filtered, _ := filterToolsByClearance(tools, 4)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered tool, got %d", len(filtered))
	}
	if filtered[0].Clearance != 2 {
		t.Errorf("expected clearance 2, got %d", filtered[0].Clearance)
	}
	if filtered[0].Description == "Send an email." {
		t.Error("expected description to be annotated with write-down warning")
	}
	if !strings.Contains(filtered[0].Description, "write-down risk") {
		t.Errorf("expected write-down warning in description, got %q", filtered[0].Description)
	}
}

func TestFilterToolsByClearance_EqualClearance(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "email_send", Description: "Send an email.", Clearance: 3},
	}
	filtered, _ := filterToolsByClearance(tools, 3)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered tool, got %d", len(filtered))
	}
	if filtered[0].Description != "Send an email." {
		t.Errorf("expected description unchanged, got %q", filtered[0].Description)
	}
}

func TestFilterToolsByClearance_Mixed(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "web_search", Description: "Search the web.", Clearance: 1},
		{Name: "email_send", Description: "Send an email.", Clearance: 2},
		{Name: "calendar_read", Description: "Read calendar.", Clearance: 3},
		{Name: "finances_read", Description: "Read finances.", Clearance: 5},
	}
	filtered, clearances := filterToolsByClearance(tools, 3)

	if len(filtered) != 3 {
		t.Fatalf("expected 3 filtered tools, got %d", len(filtered))
	}

	// web_search (1 < 3) — annotated
	if !strings.Contains(filtered[0].Description, "write-down risk") {
		t.Errorf("expected web_search annotated, got %q", filtered[0].Description)
	}
	// email_send (2 < 3) — annotated
	if !strings.Contains(filtered[1].Description, "write-down risk") {
		t.Errorf("expected email_send annotated, got %q", filtered[1].Description)
	}
	// calendar_read (3 == 3) — unchanged
	if filtered[2].Description != "Read calendar." {
		t.Errorf("expected calendar_read unchanged, got %q", filtered[2].Description)
	}
	// finances_read (5 > 3) — dropped but in map
	if _, ok := clearances["finances_read"]; !ok {
		t.Error("expected finances_read in clearances map")
	}
}

func TestToolEffect_BLPReadUp(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test"})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"finances_read": 5},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("finances_read")).Effect); got != "deny" {
		t.Errorf("toolEffect(finances_read) = %q, want deny", got)
	}
}

func TestToolEffect_BLPWriteDown(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 4,
		toolClearances:     map[string]int{"email_send": 2},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("email_send")).Effect); got != "require_confirmation" {
		t.Errorf("toolEffect(email_send) = %q, want require_confirmation", got)
	}
}

func TestToolEffect_WriteDownWithoutGrantDenies(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test"})
	h := &InferenceHandler{
		effectiveClearance: 4,
		toolClearances:     map[string]int{"email_send": 2},
		agent:              ag,
	}
	// A write-down does not bypass the capability gate: with no grant the
	// most-restrictive fold keeps the grant's default deny.
	if got := string(h.toolEffect(callOf("email_send")).Effect); got != "deny" {
		t.Errorf("toolEffect(email_send) = %q, want deny", got)
	}
}

func TestToolEffect_BLPEqualClearance(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/*"}, Effect: "allow"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"calendar_read": 3},
		toolResources:      map[string]string{"calendar_read": "builtin/calendar_read"},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("calendar_read")).Effect); got != "allow" {
		t.Errorf("toolEffect(calendar_read) = %q, want allow", got)
	}
}

func TestToolEffect_BLPEqualClearanceNoPermission(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test"})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"calendar_read": 3},
		toolResources:      map[string]string{"calendar_read": "builtin/calendar_read"},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("calendar_read")).Effect); got != "deny" {
		t.Errorf("toolEffect(calendar_read) = %q, want deny", got)
	}
}

func TestToolEffect_WildcardPermission(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"calendar_read": 3},
		toolResources:      map[string]string{"calendar_read": "builtin/calendar_read"},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("calendar_read")).Effect); got != "allow" {
		t.Errorf("toolEffect(calendar_read) = %q, want allow", got)
	}
}

func TestToolEffect_ResourcePolicyRestricts(t *testing.T) {
	// Agent is broadly granted; the resource policy tightens a specific tool.
	wildcard := config.Statement{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"}

	tests := []struct {
		name       string
		resPolicy  []config.Statement
		wantEffect string
	}{
		{
			name:       "no resource policy leaves the grant intact",
			wantEffect: "allow",
		},
		{
			name:       "resource policy deny overrides the grant",
			resPolicy:  []config.Statement{{Actions: []string{"tool:invoke"}, Resources: []string{"frsn:tool/builtin/email_send"}, Effect: "deny"}},
			wantEffect: "deny",
		},
		{
			name:       "resource policy confirm overrides the grant",
			resPolicy:  []config.Statement{{Actions: []string{"tool:invoke"}, Resources: []string{"frsn:tool/builtin/email_send"}, Effect: "require_confirmation"}},
			wantEffect: "require_confirmation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ag, _ := NewAgent(&config.AgentDefinition{
				Name:        "test",
				Permissions: []config.Statement{wildcard},
			})
			h := &InferenceHandler{
				agent:              ag,
				effectiveClearance: 3,
				toolClearances:     map[string]int{"email_send": 3}, // equal: BLP abstains
				toolResources:      map[string]string{"email_send": "frsn:tool/builtin/email_send"},
				resourcePolicies:   tt.resPolicy,
			}
			if got := string(h.toolEffect(callOf("email_send")).Effect); got != tt.wantEffect {
				t.Errorf("toolEffect(email_send) = %q, want %q", got, tt.wantEffect)
			}
		})
	}
}

func TestToolEffect_BLPMissingFromMap(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{},
		toolResources:      map[string]string{"unknown": "builtin/unknown"},
		agent:              ag,
	}
	// Missing tool defaults to 0 clearance, which is < effectiveClearance (3),
	// so a granted call is a write-down and requires confirmation.
	if got := string(h.toolEffect(callOf("unknown")).Effect); got != "require_confirmation" {
		t.Errorf("toolEffect(unknown) = %q, want require_confirmation", got)
	}
}

func TestToolEffect_BLPEqualClearanceSpecificPermission(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/email_send"}, Effect: "allow"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"email_send": 3},
		toolResources:      map[string]string{"email_send": "builtin/email_send"},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("email_send")).Effect); got != "allow" {
		t.Errorf("toolEffect(email_send) = %q, want allow", got)
	}
}

func TestToolEffect_DenyOverridesRequireConfirmation(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/*"}, Effect: "require_confirmation"},
			{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/email_send"}, Effect: "deny"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"email_send": 3},
		toolResources:      map[string]string{"email_send": "builtin/email_send"},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("email_send")).Effect); got != "deny" {
		t.Errorf("toolEffect(email_send) = %q, want deny", got)
	}
}

func TestToolEffect_RequireConfirmationOverridesAllow(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/*"}, Effect: "allow"},
			{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/email_send"}, Effect: "require_confirmation"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"email_send": 3},
		toolResources:      map[string]string{"email_send": "builtin/email_send"},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("email_send")).Effect); got != "require_confirmation" {
		t.Errorf("toolEffect(email_send) = %q, want require_confirmation", got)
	}
}

func TestToolEffect_DenyOverridesAllow(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"},
			{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/email_send"}, Effect: "deny"},
		},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"email_send": 3},
		toolResources:      map[string]string{"email_send": "builtin/email_send"},
		agent:              ag,
	}
	if got := string(h.toolEffect(callOf("email_send")).Effect); got != "deny" {
		t.Errorf("toolEffect(email_send) = %q, want deny", got)
	}
}

// TestToolEffect_PerCallResourceClearance verifies that when the executor
// resolves a per-call resource clearance (e.g. create_room's target ceiling),
// it overrides the static tool clearance and drives the BLP decision.
func TestToolEffect_PerCallResourceClearance(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name: "test",
		Permissions: []config.Statement{
			{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"},
		},
	})
	tests := []struct {
		name       string
		resolved   int
		wantEffect policy.Effect
		wantReason policy.Reason
	}{
		{"read-up denies", 5, policy.Deny, policy.ReasonReadUp},
		{"equal falls through to allow", 3, policy.Allow, policy.ReasonAllowed},
		{"write-down confirms", 1, policy.Confirm, policy.ReasonWriteDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := tt.resolved
			exec := &recordingExecutor{
				resolveClr: func(inference.ToolCallWire) (int, bool) { return resolved, true },
			}
			h := &InferenceHandler{
				agent:              ag,
				effectiveClearance: 3,
				toolClearances:     map[string]int{"create_room": 0}, // static; must be overridden
				toolResources:      map[string]string{"create_room": "frsn:tool/builtin/create_room"},
				executor:           exec,
			}
			d := h.toolEffect(callOf("create_room"))
			if d.Effect != tt.wantEffect {
				t.Errorf("Effect = %q, want %q", d.Effect, tt.wantEffect)
			}
			if d.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", d.Reason, tt.wantReason)
			}
		})
	}
}

// TestToolEffect_ResolverDeclinedUsesStatic verifies that when the resolver
// returns ok=false, the static tool clearance is used instead.
func TestToolEffect_ResolverDeclinedUsesStatic(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test"})
	exec := &recordingExecutor{
		resolveClr: func(inference.ToolCallWire) (int, bool) { return 0, false },
	}
	h := &InferenceHandler{
		agent:              ag,
		effectiveClearance: 3,
		toolClearances:     map[string]int{"x": 5}, // static read-up (5 > 3)
		toolResources:      map[string]string{"x": "builtin/x"},
		executor:           exec,
	}
	d := h.toolEffect(callOf("x"))
	if d.Effect != policy.Deny || d.Reason != policy.ReasonReadUp {
		t.Errorf("got %v/%v, want deny/blp_read_up (static clearance path)", d.Effect, d.Reason)
	}
}

// TestApplyConfirmations covers the three terminal confirmation outcomes.
func TestApplyConfirmations(t *testing.T) {
	tc := inference.ToolCallWire{ID: "c1", Function: inference.ToolFunctionWire{Name: "create_room"}}

	t.Run("deny does not execute", func(t *testing.T) {
		ag, _ := NewAgent(&config.AgentDefinition{Name: "test"})
		exec := &recordingExecutor{}
		h := &InferenceHandler{
			agent:                ag,
			executor:             exec,
			pendingConfirmations: []confirmationEntry{{call: tc, depID: "d1"}},
		}
		err := h.applyConfirmations(context.Background(), map[string]dag.Result{
			"d1": {Status: dag.StatusDenied},
		})
		if err != nil {
			t.Fatalf("applyConfirmations: %v", err)
		}
		if len(exec.executed) != 0 {
			t.Errorf("executor ran %d times on deny, want 0", len(exec.executed))
		}
		if len(h.turnHistory) != 1 || h.turnHistory[0].Content != "Action denied by user." {
			t.Errorf("turnHistory = %+v, want a single denied entry", h.turnHistory)
		}
		if h.turnHistory[0].ToolCallID != "c1" {
			t.Errorf("ToolCallID = %q, want c1", h.turnHistory[0].ToolCallID)
		}
	})

	t.Run("revise does not execute and carries feedback", func(t *testing.T) {
		ag, _ := NewAgent(&config.AgentDefinition{Name: "test"})
		exec := &recordingExecutor{}
		h := &InferenceHandler{
			agent:                ag,
			executor:             exec,
			pendingConfirmations: []confirmationEntry{{call: tc, depID: "d1"}},
		}
		err := h.applyConfirmations(context.Background(), map[string]dag.Result{
			"d1": {Status: dag.StatusRevise, Content: "use a lower clearance"},
		})
		if err != nil {
			t.Fatalf("applyConfirmations: %v", err)
		}
		if len(exec.executed) != 0 {
			t.Errorf("executor ran %d times on revise, want 0", len(exec.executed))
		}
		if len(h.turnHistory) != 1 ||
			h.turnHistory[0].Content != "User asked you to revise this action: use a lower clearance" {
			t.Errorf("turnHistory = %+v, want revise feedback entry", h.turnHistory)
		}
	})

	t.Run("allow executes with edited args", func(t *testing.T) {
		ag, _ := NewAgent(&config.AgentDefinition{Name: "test"})
		exec := &recordingExecutor{result: "done"}
		h := &InferenceHandler{
			agent:                ag,
			executor:             exec,
			pendingConfirmations: []confirmationEntry{{call: tc, depID: "d1"}},
		}
		err := h.applyConfirmations(context.Background(), map[string]dag.Result{
			"d1": {Status: dag.StatusAllowed, EditedArgs: `{"clearance_ceiling":"2"}`},
		})
		if err != nil {
			t.Fatalf("applyConfirmations: %v", err)
		}
		if len(exec.executed) != 1 {
			t.Fatalf("executor ran %d times on allow, want 1", len(exec.executed))
		}
		if exec.executed[0].Function.Arguments != `{"clearance_ceiling":"2"}` {
			t.Errorf("executed args = %q, want edited args", exec.executed[0].Function.Arguments)
		}
		if len(h.turnHistory) != 1 || h.turnHistory[0].Content != "done" {
			t.Errorf("turnHistory = %+v, want tool result 'done'", h.turnHistory)
		}
	})
}

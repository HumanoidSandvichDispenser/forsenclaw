package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
)

// AuditLogger records tool invocations.
type AuditLogger interface {
	LogToolCall(entry ToolAuditEntry)
}

// ToolAuditEntry is a single audit log record.
type ToolAuditEntry struct {
	Timestamp  time.Time
	AgentID    string
	ToolID     string
	CallID     string
	Parameters map[string]string
	Effect     string
	Result     string
	IsError    bool
}

// ConfirmationRequest is sent to the confirmation channel when a tool requires
// user approval before execution.
type ConfirmationRequest struct {
	Call     ToolCall
	Response chan<- bool // send true to approve, false to reject
}

// ExecutorConfig controls executor behaviour.
type ExecutorConfig struct {
	// MaxIterations is the hard cap on agentic loop turns. Default 10.
	MaxIterations int
}

// Executor runs permission-gated tool calls on behalf of an agent.
type Executor struct {
	registry      Registry
	permissions   []config.Permission
	confirmations chan<- ConfirmationRequest // nil means require_confirmation = auto-deny
	audit         AuditLogger
	Cfg           ExecutorConfig
}

// NewExecutor creates a permission-gated Executor.
func NewExecutor(
	registry Registry,
	permissions []config.Permission,
	confirmations chan<- ConfirmationRequest,
	audit AuditLogger,
	cfg ExecutorConfig,
) *Executor {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 10
	}
	return &Executor{
		registry:      registry,
		permissions:   permissions,
		confirmations: confirmations,
		audit:         audit,
		Cfg:           cfg,
	}
}

// Execute checks permissions, routes to the MCP server, and returns a ToolResult.
// It never returns a Go error — all failure modes are captured in ToolResult.IsError.
func (e *Executor) Execute(ctx context.Context, agentID string, call ToolCall) ToolResult {
	effect := e.checkPermission(call.ToolID)

	result := ToolResult{
		CallID: call.ID,
		ToolID: call.ToolID,
	}

	switch effect {
	case "deny":
		result.IsError = true
		result.Error = fmt.Sprintf("permission denied: tool:invoke[%s]", call.ToolID)

	case "require_confirmation":
		if e.confirmations == nil {
			result.IsError = true
			result.Error = fmt.Sprintf("require_confirmation: no confirmation channel available for tool:invoke[%s]", call.ToolID)
		} else {
			respCh := make(chan bool, 1)
			e.confirmations <- ConfirmationRequest{
				Call:     call,
				Response: respCh,
			}
			approved := <-respCh
			if !approved {
				result.IsError = true
				result.Error = fmt.Sprintf("permission denied: user rejected tool:invoke[%s]", call.ToolID)
			} else {
				effect = "require_confirmation:approved"
				result = e.callMCP(ctx, call)
			}
		}

	case "allow":
		result = e.callMCP(ctx, call)
	}

	if e.audit != nil {
		auditResult := result.Content
		if result.IsError {
			auditResult = result.Error
		}
		e.audit.LogToolCall(ToolAuditEntry{
			Timestamp:  time.Now().UTC(),
			AgentID:    agentID,
			ToolID:     call.ToolID,
			CallID:     call.ID,
			Parameters: call.Parameters,
			Effect:     effect,
			Result:     auditResult,
			IsError:    result.IsError,
		})
	}

	return result
}

// checkPermission returns the effect for the given tool ID by walking the
// agent's permission list for the first matching tool:invoke entry.
// Returns "deny" if no entry matches.
func (e *Executor) checkPermission(toolID string) string {
	// TODO: instead of first matching, we will later support specificity-based
	// conflict resolution
	for _, p := range e.permissions {
		if p.Action != "tool:invoke" {
			continue
		}
		if matchScope(p.Scope, toolID) {
			return p.Effect
		}
	}
	return "deny"
}

// matchScope returns true if the scope pattern matches the toolID.
// Supported: exact match, "*" (all), "prefix:*" (prefix wildcard, e.g. "email:*").
func matchScope(scope, toolID string) bool {
	if scope == "*" {
		return true
	}
	if strings.HasSuffix(scope, ":*") {
		prefix := strings.TrimSuffix(scope, "*")
		return strings.HasPrefix(toolID, prefix)
	}
	return scope == toolID
}

// callMCP invokes the MCP server for the given call.
func (e *Executor) callMCP(ctx context.Context, call ToolCall) ToolResult {
	if e.registry == nil {
		return ToolResult{
			CallID:  call.ID,
			ToolID:  call.ToolID,
			IsError: true,
			Error:   "no MCP registry configured",
		}
	}

	client, err := e.registry.Resolve(call.ToolID)
	if err != nil {
		return ToolResult{
			CallID:  call.ID,
			ToolID:  call.ToolID,
			IsError: true,
			Error:   err.Error(),
		}
	}

	content, err := client.Call(ctx, call.ToolID, call.Parameters)
	if err != nil {
		return ToolResult{
			CallID:  call.ID,
			ToolID:  call.ToolID,
			IsError: true,
			Error:   err.Error(),
		}
	}

	return ToolResult{
		CallID:  call.ID,
		ToolID:  call.ToolID,
		Content: content,
	}
}

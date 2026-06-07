package mcp

import (
	"context"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/tool"
)

// registryTool adapts an MCP-backed tool to the unified tool.Tool interface, so
// MCP tools and native tools share one execution path.
type registryTool struct {
	def    inference.ToolDefinition
	client MCPClient
}

// Definition returns the tool's schema for context injection and gating.
func (t registryTool) Definition() inference.ToolDefinition { return t.def }

// Invoke forwards the call to the MCP client. The invocation principal is not
// needed by MCP servers (gating happened upstream); it is accepted to satisfy
// the interface and will matter once execution crosses into a per-agent worker.
func (t registryTool) Invoke(ctx context.Context, _ tool.Invocation, params map[string]string) (string, error) {
	return t.client.Call(ctx, t.def.Name, params)
}

// ResourceClearance bridges tool.DynamicClearance to clients that derive a
// per-call clearance from arguments (e.g. create_room); other clients report no
// dynamic clearance.
func (t registryTool) ResourceClearance(params map[string]string) (int, bool) {
	dc, ok := t.client.(interface {
		ResourceClearance(map[string]string) (int, bool)
	})
	if !ok {
		return 0, false
	}
	return dc.ResourceClearance(params)
}

// Tools adapts every tool known to the registry into a tool.Tool.
func Tools(reg Registry) []tool.Tool {
	defs := reg.AllDefinitions()
	out := make([]tool.Tool, 0, len(defs))
	for _, def := range defs {
		client, err := reg.Resolve(def.Name)
		if err != nil {
			continue
		}
		out = append(out, registryTool{def: def, client: client})
	}
	return out
}

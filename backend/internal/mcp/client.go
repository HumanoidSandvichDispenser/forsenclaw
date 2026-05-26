package mcp

import (
	"context"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

// MCPClient represents a connected MCP server.
type MCPClient interface {
	// Call invokes a named tool with the given parameters.
	// Returns the result text and any error.
	Call(ctx context.Context, toolID string, params map[string]string) (string, error)

	// ToolIDs returns all tool identifiers provided by this server.
	ToolIDs() []string

	// Healthy returns whether the server is currently reachable.
	Healthy() bool
}

// ToolDescriber is an optional interface that MCPClient implementations may
// satisfy to expose their tool schemas for prompt injection and native tool
// calling. Clients that do not implement this interface are silently skipped
// when building schema lists.
type ToolDescriber interface {
	// XMLSchemas returns one pre-formatted XML schema string per tool,
	// suitable for injection into the system prompt in XML tool mode.
	XMLSchemas() []string

	// NativeDefinitions returns one ToolDefinition per tool for use with
	// native tool calling APIs (Anthropic, OpenAI).
	NativeDefinitions() []inference.ToolDefinition
}

// Registry maps tool IDs to their hosting MCPClient and clearance level.
type Registry interface {
	// Resolve returns the MCPClient responsible for the given tool ID.
	// Returns an error if no server is registered for that tool.
	Resolve(toolID string) (MCPClient, error)

	// ToolClearance returns the clearance level for the given tool ID.
	// Returns 0 if the tool is not registered.
	ToolClearance(toolID string) int

	// AllSchemas returns all tool schemas for tools the registry knows about,
	// as pre-formatted strings suitable for injection into ContextPayload.ToolSchemas.
	AllSchemas() []string

	// AllDefinitions returns all tool definitions as structured schemas suitable
	// for native tool calling adapters.
	AllDefinitions() []inference.ToolDefinition
}

// inMemoryRegistry is a simple in-memory Registry backed by a map.
type inMemoryRegistry struct {
	tools      map[string]MCPClient
	clearances map[string]int
	order      []MCPClient // unique clients in insertion order
}

// NewRegistry creates a new in-memory Registry populated from the given servers.
// The clearances map provides the clearance level for each tool ID; missing
// entries default to 0 (which callers should resolve to system max).
func NewRegistry(servers []MCPClient, clearances map[string]int) Registry {
	r := &inMemoryRegistry{
		tools:      make(map[string]MCPClient),
		clearances: make(map[string]int),
	}
	seen := make(map[MCPClient]bool)
	for _, srv := range servers {
		for _, id := range srv.ToolIDs() {
			r.tools[id] = srv
			if clearances != nil {
				r.clearances[id] = clearances[id]
			}
		}
		if !seen[srv] {
			seen[srv] = true
			r.order = append(r.order, srv)
		}
	}
	return r
}

func (r *inMemoryRegistry) Resolve(toolID string) (MCPClient, error) {
	client, ok := r.tools[toolID]
	if !ok {
		return nil, fmt.Errorf("no MCP server registered for tool %q", toolID)
	}
	return client, nil
}

func (r *inMemoryRegistry) ToolClearance(toolID string) int {
	return r.clearances[toolID]
}

func (r *inMemoryRegistry) AllSchemas() []string {
	var schemas []string
	for _, client := range r.order {
		if d, ok := client.(ToolDescriber); ok {
			schemas = append(schemas, d.XMLSchemas()...)
		}
	}
	return schemas
}

func (r *inMemoryRegistry) AllDefinitions() []inference.ToolDefinition {
	var defs []inference.ToolDefinition
	for _, client := range r.order {
		if d, ok := client.(ToolDescriber); ok {
			defs = append(defs, d.NativeDefinitions()...)
		}
	}
	return defs
}

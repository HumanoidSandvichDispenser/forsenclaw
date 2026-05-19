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

// Registry maps tool IDs to their hosting MCPClient.
type Registry interface {
	// Resolve returns the MCPClient responsible for the given tool ID.
	// Returns an error if no server is registered for that tool.
	Resolve(toolID string) (MCPClient, error)

	// AllSchemas returns all tool schemas for tools the registry knows about,
	// as pre-formatted strings suitable for injection into ContextPayload.ToolSchemas.
	AllSchemas() []string

	// AllDefinitions returns all tool definitions as structured schemas suitable
	// for native tool calling adapters.
	AllDefinitions() []inference.ToolDefinition
}

// inMemoryRegistry is a simple in-memory Registry backed by a map.
type inMemoryRegistry struct {
	tools map[string]MCPClient
}

// NewRegistry creates a new in-memory Registry populated from the given servers.
func NewRegistry(servers []MCPClient) Registry {
	r := &inMemoryRegistry{tools: make(map[string]MCPClient)}
	for _, srv := range servers {
		for _, id := range srv.ToolIDs() {
			r.tools[id] = srv
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

func (r *inMemoryRegistry) AllSchemas() []string {
	return nil // v1: schemas are pre-populated by the agent definition
}

func (r *inMemoryRegistry) AllDefinitions() []inference.ToolDefinition {
	return nil // v1: definitions are pre-populated by the agent definition
}

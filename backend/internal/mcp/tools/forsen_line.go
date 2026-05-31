package tools

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
)

var forsenLines = []string{
	"what's this, an LLM for ants?",
	"alright...",
	"harry pottahhhhh",
	"Or as Bruce Lee would say.... WOTAAAAHHHH",
	"Previously on lost...",
	"GFMB forsenE",
	"No, I don't think so .",
	"your mom!",
	"let's see, let's see, the winner gets tea",
	"but first let me take a selfie",
	"if you don't snus, you lose",
	"*stares silentyl*",
	"then i don't need a jacket!",
	"idk kev",
	"how can she slap?",
}

// ForsenLineClient is a minimal built-in tool for testing the tool call
// pipeline. It returns a random forsen line.
type ForsenLineClient struct{}

func (c *ForsenLineClient) Call(_ context.Context, toolID string, _ map[string]string) (string, error) {
	if toolID != "forsen_line" {
		return "", fmt.Errorf("unsupported tool %q", toolID)
	}
	return forsenLines[rand.Intn(len(forsenLines))], nil
}

func (c *ForsenLineClient) ToolIDs() []string { return []string{"forsen_line"} }
func (c *ForsenLineClient) Healthy() bool     { return true }

func (c *ForsenLineClient) NativeDefinitions() []inference.ToolDefinition {
	return []inference.ToolDefinition{
		{
			Name:        "forsen_line",
			Description: "Returns a random forsen line. Use this to verify tool call plumbing is working.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
	}
}

func (c *ForsenLineClient) XMLSchemas() []string { return nil }

var _ mcp.MCPClient = (*ForsenLineClient)(nil)
var _ mcp.ToolDescriber = (*ForsenLineClient)(nil)

package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/tool"
)

// noteTool lets an agent append to its own daily working notes. The note is
// written at the agent's operating clearance, so it reappears in context only
// when the agent is operating at or above that level.
type noteTool struct {
	paths *paths.Paths
}

// NewNoteTool builds the native "note" tool.
func NewNoteTool(p *paths.Paths) tool.Tool {
	return &noteTool{paths: p}
}

func (t *noteTool) Definition() inference.ToolDefinition {
	return inference.ToolDefinition{
		Name: "note",
		Description: "Append an observation to your daily working notes. Use this for things " +
			"worth remembering for today and tomorrow — open threads, decisions, things the " +
			"user told you. The note is recorded at your current clearance.",
		Resource:    "frsn:memory/note",
		DataActions: []string{"memory:write"},
		SelfLeveled: true,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The observation to record.",
				},
			},
			"required": []string{"content"},
		},
	}
}

func (t *noteTool) Invoke(_ context.Context, inv tool.Invocation, params map[string]string) (string, error) {
	content := strings.TrimSpace(params["content"])
	if content == "" {
		return "", fmt.Errorf("note content is required")
	}
	dir := t.paths.AgentClearanceDir(inv.AgentName, inv.OperatingClearance)
	if err := WriteDailyNote(dir, content); err != nil {
		return "", err
	}
	return "Noted.", nil
}

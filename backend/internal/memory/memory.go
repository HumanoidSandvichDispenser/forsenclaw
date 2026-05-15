package memory

import (
	"fmt"
	"os"
	"path/filepath"
)

// MemoryFileName is the filename for an agent's curated long-term memory.
const MemoryFileName = "MEMORY.md"

// ReadMemory reads the agent's MEMORY.md file.
// Returns empty content and no error if the file does not exist.
func ReadMemory(agentDataDir string) (string, error) {
	path := filepath.Join(agentDataDir, MemoryFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading MEMORY.md: %w", err)
	}
	return string(data), nil
}

// WriteMemory writes content to the agent's MEMORY.md file.
// Creates the file (and parent directories) if needed.
func WriteMemory(agentDataDir string, content string) error {
	if err := os.MkdirAll(agentDataDir, 0755); err != nil {
		return fmt.Errorf("creating agent data dir: %w", err)
	}
	path := filepath.Join(agentDataDir, MemoryFileName)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing MEMORY.md: %w", err)
	}
	return nil
}

// EnsureMemory creates MEMORY.md with a default template if it doesn't exist
// and identity_continuity is enabled.
func EnsureMemory(agentDataDir string, agentName string, enabled bool) error {
	if !enabled {
		return nil
	}
	path := filepath.Join(agentDataDir, MemoryFileName)
	_, err := os.Stat(path)
	if err == nil {
		return nil // already exists
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("checking MEMORY.md: %w", err)
	}

	template := fmt.Sprintf("# %s — Memory\n\n", agentName)
	template += "This file contains durable facts, preferences, standing decisions, "
	template += "and concise summaries about the user.\n\n"
	template += "## Key Facts\n\n"
	template += "- \n\n"
	template += "## Preferences\n\n"
	template += "- \n\n"
	template += "## Standing Decisions\n\n"
	template += "- \n"

	return WriteMemory(agentDataDir, template)
}

package memory

import (
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

// ClearanceLevel is one clearance band of an agent's on-disk memory: the
// MEMORY.md and recent daily notes stored at that level. Clearance 0 is the
// legacy flat directory (unleveled baseline) that predates the clearance split.
type ClearanceLevel struct {
	Clearance int
	Memory    string
	Notes     []DailyNote
}

// Inspector reads an agent's memory and daily notes broken out by clearance
// level, for the memory/notes inspection dock. It is read-only and applies no
// access control itself — the caller bounds maxClearance to the viewer's
// clearance (no read-up).
type Inspector struct {
	paths *paths.Paths
}

// NewInspector creates an Inspector over the given paths.
func NewInspector(p *paths.Paths) *Inspector {
	return &Inspector{paths: p}
}

// Inspect returns the agent's memory levels: the legacy flat baseline (when
// includeFlat is set) followed by clearance levels 1..maxClearance. Empty levels
// (no memory and no notes) are omitted. maxClearance must already be bounded to
// the viewer's clearance by the caller.
func (i *Inspector) Inspect(name string, maxClearance int, includeFlat bool) ([]ClearanceLevel, error) {
	var levels []ClearanceLevel

	if includeFlat {
		level, err := i.readLevel(i.paths.AgentDataDir(name), 0)
		if err != nil {
			return nil, err
		}
		if level != nil {
			levels = append(levels, *level)
		}
	}

	for lvl := 1; lvl <= maxClearance; lvl++ {
		level, err := i.readLevel(i.paths.AgentClearanceDir(name, lvl), lvl)
		if err != nil {
			return nil, err
		}
		if level != nil {
			levels = append(levels, *level)
		}
	}
	return levels, nil
}

// readLevel reads one directory's memory + recent daily notes, returning nil
// when the level holds nothing.
func (i *Inspector) readLevel(dir string, clearance int) (*ClearanceLevel, error) {
	mem, err := ReadMemory(dir)
	if err != nil {
		return nil, err
	}
	notes, err := ReadDailyNotes(dir, true)
	if err != nil {
		return nil, err
	}
	if mem == "" && len(notes) == 0 {
		return nil, nil
	}
	return &ClearanceLevel{Clearance: clearance, Memory: mem, Notes: notes}, nil
}

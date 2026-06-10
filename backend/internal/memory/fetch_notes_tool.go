package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/tool"
)

// fetchNotesDateLayout is the date format the tool accepts and emits.
const fetchNotesDateLayout = "2006-01-02"

// fetchNotesMaxSpanDays bounds the requested range so a single call can't dump
// an unbounded amount of history into the model context.
const fetchNotesMaxSpanDays = 92

// fetchNotesTool retrieves an agent's own daily notes for a date or date range.
// It reads across the agent's clearance strata up to its operating clearance,
// so it never surfaces notes recorded above the level the agent is acting at.
type fetchNotesTool struct {
	paths *paths.Paths
}

// NewFetchNotesTool builds the native "fetch_notes" tool.
func NewFetchNotesTool(p *paths.Paths) tool.Tool {
	return &fetchNotesTool{paths: p}
}

func (t *fetchNotesTool) Definition() inference.ToolDefinition {
	return inference.ToolDefinition{
		Name: "fetch_notes",
		Description: "Retrieve your daily working notes for a past date or date range. " +
			"Dates are UTC in YYYY-MM-DD form. Provide start_date alone to fetch a single " +
			"day, or start_date and end_date (inclusive) to fetch a range. Only notes recorded " +
			"at or below your current clearance are returned.",
		Resource:    "frsn:memory/note",
		DataActions: []string{"memory:read"},
		SelfLeveled: true,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"start_date": map[string]interface{}{
					"type":        "string",
					"description": "First day to fetch, UTC, YYYY-MM-DD.",
				},
				"end_date": map[string]interface{}{
					"type":        "string",
					"description": "Last day to fetch (inclusive), UTC, YYYY-MM-DD. Defaults to start_date.",
				},
			},
			"required": []string{"start_date"},
		},
	}
}

func (t *fetchNotesTool) Invoke(_ context.Context, inv tool.Invocation, params map[string]string) (string, error) {
	start, err := time.Parse(fetchNotesDateLayout, strings.TrimSpace(params["start_date"]))
	if err != nil {
		return "", fmt.Errorf("start_date must be in YYYY-MM-DD form: %w", err)
	}
	start = start.UTC().Truncate(24 * time.Hour)

	end := start
	if raw := strings.TrimSpace(params["end_date"]); raw != "" {
		end, err = time.Parse(fetchNotesDateLayout, raw)
		if err != nil {
			return "", fmt.Errorf("end_date must be in YYYY-MM-DD form: %w", err)
		}
		end = end.UTC().Truncate(24 * time.Hour)
	}

	if end.Before(start) {
		return "", fmt.Errorf("end_date %s is before start_date %s", end.Format(fetchNotesDateLayout), start.Format(fetchNotesDateLayout))
	}
	if days := int(end.Sub(start).Hours()/24) + 1; days > fetchNotesMaxSpanDays {
		return "", fmt.Errorf("range spans %d days; fetch at most %d days at a time", days, fetchNotesMaxSpanDays)
	}

	// Lowest clearance first, mirroring assembly order, so a day's notes read
	// from least- to most-privileged stratum.
	dirs := agentMemoryDirsUpTo(t.paths, inv.AgentName, inv.OperatingClearance)

	var b strings.Builder
	for d := start; !d.After(end); d = d.Add(24 * time.Hour) {
		var dayParts []string
		for _, dir := range dirs {
			note, err := ReadDailyNoteForDate(dir, d)
			if err != nil {
				return "", err
			}
			if note.Content != "" {
				dayParts = append(dayParts, strings.TrimSpace(note.Content))
			}
		}
		if len(dayParts) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# ")
		b.WriteString(d.Format(fetchNotesDateLayout))
		b.WriteString("\n\n")
		b.WriteString(strings.Join(dayParts, "\n\n"))
	}

	if b.Len() == 0 {
		if start.Equal(end) {
			return fmt.Sprintf("No notes found for %s.", start.Format(fetchNotesDateLayout)), nil
		}
		return fmt.Sprintf("No notes found between %s and %s.", start.Format(fetchNotesDateLayout), end.Format(fetchNotesDateLayout)), nil
	}
	return b.String(), nil
}

package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/tool"
)

// writeNoteAt writes a note file for a given date into an agent data dir's
// memory subdir, simulating a note recorded on that day.
func writeNoteAt(t *testing.T, agentDataDir, date, content string) {
	t.Helper()
	notesDir := filepath.Join(agentDataDir, dailyNotesSubdir)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, date+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
}

func TestFetchNotesTool(t *testing.T) {
	dir := t.TempDir()
	p := paths.NewPathsFromRoots(dir, dir, dir)
	tl := NewFetchNotesTool(p)

	writeNoteAt(t, p.AgentClearanceDir("housewife", 1), "2026-06-01", "low note")
	writeNoteAt(t, p.AgentClearanceDir("housewife", 3), "2026-06-01", "high note")
	writeNoteAt(t, p.AgentClearanceDir("housewife", 1), "2026-06-02", "day two")

	invAt := func(clearance int) tool.Invocation {
		return tool.Invocation{AgentName: "housewife", OperatingClearance: clearance}
	}

	t.Run("single date includes notes at and below operating clearance", func(t *testing.T) {
		out, err := tl.Invoke(context.Background(), invAt(3), map[string]string{"start_date": "2026-06-01"})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if !strings.Contains(out, "low note") || !strings.Contains(out, "high note") {
			t.Errorf("expected both strata at clearance 3, got: %q", out)
		}
	})

	t.Run("read-up is blocked: above operating clearance is hidden", func(t *testing.T) {
		out, err := tl.Invoke(context.Background(), invAt(2), map[string]string{"start_date": "2026-06-01"})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if !strings.Contains(out, "low note") {
			t.Errorf("expected c1 note visible at clearance 2, got: %q", out)
		}
		if strings.Contains(out, "high note") {
			t.Errorf("c3 note must not be visible at clearance 2, got: %q", out)
		}
	})

	t.Run("range spans multiple days", func(t *testing.T) {
		out, err := tl.Invoke(context.Background(), invAt(1), map[string]string{
			"start_date": "2026-06-01", "end_date": "2026-06-02",
		})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if !strings.Contains(out, "low note") || !strings.Contains(out, "day two") {
			t.Errorf("expected both days, got: %q", out)
		}
		// Oldest first.
		if strings.Index(out, "2026-06-01") > strings.Index(out, "2026-06-02") {
			t.Errorf("expected oldest date first, got: %q", out)
		}
	})

	t.Run("no notes found", func(t *testing.T) {
		out, err := tl.Invoke(context.Background(), invAt(5), map[string]string{"start_date": "2020-01-01"})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if !strings.Contains(out, "No notes found") {
			t.Errorf("expected not-found message, got: %q", out)
		}
	})

	t.Run("end before start errors", func(t *testing.T) {
		_, err := tl.Invoke(context.Background(), invAt(5), map[string]string{
			"start_date": "2026-06-02", "end_date": "2026-06-01",
		})
		if err == nil {
			t.Error("expected error for inverted range")
		}
	})

	t.Run("bad date format errors", func(t *testing.T) {
		_, err := tl.Invoke(context.Background(), invAt(5), map[string]string{"start_date": "June 1"})
		if err == nil {
			t.Error("expected error for malformed date")
		}
	})

	t.Run("oversized range errors", func(t *testing.T) {
		_, err := tl.Invoke(context.Background(), invAt(5), map[string]string{
			"start_date": "2026-01-01", "end_date": "2026-12-31",
		})
		if err == nil {
			t.Error("expected error for range exceeding the span cap")
		}
	})
}

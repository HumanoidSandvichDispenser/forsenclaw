package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const dailyNotesSubdir = "memory"

// DailyNote represents a single day's working notes.
type DailyNote struct {
	Date    time.Time
	Content string
}

// ReadDailyNotes reads today's and yesterday's daily note files for the agent.
// Returns empty slice if daily_notes feature is disabled.
func ReadDailyNotes(agentDataDir string, enabled bool) ([]DailyNote, error) {
	if !enabled {
		return nil, nil
	}

	notesDir := filepath.Join(agentDataDir, dailyNotesSubdir)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.Add(-24 * time.Hour)

	var notes []DailyNote
	for _, date := range []time.Time{yesterday, today} {
		note, err := readDailyNote(notesDir, date)
		if err != nil {
			return nil, err
		}
		if note.Content != "" {
			notes = append(notes, note)
		}
	}
	return notes, nil
}

// WriteDailyNote appends content to today's daily note file.
// Creates the file and parent directories if needed.
func WriteDailyNote(agentDataDir string, content string) error {
	notesDir := filepath.Join(agentDataDir, dailyNotesSubdir)
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		return fmt.Errorf("creating daily notes dir: %w", err)
	}

	path := dailyNotePath(notesDir, time.Now().UTC())
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening daily note: %w", err)
	}
	defer f.Close()

	timestamp := time.Now().UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("\n## %s\n\n%s\n", timestamp, content)
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("writing daily note: %w", err)
	}
	return nil
}

func readDailyNote(notesDir string, date time.Time) (DailyNote, error) {
	path := dailyNotePath(notesDir, date)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DailyNote{Date: date, Content: ""}, nil
		}
		return DailyNote{}, fmt.Errorf("reading daily note %s: %w", path, err)
	}
	return DailyNote{Date: date, Content: string(data)}, nil
}

func dailyNotePath(notesDir string, date time.Time) string {
	filename := date.Format("2006-01-02") + ".md"
	return filepath.Join(notesDir, filename)
}

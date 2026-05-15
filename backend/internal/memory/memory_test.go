package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadWriteMemory(t *testing.T) {
	dir := t.TempDir()

	// Read non-existent → empty, no error
	content, err := ReadMemory(dir)
	if err != nil {
		t.Fatalf("ReadMemory failed: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}

	// Write
	if err := WriteMemory(dir, "# Test Memory\n\nKey fact."); err != nil {
		t.Fatalf("WriteMemory failed: %v", err)
	}

	// Read back
	content, err = ReadMemory(dir)
	if err != nil {
		t.Fatalf("ReadMemory failed: %v", err)
	}
	if content != "# Test Memory\n\nKey fact." {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestEnsureMemory(t *testing.T) {
	dir := t.TempDir()

	// With identity_continuity enabled
	if err := EnsureMemory(dir, "test-agent", true); err != nil {
		t.Fatalf("EnsureMemory failed: %v", err)
	}

	content, err := ReadMemory(dir)
	if err != nil {
		t.Fatalf("ReadMemory failed: %v", err)
	}
	if !strings.Contains(content, "test-agent") {
		t.Fatalf("expected template to contain agent name, got: %q", content)
	}

	// Second call should be no-op
	if err := EnsureMemory(dir, "test-agent", true); err != nil {
		t.Fatalf("EnsureMemory idempotent failed: %v", err)
	}

	// With identity_continuity disabled → no file created
	dir2 := t.TempDir()
	if err := EnsureMemory(dir2, "test-agent", false); err != nil {
		t.Fatalf("EnsureMemory disabled failed: %v", err)
	}
	_, err = os.Stat(filepath.Join(dir2, MemoryFileName))
	if !os.IsNotExist(err) {
		t.Fatal("expected no MEMORY.md when disabled")
	}
}

func TestDailyNotes(t *testing.T) {
	dir := t.TempDir()

	// Read with no notes → empty
	notes, err := ReadDailyNotes(dir, true)
	if err != nil {
		t.Fatalf("ReadDailyNotes failed: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected 0 notes, got %d", len(notes))
	}

	// Disabled → empty
	notes, err = ReadDailyNotes(dir, false)
	if err != nil {
		t.Fatalf("ReadDailyNotes disabled failed: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected 0 notes when disabled, got %d", len(notes))
	}

	// Write a note
	if err := WriteDailyNote(dir, "Observation 1"); err != nil {
		t.Fatalf("WriteDailyNote failed: %v", err)
	}

	// Read should find today's note
	notes, err = ReadDailyNotes(dir, true)
	if err != nil {
		t.Fatalf("ReadDailyNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Observation 1") {
		t.Fatalf("expected note to contain 'Observation 1', got: %q", notes[0].Content)
	}

	// Write another note
	if err := WriteDailyNote(dir, "Observation 2"); err != nil {
		t.Fatalf("WriteDailyNote 2 failed: %v", err)
	}

	notes, err = ReadDailyNotes(dir, true)
	if err != nil {
		t.Fatalf("ReadDailyNotes failed: %v", err)
	}
	if len(notes) != 1 { // still just today
		t.Fatalf("expected 1 note (today), got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Observation 1") {
		t.Fatalf("expected note to contain 'Observation 1', got: %q", notes[0].Content)
	}
	if !strings.Contains(notes[0].Content, "Observation 2") {
		t.Fatalf("expected note to contain 'Observation 2', got: %q", notes[0].Content)
	}
}

func TestDailyNotesYesterday(t *testing.T) {
	dir := t.TempDir()
	notesDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		t.Fatalf("creating notes dir: %v", err)
	}

	// Create yesterday's note
	yesterday := time.Now().UTC().Add(-24*time.Hour).Format("2006-01-02") + ".md"
	yesterdayPath := filepath.Join(notesDir, yesterday)
	if err := os.WriteFile(yesterdayPath, []byte("Yesterday's observation."), 0644); err != nil {
		t.Fatalf("writing yesterday's note: %v", err)
	}

	notes, err := ReadDailyNotes(dir, true)
	if err != nil {
		t.Fatalf("ReadDailyNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note (yesterday), got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Yesterday's observation") {
		t.Fatalf("expected yesterday's content, got: %q", notes[0].Content)
	}
}

func TestTokenCounter(t *testing.T) {
	counter := &CharHeuristicCounter{}

	tests := []struct {
		text string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		{"abcdefghi", 3},
		{"日本語", 1}, // 3 runes → 1 token
	}

	for _, tt := range tests {
		got := counter.Count(tt.text)
		if got != tt.want {
			t.Errorf("Count(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		text    string
		budget  int
		fromEnd bool
		want    string
	}{
		{"hello world", 100, false, "hello world"},
		{"hello world", 2, false, "hello wo"},
		{"hello world", 1, false, "hell"},
		{"hello world", 3, true, "hello world"},
		{"", 10, false, ""},
		{"hello world", 0, false, ""},
	}

	for _, tt := range tests {
		opts := TruncateOptions{Budget: tt.budget, FromStart: tt.fromEnd}
		got := Truncate(tt.text, opts)
		if got != tt.want {
			t.Errorf("Truncate(%q, budget=%d, fromStart=%v) = %q, want %q",
				tt.text, tt.budget, tt.fromEnd, got, tt.want)
		}
	}
}

func TestTruncateLines(t *testing.T) {
	text := "line 1\nline 2\nline 3\nline 4"

	// Budget of 2 tokens ≈ 8 chars. line 1 + newline + line 2 = ~14 chars, too much.
	// line 1 = ~6 chars, fits.
	opts := TruncateOptions{Budget: 2}
	got := TruncateLines(text, opts)
	if got == "" {
		t.Fatal("expected non-empty truncated text")
	}
	if len(got) > 16 { // ~4 tokens * 4 chars
		t.Fatalf("truncated text too long: %q", got)
	}
}

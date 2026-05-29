package audit

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

var testEvent = Event{
	Timestamp: time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	Level:     LevelInfo,
	Kind:      KindToolInvoked,
	AgentID:   "agent1",
	Fields:    map[string]any{"tool_id": "webfetch", "call_id": "abc"},
}

// --- ConsoleSink ---

func TestConsoleSink_Write(t *testing.T) {
	s := NewConsoleSink()
	if err := s.Write(testEvent); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
}

func TestConsoleSink_WriteNoFields(t *testing.T) {
	s := NewConsoleSink()
	err := s.Write(Event{
		Timestamp: time.Now(),
		Level:     LevelWarn,
		Kind:      KindPermissionDenied,
	})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
}

// --- SQLiteSink ---

func TestSQLiteSink_Write(t *testing.T) {
	f, err := os.CreateTemp("", "audit-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	s, err := NewSQLiteSink(f.Name())
	if err != nil {
		t.Fatalf("NewSQLiteSink: %v", err)
	}
	defer s.Close()

	if err := s.Write(testEvent); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify the row was written.
	db, err := sql.Open("sqlite", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var level, kind, agentID, fields string
	row := db.QueryRow(`SELECT level, kind, agent_id, fields FROM audit_events LIMIT 1`)
	if err := row.Scan(&level, &kind, &agentID, &fields); err != nil {
		t.Fatalf("scan row: %v", err)
	}
	if level != "info" {
		t.Errorf("level = %q, want info", level)
	}
	if kind != string(KindToolInvoked) {
		t.Errorf("kind = %q, want %s", kind, KindToolInvoked)
	}
	if agentID != "agent1" {
		t.Errorf("agent_id = %q, want agent1", agentID)
	}
	if !strings.Contains(fields, "webfetch") {
		t.Errorf("fields = %q, want it to contain 'webfetch'", fields)
	}
}

func TestSQLiteSink_MultipleWrites(t *testing.T) {
	f, err := os.CreateTemp("", "audit-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	s, err := NewSQLiteSink(f.Name())
	if err != nil {
		t.Fatalf("NewSQLiteSink: %v", err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		if err := s.Write(testEvent); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	db, err := sql.Open("sqlite", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM audit_events`).Scan(&count)
	if count != 5 {
		t.Errorf("expected 5 rows, got %d", count)
	}
}

func TestSQLiteSink_NilFields(t *testing.T) {
	f, err := os.CreateTemp("", "audit-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	s, err := NewSQLiteSink(f.Name())
	if err != nil {
		t.Fatalf("NewSQLiteSink: %v", err)
	}
	defer s.Close()

	err = s.Write(Event{
		Timestamp: time.Now(),
		Level:     LevelDebug,
		Kind:      KindAgentStarted,
		AgentID:   "agent2",
		Fields:    nil,
	})
	if err != nil {
		t.Fatalf("Write with nil fields: %v", err)
	}
}

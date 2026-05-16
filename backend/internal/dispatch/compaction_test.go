package dispatch

import (
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
)

func TestAssembledContextSize(t *testing.T) {
	tests := []struct {
		name     string
		assembled *memory.AssembledContext
		want     int
	}{
		{
			name: "sums all fields",
			assembled: &memory.AssembledContext{
				SystemPrompt: "system",
				Memory:       "memory",
				DailyNotes:   []string{"note1", "note2"},
				CrossRoomFeed: []string{"feed1"},
				CurrentRoomHistory: []string{"hist1", "hist2"},
				RAGResults:   []string{"rag1"},
				ToolSchemas:  []string{"tool1"},
				TurnBudget:   "budget",
				RFC:          "rfc",
			},
			// Should NOT double-count: CurrentRoomHistory should exclude the last message
			// because that message is included in RFC.
			want: len("system") + len("memory") + len("note1") + len("note2") +
				len("feed1") + len("hist1") + // only first history message
				len("rag1") + len("tool1") + len("budget") + len("rfc"),
		},
		{
			name: "single message history not double counted",
			assembled: &memory.AssembledContext{
				SystemPrompt: "system",
				CurrentRoomHistory: []string{"only_msg"},
				RFC:          "only_msg",
			},
			// CurrentRoomHistory should be empty since the only message is in RFC
			want: len("system") + len("only_msg"),
		},
		{
			name: "empty history with RFC only",
			assembled: &memory.AssembledContext{
				SystemPrompt: "system",
				CurrentRoomHistory: []string{},
				RFC:          "rfc_only",
			},
			want: len("system") + len("rfc_only"),
		},
		{
			name: "includes RAG results",
			assembled: &memory.AssembledContext{
				SystemPrompt: "system",
				RAGResults:   []string{"result1", "result2"},
				RFC:          "rfc",
			},
			want: len("system") + len("result1") + len("result2") + len("rfc"),
		},
		{
			name: "includes tool schemas",
			assembled: &memory.AssembledContext{
				SystemPrompt: "system",
				ToolSchemas:  []string{"schema1"},
				RFC:          "rfc",
			},
			want: len("system") + len("schema1") + len("rfc"),
		},
		{
			name: "includes turn budget",
			assembled: &memory.AssembledContext{
				SystemPrompt: "system",
				TurnBudget:   "turn budget notice",
				RFC:          "rfc",
			},
			want: len("system") + len("turn budget notice") + len("rfc"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assembledContextSize(tt.assembled)
			if got != tt.want {
				t.Errorf("assembledContextSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

package api

import "testing"

func TestClearedToReadDAG(t *testing.T) {
	tests := []struct {
		name          string
		viewer, agent int
		want          bool
	}{
		{"equal clearance reads", 5, 5, true},
		{"viewer above reads down", 5, 2, true},
		{"viewer below denied (no read-up)", 2, 5, false},
		{"viewer just below denied", 4, 5, false},
		{"zero-clearance agent always readable", 3, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clearedToReadDAG(tt.viewer, tt.agent); got != tt.want {
				t.Errorf("clearedToReadDAG(%d, %d) = %v, want %v", tt.viewer, tt.agent, got, tt.want)
			}
		})
	}
}

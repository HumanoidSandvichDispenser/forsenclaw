package config

import "testing"

func TestSystemMax(t *testing.T) {
	tests := []struct {
		name   string
		levels map[string]int
		want   int
	}{
		{"empty falls back to default", nil, DefaultSystemMax},
		{"all zero falls back to default", map[string]int{"public": 0}, DefaultSystemMax},
		{"max of values", map[string]int{"public": 0, "internal": 1, "confidential": 2, "restricted": 3}, 3},
		{"single positive", map[string]int{"secret": 7}, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ServerConfig{ClearanceLevels: tt.levels}
			if got := c.SystemMax(); got != tt.want {
				t.Errorf("SystemMax() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResolveClearanceName(t *testing.T) {
	c := &ServerConfig{ClearanceLevels: map[string]int{
		"public": 0, "internal": 1, "confidential": 2,
	}}
	tests := []struct {
		in      string
		wantVal int
		wantOK  bool
	}{
		{"confidential", 2, true},
		{"public", 0, true},
		{" internal ", 1, true}, // trimmed
		{"3", 3, true},          // plain integer
		{"0", 0, true},
		{"nonsense", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := c.ResolveClearanceName(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ResolveClearanceName(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
			if got != tt.wantVal {
				t.Errorf("ResolveClearanceName(%q) = %d, want %d", tt.in, got, tt.wantVal)
			}
		})
	}
}

func TestResolveClearanceName_NoLevels(t *testing.T) {
	c := &ServerConfig{}
	if v, ok := c.ResolveClearanceName("4"); !ok || v != 4 {
		t.Errorf("integer parse with no levels = (%d,%v), want (4,true)", v, ok)
	}
	if _, ok := c.ResolveClearanceName("confidential"); ok {
		t.Error("named level should not resolve when no levels configured")
	}
}

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewPaths_defaults(t *testing.T) {
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_DATA_HOME")
	os.Unsetenv("XDG_CACHE_HOME")

	p := NewPaths()
	home, _ := os.UserHomeDir()

	if p.ConfigRoot != filepath.Join(home, ".config", "hearth") {
		t.Errorf("ConfigRoot = %q, want %q", p.ConfigRoot, filepath.Join(home, ".config", "hearth"))
	}
	if p.DataRoot != filepath.Join(home, ".local", "share", "hearth") {
		t.Errorf("DataRoot = %q, want %q", p.DataRoot, filepath.Join(home, ".local", "share", "hearth"))
	}
	if p.CacheRoot != filepath.Join(home, ".cache", "hearth") {
		t.Errorf("CacheRoot = %q, want %q", p.CacheRoot, filepath.Join(home, ".cache", "hearth"))
	}
}

func TestNewPaths_envOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_DATA_HOME", "/tmp/data")
	t.Setenv("XDG_CACHE_HOME", "/tmp/cache")

	p := NewPaths()

	if p.ConfigRoot != "/tmp/cfg/hearth" {
		t.Errorf("ConfigRoot = %q, want /tmp/cfg/hearth", p.ConfigRoot)
	}
	if p.DataRoot != "/tmp/data/hearth" {
		t.Errorf("DataRoot = %q, want /tmp/data/hearth", p.DataRoot)
	}
	if p.CacheRoot != "/tmp/cache/hearth" {
		t.Errorf("CacheRoot = %q, want /tmp/cache/hearth", p.CacheRoot)
	}
}

func TestNewPathsFromRoots(t *testing.T) {
	p := NewPathsFromRoots("/a", "/b", "/c")

	if p.ConfigRoot != "/a" {
		t.Errorf("ConfigRoot = %q, want /a", p.ConfigRoot)
	}
	if p.DataRoot != "/b" {
		t.Errorf("DataRoot = %q, want /b", p.DataRoot)
	}
	if p.CacheRoot != "/c" {
		t.Errorf("CacheRoot = %q, want /c", p.CacheRoot)
	}
}

func TestAgentDirs(t *testing.T) {
	p := NewPathsFromRoots("/cfg", "/data", "/cache")

	if p.AgentConfigDir("housewife") != "/cfg/agents/housewife" {
		t.Errorf("AgentConfigDir = %q, want /cfg/agents/housewife", p.AgentConfigDir("housewife"))
	}
	if p.AgentDataDir("housewife") != "/data/agents/housewife" {
		t.Errorf("AgentDataDir = %q, want /data/agents/housewife", p.AgentDataDir("housewife"))
	}
}

func TestAllDirs(t *testing.T) {
	p := NewPathsFromRoots("/cfg", "/data", "/cache")
	dirs := p.AllDirs()

	expected := []string{
		"/cfg/agents",
		"/data/agents",
		"/data/rooms",
		"/data/staging/agents",
		"/data/db",
		"/cache",
		"/cache/search",
		"/cache/embeddings",
	}

	if len(dirs) != len(expected) {
		t.Fatalf("AllDirs returned %d dirs, want %d", len(dirs), len(expected))
	}
	for i, d := range expected {
		if dirs[i] != d {
			t.Errorf("AllDirs[%d] = %q, want %q", i, dirs[i], d)
		}
	}
}

func TestDBPaths(t *testing.T) {
	p := NewPathsFromRoots("/cfg", "/data", "/cache")

	if p.RoomsDBPath() != "/data/db/rooms.db" {
		t.Errorf("RoomsDBPath = %q, want /data/db/rooms.db", p.RoomsDBPath())
	}
	if p.AuditDBPath() != "/data/db/audit.db" {
		t.Errorf("AuditDBPath = %q, want /data/db/audit.db", p.AuditDBPath())
	}
}

func TestServerConfigFile(t *testing.T) {
	p := NewPathsFromRoots("/cfg", "/data", "/cache")

	if p.ServerConfigFile() != "/cfg/hearth.yaml" {
		t.Errorf("ServerConfigFile = %q, want /cfg/hearth.yaml", p.ServerConfigFile())
	}
}

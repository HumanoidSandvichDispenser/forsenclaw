package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

func TestBootstrap_createsDirs(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	cacheDir := t.TempDir()

	p := paths.NewPathsFromRoots(
		filepath.Join(cfgDir, "hearth"),
		filepath.Join(dataDir, "hearth"),
		filepath.Join(cacheDir, "hearth"),
	)

	if err := Bootstrap(p); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	for _, dir := range p.AllDirs() {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}
}

func TestBootstrap_idempotent(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	cacheDir := t.TempDir()

	p := paths.NewPathsFromRoots(
		filepath.Join(cfgDir, "hearth"),
		filepath.Join(dataDir, "hearth"),
		filepath.Join(cacheDir, "hearth"),
	)

	if err := Bootstrap(p); err != nil {
		t.Fatalf("first Bootstrap returned error: %v", err)
	}
	if err := Bootstrap(p); err != nil {
		t.Fatalf("second Bootstrap returned error: %v", err)
	}

	for _, dir := range p.AllDirs() {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}
}

func TestBootstrap_writesDefaultConfig(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	cacheDir := t.TempDir()

	p := paths.NewPathsFromRoots(
		filepath.Join(cfgDir, "hearth"),
		filepath.Join(dataDir, "hearth"),
		filepath.Join(cacheDir, "hearth"),
	)

	if err := Bootstrap(p); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	data, err := os.ReadFile(p.ServerConfigFile())
	if err != nil {
		t.Fatalf("reading default config: %v", err)
	}

	if len(data) == 0 {
		t.Error("default config is empty")
	}
}

func TestBootstrap_doesNotOverwriteConfig(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	cacheDir := t.TempDir()

	p := paths.NewPathsFromRoots(
		filepath.Join(cfgDir, "hearth"),
		filepath.Join(dataDir, "hearth"),
		filepath.Join(cacheDir, "hearth"),
	)

	if err := os.MkdirAll(filepath.Dir(p.ServerConfigFile()), 0755); err != nil {
		t.Fatal(err)
	}

	existingContent := []byte("listen: :9999\n")
	if err := os.WriteFile(p.ServerConfigFile(), existingContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := Bootstrap(p); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	data, err := os.ReadFile(p.ServerConfigFile())
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	if string(data) != string(existingContent) {
		t.Error("Bootstrap overwrote existing config file")
	}
}

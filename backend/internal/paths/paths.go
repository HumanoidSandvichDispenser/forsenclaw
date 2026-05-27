package paths

import (
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigRoot string
	DataRoot   string
	CacheRoot  string
}

func NewPaths() *Paths {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, _ := os.UserHomeDir()
		configHome = filepath.Join(home, ".config")
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}

	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, _ := os.UserHomeDir()
		cacheHome = filepath.Join(home, ".cache")
	}

	return &Paths{
		ConfigRoot: filepath.Join(configHome, "hearth"),
		DataRoot:   filepath.Join(dataHome, "hearth"),
		CacheRoot:  filepath.Join(cacheHome, "hearth"),
	}
}

func NewPathsFromRoots(config, data, cache string) *Paths {
	return &Paths{
		ConfigRoot: config,
		DataRoot:   data,
		CacheRoot:  cache,
	}
}

func (p *Paths) AgentConfigDir(name string) string {
	return filepath.Join(p.ConfigRoot, "agents", name)
}

func (p *Paths) AgentDataDir(name string) string {
	return filepath.Join(p.DataRoot, "agents", name)
}

func (p *Paths) AgentsConfigDir() string {
	return filepath.Join(p.ConfigRoot, "agents")
}

func (p *Paths) AgentsDataDir() string {
	return filepath.Join(p.DataRoot, "agents")
}

func (p *Paths) StagingDir() string {
	return filepath.Join(p.DataRoot, "staging")
}

func (p *Paths) StagingAgentsDir() string {
	return filepath.Join(p.DataRoot, "staging", "agents")
}

func (p *Paths) DBDir() string {
	return filepath.Join(p.DataRoot, "db")
}

func (p *Paths) ServerConfigFile() string {
	return filepath.Join(p.ConfigRoot, "hearth.yaml")
}

func (p *Paths) RoomsDBPath() string {
	return filepath.Join(p.DBDir(), "rooms.db")
}

func (p *Paths) AuditDBPath() string {
	return filepath.Join(p.DBDir(), "audit.db")
}

func (p *Paths) SearchCacheDir() string {
	return filepath.Join(p.CacheRoot, "search")
}

func (p *Paths) EmbeddingCacheDir() string {
	return filepath.Join(p.CacheRoot, "embeddings")
}

func (p *Paths) AllDirs() []string {
	return []string{
		p.AgentsConfigDir(),
		p.AgentsDataDir(),
		p.StagingAgentsDir(),
		p.DBDir(),
		p.CacheRoot,
		p.SearchCacheDir(),
		p.EmbeddingCacheDir(),
	}
}

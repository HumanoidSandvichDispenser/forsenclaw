package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/bootstrap"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

func main() {
	configFile := flag.String("config", "", "path to hearth.yaml (overrides default location)")
	validateOnly := flag.Bool("validate", false, "validate configuration and exit")
	flag.Parse()

	p := resolvePaths(*configFile)

	if err := bootstrap.Bootstrap(p); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	serverCfg, err := config.LoadServerConfig(p.ServerConfigFile())
	if err != nil {
		log.Fatalf("failed to load server config: %v", err)
	}

	agents, err := config.LoadAgents(p.AgentsConfigDir(), serverCfg)
	if err != nil {
		log.Fatalf("failed to load agents: %v", err)
	}

	log.Printf("loaded %d agent(s)", len(agents))
	for name, agent := range agents {
		perms, _ := agent.ParsedPermissions()
		log.Printf("  %s: clearance=%d, permissions=%d", name, agent.Clearance, len(perms))
	}

	if *validateOnly {
		fmt.Println("configuration valid")
		return
	}

	startServer(serverCfg)
}

func resolvePaths(configOverride string) *paths.Paths {
	cfgHome := os.Getenv("HEARTH_CONFIG_HOME")
	dataHome := os.Getenv("HEARTH_DATA_HOME")
	cacheHome := os.Getenv("HEARTH_CACHE_HOME")

	if cfgHome == "" {
		cfgHome = os.Getenv("XDG_CONFIG_HOME")
	}
	if dataHome == "" {
		dataHome = os.Getenv("XDG_DATA_HOME")
	}
	if cacheHome == "" {
		cacheHome = os.Getenv("XDG_CACHE_HOME")
	}

	if cfgHome != "" || dataHome != "" || cacheHome != "" {
		p := paths.NewPaths()
		if cfgHome != "" {
			p.ConfigRoot = cfgHome + "/hearth"
		}
		if dataHome != "" {
			p.DataRoot = dataHome + "/hearth"
		}
		if cacheHome != "" {
			p.CacheRoot = cacheHome + "/hearth"
		}
		return p
	}

	return paths.NewPaths()
}

func startServer(cfg *config.ServerConfig) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	log.Printf("Hearth starting on %s", cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

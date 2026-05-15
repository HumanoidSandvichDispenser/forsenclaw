package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/bootstrap"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/search"
)

func main() {
	if len(os.Args) < 2 {
		runServer(os.Args[1:])
		return
	}

	switch os.Args[1] {
	case "index":
		runIndex(os.Args[2:])
	case "server", "serve":
		runServer(os.Args[2:])
	default:
		// Default to server for backward compatibility
		runServer(os.Args[1:])
	}
}

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	configFile := fs.String("config", "", "path to hearth.yaml (overrides default location)")
	validateOnly := fs.Bool("validate", false, "validate configuration and exit")
	fs.Parse(args)

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

func runIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	rebuild := fs.Bool("rebuild", false, "rebuild search index from agent memory files")
	configFile := fs.String("config", "", "path to hearth.yaml (overrides default location)")
	fs.Parse(args)

	if !*rebuild {
		fmt.Fprintln(os.Stderr, "Usage: hearth index --rebuild")
		os.Exit(1)
	}

	p := resolvePaths(*configFile)

	if err := bootstrap.Bootstrap(p); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	serverCfg, err := config.LoadServerConfig(p.ServerConfigFile())
	if err != nil {
		log.Fatalf("failed to load server config: %v", err)
	}

	idx, err := search.NewSQLiteIndex(p.SearchCacheDir() + "/search.db")
	if err != nil {
		log.Fatalf("failed to open search index: %v", err)
	}
	defer idx.Close()

	embedder := search.NewOllamaEmbedder(serverCfg.Embeddings)
	rebuilder := search.NewRebuilder(idx, embedder, p)

	log.Println("Rebuilding search index...")
	if err := rebuilder.Rebuild(context.Background()); err != nil {
		log.Fatalf("rebuild failed: %v", err)
	}
	log.Println("Search index rebuilt successfully.")
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

	if configOverride != "" {
		cfgHome = configOverride
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

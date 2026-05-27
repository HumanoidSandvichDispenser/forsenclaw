package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/api"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/audit"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/bootstrap"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp"
	mcpTools "github.com/humanoidsandvichdispenser/hearth/backend/internal/mcp/tools"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/search"
	storedb "github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
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

	startServer(serverCfg, p)
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

func startServer(cfg *config.ServerConfig, p *paths.Paths) {
	// 1. Open rooms DB
	store, err := storedb.NewSQLiteStore(p.RoomsDBPath())
	if err != nil {
		log.Fatalf("failed to open rooms DB: %v", err)
	}
	defer store.Close()

	// 3. Create inference registry
	registry, err := inference.NewRegistry(cfg)
	if err != nil {
		log.Fatalf("failed to create inference registry: %v", err)
	}

	// 3b. Create built-in MCP registry.
	mcpRegistry, err := buildMCPRegistry(cfg)
	if err != nil {
		log.Fatalf("failed to create MCP registry: %v", err)
	}

	// 4. Create MCP executor
	mcpExecutor := mcp.NewExecutor(mcpRegistry, audit.Nop())

	// 5. Create WebSocket hub (before manager so we can pass the notifier adapter)
	hub := api.NewHub()
	go hub.Run()

	// 5b. Create assembler and agent manager
	assembler := memory.NewAssembler(p, 0, store, store)
	agentMgr, err := agent.NewManager(p, cfg, registry, assembler, mcpExecutor, api.NewHubNotifier(hub))
	if err != nil {
		log.Fatalf("failed to create agent manager: %v", err)
	}
	defer agentMgr.Close()

	// 7. Create dispatcher
	dispatcher := dispatch.NewDispatcher(agentMgr)

	// 8. Create service and API
	svc := api.NewService(dispatcher, store, store, agentMgr, hub)

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(api.AuthMiddleware()) // placeholder auth

	api.NewAPI(router, svc)

	// 10. Start server with graceful shutdown
	server := &http.Server{
		Addr:    cfg.Listen,
		Handler: router,
	}

	go func() {
		log.Printf("Hearth starting on %s", cfg.Listen)
		log.Printf("API docs:    http://%s/docs", cfg.Listen)
		log.Printf("OpenAPI:     http://%s/openapi.json", cfg.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	log.Println("server stopped")
}

func buildMCPRegistry(cfg *config.ServerConfig) (mcp.Registry, error) {
	// TODO: read systemMax from clearance_levels config when implemented.
	const systemMax = 5

	clearances := make(map[string]int)

	webfetchClearance := cfg.ResolveToolClearance(cfg.Tools.WebFetch.Clearance, systemMax)
	clients := []mcp.MCPClient{mcpTools.NewWebFetch(webfetchClearance)}
	clearances["webfetch"] = webfetchClearance

	if apiKey := cfg.Tools.BraveSearch.APIKey.Resolve(); apiKey != "" {
		braveClearance := cfg.ResolveToolClearance(cfg.Tools.BraveSearch.Clearance, systemMax)
		client, err := mcpTools.NewBraveSearch(apiKey, braveClearance)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
		clearances["web_search"] = braveClearance
	}

	return mcp.NewRegistry(clients, clearances), nil
}

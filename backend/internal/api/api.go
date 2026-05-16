package api

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// Service holds all dependencies for the API handlers.
type Service struct {
	dispatcher *dispatch.Dispatcher
	store      room.Store
	agentMgr   *agent.Manager
	hub        *Hub
	paths      *paths.Paths
}

// NewService creates a new API service with the given dependencies.
func NewService(
	d *dispatch.Dispatcher,
	s room.Store,
	m *agent.Manager,
	h *Hub,
	p *paths.Paths,
) *Service {
	return &Service{
		dispatcher: d,
		store:      s,
		agentMgr:   m,
		hub:        h,
		paths:      p,
	}
}

// NewAPI creates a Huma API instance and registers all routes.
func NewAPI(router chi.Router, svc *Service) huma.API {
	// Register WebSocket handler (raw, not through Huma)
	router.HandleFunc("/api/ws", svc.handleWebSocket)

	// Create Huma API
	api := humachi.New(router, huma.DefaultConfig("Hearth API", "1.0.0"))

	// Register REST operations
	registerRoomRoutes(api, svc)
	registerMessageRoutes(api, svc)
	registerAgentRoutes(api, svc)
	registerContextPreviewRoutes(api, svc)
	registerUserRoutes(api, svc)

	return api
}

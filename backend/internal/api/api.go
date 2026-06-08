package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
)

// Compactor compacts an agent's transcript in a room down to a target size.
// Satisfied by *memory.Compactor; may be nil (e.g. in tests), in which case the
// compact route reports the feature as unavailable.
type Compactor interface {
	Compact(ctx context.Context, ag *agent.Agent, roomID int64, target int, instructions string) error
	Stats(ctx context.Context, ag *agent.Agent, roomID int64) (memory.CompactionStats, error)
}

// MemoryReader reads an agent's memory + daily notes by clearance level.
// Satisfied by *memory.Inspector; may be nil (e.g. in tests).
type MemoryReader interface {
	Inspect(name string, maxClearance int, includeFlat bool) ([]memory.ClearanceLevel, error)
}

// Service holds all dependencies for the API handlers.
type Service struct {
	dispatcher *dispatch.Dispatcher
	rooms      store.RoomRepository
	messages   store.MessageRepository
	agentMgr   *agent.Manager
	assembler  agent.Assembler
	executor   agent.ToolExecutor
	compactor  Compactor
	memory     MemoryReader
	hub        *Hub
	user       room.Actor
}

// NewService creates a new API service with the given dependencies.
// executor may be nil (e.g. in tests); the context preview then assembles
// without tool definitions.
func NewService(
	d *dispatch.Dispatcher,
	rooms store.RoomRepository,
	messages store.MessageRepository,
	m *agent.Manager,
	assembler agent.Assembler,
	executor agent.ToolExecutor,
	compactor Compactor,
	memReader MemoryReader,
	h *Hub,
	user room.Actor,
) *Service {
	return &Service{
		dispatcher: d,
		rooms:      rooms,
		messages:   messages,
		agentMgr:   m,
		assembler:  assembler,
		executor:   executor,
		compactor:  compactor,
		memory:     memReader,
		hub:        h,
		user:       user,
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
	registerCompactRoutes(api, svc)
	registerBranchRoutes(api, svc)
	registerAgentRoutes(api, svc)
	registerAgentMemoryRoutes(api, svc)
	registerDAGRoutes(api, svc)
	registerConfirmationRoutes(api, svc)
	registerContextPreviewRoutes(api, svc)
	registerUserRoutes(api, svc)

	return api
}

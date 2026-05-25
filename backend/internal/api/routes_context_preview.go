package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// ---------------------------------------------------------------------------
// Context preview routes
// ---------------------------------------------------------------------------

func registerContextPreviewRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-context",
		Method:      http.MethodGet,
		Path:        "/api/rooms/{room_id}/agents/{agent_name}/context-preview",
		Summary:     "Preview agent context window",
		Tags:        []string{"Rooms", "Agents"},
	}, func(ctx context.Context, input *GetContextPreviewRequest) (*GetContextPreviewResponse, error) {
		return svc.previewContext(ctx, input)
	})
}

// ---------------------------------------------------------------------------
// Context preview handlers
// ---------------------------------------------------------------------------

func (svc *Service) previewContext(_ context.Context, _ *GetContextPreviewRequest) (*GetContextPreviewResponse, error) {
	// TODO: implement with new assembler
	return nil, huma.Error501NotImplemented("context preview not yet implemented")
}

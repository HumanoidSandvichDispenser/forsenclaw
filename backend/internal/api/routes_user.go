package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// ---------------------------------------------------------------------------
// User routes
// ---------------------------------------------------------------------------

func registerUserRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "get-me",
		Method:      http.MethodGet,
		Path:        "/api/me",
		Summary:     "Get current user",
		Tags:        []string{"User"},
	}, func(ctx context.Context, input *GetMeRequest) (*GetMeResponse, error) {
		return svc.getMe(ctx)
	})
}

// ---------------------------------------------------------------------------
// User handlers
// ---------------------------------------------------------------------------

func (svc *Service) getMe(ctx context.Context) (*GetMeResponse, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	resp := &GetMeResponse{}
	resp.Body = UserResponse{
		ID:   user.ID,
		Name: user.Name,
		Role: user.Role,
	}
	return resp, nil
}

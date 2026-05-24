package agent

import (
	"context"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dag"
)

// InferenceHandler handles a request node by running inference.
type InferenceHandler struct {
	req Request
}

func (h *InferenceHandler) Handle(_ context.Context, _ map[string]dag.Result) ([]dag.Dep, *dag.Result, error) {
	// TODO
	panic("not implemented")
}

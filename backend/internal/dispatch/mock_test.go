package dispatch

import (
	"context"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)


// mockProvider is a test provider that returns a pre-configured response.
type mockProvider struct {
	response string
	err      error
}

func newMockProvider(response string, err error) *mockProvider {
	return &mockProvider{response: response, err: err}
}

func (m *mockProvider) Infer(ctx context.Context, req inference.InferRequest) (<-chan inference.StreamingChunk, error) {
	if m.err != nil {
		return nil, m.err
	}

	ch := make(chan inference.StreamingChunk, 1)
	go func() {
		defer close(ch)
		ch <- inference.StreamingChunk{
			Content:      m.response,
			FinishReason: "stop",
		}
	}()
	return ch, nil
}

// mockRegistry is a test ModelResolver that returns a mock provider.
type mockRegistry struct {
	provider inference.Provider
	modelID  string
}

func newMockRegistry(provider inference.Provider, modelID string) *mockRegistry {
	return &mockRegistry{provider: provider, modelID: modelID}
}

func (m *mockRegistry) ResolveTier(agentDef *config.AgentDefinition, tier inference.ModelTier) (inference.Provider, string, error) {
	return m.provider, m.modelID, nil
}

package providers

import (
	"context"
	"os"
	"lato/internal/tools"
)

// LatoFreeModel maps a Lato Free model ID to its OpenRouter model ID.
type LatoFreeModel struct {
	LatoID       string // e.g., "lato-free/ling"
	OpenRouterID string // e.g., "inclusionai/ling-3.0-flash-fin:free"
	Name         string // Display name
}

// LatoFreeModels is the curated list of free models.
var LatoFreeModels = []LatoFreeModel{
	{
		LatoID:       "lato-free/ling",
		OpenRouterID: "inclusionai/ling-3.0-flash-fin:free",
		Name:         "Ling 3.0 Flash Fin Free",
	},
	{
		LatoID:       "lato-free/nemotron-ultra",
		OpenRouterID: "nvidia/nemotron-3-ultra-550b-a55b:free",
		Name:         "Nemotron 3 Ultra Free",
	},
	{
		LatoID:       "lato-free/minimax-m3",
		OpenRouterID: "minimax/minimax-m3:free",
		Name:         "MiniMax M3 Free",
	},
	{
		LatoID:       "lato-free/nemotron-super",
		OpenRouterID: "nvidia/nemotron-3-super-120b-a12b:free",
		Name:         "Nemotron 3 Super Free",
	},
	{
		LatoID:       "lato-free/minimax-m2.7",
		OpenRouterID: "minimax/minimax-m2.7:free",
		Name:         "MiniMax M2.7 Free",
	},
}

// latoFreeModelMap maps Lato IDs to OpenRouter IDs for quick lookup.
var latoFreeModelMap = func() map[string]string {
	m := make(map[string]string, len(LatoFreeModels))
	for _, model := range LatoFreeModels {
		m[model.LatoID] = model.OpenRouterID
	}
	return m
}()

// getLatoFreeAPIKey returns the API key for Lato Free.
// Priority: 1) built-in credential (release builds), 2) environment variable (development).
func getLatoFreeAPIKey() string {
	// First priority: built-in credential (set at build time for releases)
	if LatoFreeBuiltinCredential != "" {
		return LatoFreeBuiltinCredential
	}
	// Second priority: environment variable (development/testing)
	return os.Getenv("LATO_FREE_OPENROUTER_API_KEY")
}

// GetLatoFreeBuiltinCredential is exported so runtime can check for built-in credential.
func GetLatoFreeBuiltinCredential() string {
	return LatoFreeBuiltinCredential
}

// LatoFreeProvider wraps the OpenAI-compatible client for Lato Free.
type LatoFreeProvider struct {
	*NvidiaProvider // embed the OpenAI-compatible client
}

// NewLatoFreeProvider creates a new Lato Free provider.
func NewLatoFreeProvider(model string) *LatoFreeProvider {
	apiKey := getLatoFreeAPIKey()
	// Use the OpenRouter endpoint with the Lato Free API key
	p := NewOpenAICompatible("https://openrouter.ai/api/v1", model, apiKey, nil)
	return &LatoFreeProvider{NvidiaProvider: p}
}

// ListModels returns the curated list of Lato Free models.
// It does NOT call the upstream /models endpoint.
func (l *LatoFreeProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	models := make([]ModelInfo, 0, len(LatoFreeModels))
	for _, m := range LatoFreeModels {
		models = append(models, ModelInfo{Name: m.Name, ID: m.LatoID})
	}
	return models, nil
}

// StreamChat translates the Lato model ID to the OpenRouter model ID
// and delegates to the embedded OpenAI-compatible client.
func (l *LatoFreeProvider) StreamChat(ctx context.Context, messages []Message, tools []tools.Definition) (<-chan StreamEvent, error) {
	// Translate the model ID if it's a Lato Free model
	if openRouterID, ok := latoFreeModelMap[l.Model]; ok {
		l.NvidiaProvider.Model = openRouterID
	}
	return l.NvidiaProvider.StreamChat(ctx, messages, tools)
}
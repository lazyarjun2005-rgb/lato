package providers

// ModelInfo describes one selectable model: the friendly name shown in
// the UI and the real model ID sent to the provider's API. The UI must
// only ever display Name; ID is what actually gets stored in config and
// sent over the wire.
type ModelInfo struct {
	Name        string
	ID          string
	Description string
}

// Provider classes select which concrete ModelProvider implementation
// talks to a provider.
const (
	// ClassOllama uses Lato's native Ollama client (NDJSON /api/chat).
	ClassOllama = "ollama"
	// ClassOpenAICompatible uses the standard OpenAI chat-completions
	// client shared by every OpenAI-shaped API (NVIDIA NIM, LM Studio,
	// OpenRouter, 9Router, OmniRoute, ...).
	ClassOpenAICompatible = "openai-compatible"
)

// ProviderInfo describes one selectable provider: how it is presented in
// the UI, which implementation speaks for it, where to reach it by
// default, and which environment variable holds its API key.
type ProviderInfo struct {
	Name        string
	ID          string
	Description string
	// Endpoint is the default endpoint used when switching to this
	// provider, so picking a provider doesn't require also knowing its
	// URL. Users can override it per provider via model.endpoint in
	// config.yaml.
	Endpoint string
	Models   []ModelInfo

	// Class selects the concrete provider implementation.
	Class string

	// APIKeyEnv names the environment variable that holds this
	// provider's API key. Empty means the provider needs no key
	// (local services such as Ollama and LM Studio). Keys are only
	// ever read from the environment — never stored on disk or
	// written into source files.
	APIKeyEnv string
}

// RequiresAPIKey reports whether this provider needs an API key.
func (p ProviderInfo) RequiresAPIKey() bool { return p.APIKeyEnv != "" }

// Registry lists every provider Lato knows how to talk to, in the
// order they're presented in the /provider picker. Adding a provider
// means editing this list only — nothing else hardcodes provider names.
// Models are deliberately not listed here: each provider queries its
// live endpoint for them (see ModelProvider.ListModels), so the /model
// picker always shows reality instead of a baked-in list.
//
// Lato remains local-first: Ollama works fully offline, and online
// providers are only contacted after the user explicitly switches to
// them (/provider openrouter, /provider 9router, ...).
var Registry = []ProviderInfo{
	{
		Name:        "Ollama",
		ID:          "ollama",
		Class:       ClassOllama,
		Description: "Local models served by Ollama. Fully offline.",
		Endpoint:    "http://localhost:11434",
	},
	{
		Name:        "LM Studio",
		ID:          "lmstudio",
		Class:       ClassOpenAICompatible,
		Description: "Local models served by LM Studio.",
		Endpoint:    "http://localhost:1234/v1",
	},
	{
		Name:        "NVIDIA NIM",
		ID:          "nvidia",
		Class:       ClassOpenAICompatible,
		Description: "Hosted models served by NVIDIA NIM.",
		Endpoint:    "https://integrate.api.nvidia.com/v1",
		APIKeyEnv:   "NVIDIA_API_KEY",
	},
	{
		Name:        "OpenRouter",
		ID:          "openrouter",
		Class:       ClassOpenAICompatible,
		Description: "Hosted models routed via OpenRouter.",
		Endpoint:    "https://openrouter.ai/api/v1",
		APIKeyEnv:   "OPENROUTER_API_KEY",
	},
	{
		Name:        "9Router",
		ID:          "9router",
		Class:       ClassOpenAICompatible,
		Description: "Models via 9Router (local or cloud endpoint).",
		Endpoint:    "http://localhost:20128/v1",
		APIKeyEnv:   "NINEROUTER_KEY",
	},
	{
		Name:        "OmniRoute",
		ID:          "omniroute",
		Class:       ClassOpenAICompatible,
		Description: "Models via OmniRoute. Set model.endpoint for your installation.",
		Endpoint:    "http://localhost:8787/v1",
		APIKeyEnv:   "OMNIROUTE_KEY",
	},
}

// ByID looks up a provider by its ID (e.g. "openrouter"). ok is false if
// no provider with that ID is registered.
func ByID(id string) (ProviderInfo, bool) {
	for _, p := range Registry {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

// DisplayName returns the friendly name for a provider ID, falling back
// to the raw ID if it isn't registered (e.g. one set by hand in
// config.yaml).
func DisplayName(providerID string) string {
	if p, ok := ByID(providerID); ok {
		return p.Name
	}
	return providerID
}

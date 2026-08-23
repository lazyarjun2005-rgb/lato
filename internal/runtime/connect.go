// Provider connection support: the runtime resolves provider
// credentials through a deterministic precedence (saved /connect
// configuration → environment variables → defaults), validates new
// connections with a lightweight model discovery call, and caches
// discovered models for the grouped /model picker.
package runtime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"lato/internal/providers"
	"lato/internal/userconfig"
)

// validateTimeout bounds how long connecting or refreshing may probe an
// endpoint before giving up.
const validateTimeout = 10 * time.Second

// connectionSource returns the loaded connection store. Loading is lazy
// so tests can construct bare runtimes, and re-reading is cheap enough
// that external edits stay visible.
func (r *Runtime) connectionSource() *userconfig.Store {
	s, err := userconfig.Load()
	if err != nil {
		// A corrupt file must not brick Lato: behave as unconfigured.
		return nil
	}
	return s
}

// Connection reports one saved provider connection.
func (r *Runtime) Connection(id string) (userconfig.Connection, bool) {
	return r.connectionSource().Get(id)
}

// Connections lists every configured provider, ordered by ID.
func (r *Runtime) Connections() []userconfig.Connection {
	return r.connectionSource().List()
}

// IsConfigured reports whether switching to id can succeed right now:
// any saved /connect configuration counts (local installs may run
// without keys), local providers are always usable offline, and hosted
// providers otherwise need their registry environment variable set.
func (r *Runtime) IsConfigured(id string) bool {
	if _, ok := r.Connection(id); ok {
		return true
	}
	if !requiresKey(id) {
		return true
	}
	info, ok := providers.ByID(id)
	if !ok || info.APIKeyEnv == "" {
		return false
	}
	return os.Getenv(info.APIKeyEnv) != ""
}

func requiresKey(id string) bool {
	info, ok := providers.ByID(id)
	return ok && info.RequiresAPIKey()
}

// ValidateConnection probes an OpenAI-compatible (or Ollama) endpoint
// with a model discovery request and returns its models. The API key is
// never included in returned errors.
func (r *Runtime) ValidateConnection(class, endpoint, apiKey string) ([]userconfig.Model, error) {
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()

	client := &http.Client{Timeout: validateTimeout}

	var (
		models []providers.ModelInfo
		err    error
	)
	switch class {
	case providers.ClassOllama:
		p := providers.NewOllamaProvider(endpoint, "validation")
		models, err = p.ListModels(ctx)
	default:
		p := providers.NewOpenAICompatible(endpoint, "validation", apiKey, client)
		models, err = p.ListModels(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("connection failed: %s", userconfig.Sanitize(err.Error(), apiKey))
	}

	out := make([]userconfig.Model, 0, len(models))
	for _, m := range models {
		out = append(out, userconfig.Model{ID: m.ID, Name: m.Name})
	}
	return out, nil
}

// finalizeConnection fills derived fields before persisting: custom
// providers without an explicit ID get a slug of their name, endpoints
// are normalized without trailing slashes, and a registered provider
// left without an endpoint falls back to its registry default (so a
// key-only flow like OpenRouter's can never validate against a blank
// URL).
func finalizeConnection(conn userconfig.Connection) userconfig.Connection {
	if conn.ID == "" {
		conn.ID = userconfig.CustomNameToID(conn.Name)
	}
	conn.Endpoint = strings.TrimRight(conn.Endpoint, "/")
	if conn.Endpoint == "" {
		if info, ok := providers.ByID(conn.ID); ok {
			conn.Endpoint = info.Endpoint
		}
	}
	return conn
}

// ConnectProvider validates a candidate connection and, only on
// success, saves it together with its discovered model list.
func (r *Runtime) ConnectProvider(conn userconfig.Connection) (int, error) {
	conn = finalizeConnection(conn)
	models, err := r.ValidateConnection(conn.Class, conn.Endpoint, conn.APIKey)
	if err != nil {
		return 0, err
	}
	conn.Models = models

	store, err := userconfig.Load()
	if err != nil {
		return 0, fmt.Errorf("load provider connections: %w", err)
	}
	if err := store.Put(conn); err != nil {
		return 0, fmt.Errorf("save provider connection: %w", err)
	}
	return len(models), nil
}

// SaveUnvalidatedConnection persists a connection without probing its
// endpoint. Used by /connect for custom providers whose discovery
// failed but whose details the user wants to keep.
func (r *Runtime) SaveUnvalidatedConnection(conn userconfig.Connection) error {
	conn = finalizeConnection(conn)
	store, err := userconfig.Load()
	if err != nil {
		return fmt.Errorf("load provider connections: %w", err)
	}
	if err := store.Put(conn); err != nil {
		return fmt.Errorf("save provider connection: %w", err)
	}
	return nil
}

// DetectImportCandidates gathers provider connections found in known
// OpenCode and Claude Code configurations. Detection only reads files;
// nothing is saved or executed until the user confirms in /connect
// import (or /import).
func (r *Runtime) DetectImportCandidates() []userconfig.Connection {
	var out []userconfig.Connection
	out = append(out, userconfig.DetectOpenCodeConfigs()...)
	if conn, ok, _ := userconfig.DetectClaudeGateway(); ok {
		out = append(out, conn)
	}
	return out
}

// RefreshConnectionModels re-runs discovery for every configured
// provider, updating cached lists. Manually registered models are
// preserved; failures are collected per provider and never abort the
// remaining refreshes.
func (r *Runtime) RefreshConnectionModels() (refreshed int, problems []string) {
	for _, conn := range r.Connections() {
		models, err := r.ValidateConnection(conn.Class, conn.Endpoint, conn.APIKey)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", conn.Name, err))
			continue
		}
		if store, loadErr := userconfig.Load(); loadErr == nil {
			if err := store.MergeDiscoveredModels(conn.ID, models); err == nil {
				refreshed++
				continue
			}
		}
		problems = append(problems, fmt.Sprintf("%s: could not save model cache", conn.Name))
	}
	return refreshed, problems
}

// RegisterModel stores a user-supplied model ID under an already
// configured provider (e.g. a dashboard-only model on 9Router). The ID
// travels to the provider verbatim: it is never parsed or rewritten.
// Whether the provider actually serves the model is only learned when
// it is used; such failures surface as normal provider errors and never
// remove the registration.
func (r *Runtime) RegisterModel(providerID, modelID, displayName string) error {
	store, err := userconfig.Load()
	if err != nil {
		return fmt.Errorf("load provider connections: %w", err)
	}
	return store.AddCustomModel(providerID, modelID, displayName)
}

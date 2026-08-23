package runtime

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/config"
	"lato/internal/effort"
	"lato/internal/providers"
	"lato/internal/userconfig"
	"lato/internal/workspace"
)

// isolateUserConfig points every user-level path at a temp directory so
// tests never read or write the developer's real configuration.
func isolateUserConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	return dir
}

func newConnectedRuntime(t *testing.T) (*Runtime, *scriptedProvider) {
	t.Helper()
	isolateUserConfig(t)
	p := &scriptedProvider{turns: [][]providers.StreamEvent{{{Text: "ok"}, {Done: true}}}}
	rt := newTestRuntime(p)
	rt.workspace = workspace.DiscoverDir(t.TempDir())
	return rt, p
}

func TestConnectProviderValidatesAndSaves(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"vendor/model:variant"},{"id":"other/model"}]}`))
	}))
	defer server.Close()

	models, err := rt.ConnectProvider(userconfig.Connection{
		ID: "openrouter", Name: "OpenRouter", Class: providers.ClassOpenAICompatible,
		Endpoint: server.URL, APIKey: "sk-connect",
	})
	if err != nil {
		t.Fatalf("ConnectProvider() error = %v", err)
	}
	if models != 2 {
		t.Errorf("discovered %d models, want 2", models)
	}
	if auth != "Bearer sk-connect" {
		t.Errorf("Authorization = %q, want bearer key", auth)
	}

	saved, ok := rt.Connection("openrouter")
	if !ok || saved.APIKey != "sk-connect" || len(saved.Models) != 2 {
		t.Errorf("saved connection = %+v", saved)
	}
}

func TestConnectProviderInvalidKeyFailsWithoutLeaking(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad key zz-insecure-zz"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := rt.ConnectProvider(userconfig.Connection{
		ID: "openrouter", Name: "OpenRouter", Class: providers.ClassOpenAICompatible,
		Endpoint: server.URL, APIKey: "zz-insecure-zz",
	})
	if err == nil {
		t.Fatal("expected validation failure for a rejected key")
	}
	msg := err.Error()
	if !strings.Contains(msg, "401") {
		t.Errorf("error %q should mention the HTTP status", msg)
	}
	if strings.Contains(msg, "zz-insecure-zz") {
		t.Error("error message leaked the API key")
	}
	if _, ok := rt.Connection("openrouter"); ok {
		t.Error("failed connection must not be saved")
	}
}

func TestConnectProviderUnreachableEndpoint(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	_, err := rt.ValidateConnection(providers.ClassOpenAICompatible, url, "")
	if err == nil || !strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("ValidateConnection() error = %v, want sanitized connection failure", err)
	}
}

func TestCustomProviderManualSaveAndConstruction(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	conn := userconfig.Connection{
		Name: "My Provider!", Custom: true, Class: providers.ClassOpenAICompatible,
		Endpoint: "http://localhost:1234/v1/",
		Models:   []userconfig.Model{{ID: "my-model", Name: "my-model"}},
	}
	if err := rt.SaveUnvalidatedConnection(conn); err != nil {
		t.Fatalf("SaveUnvalidatedConnection() error = %v", err)
	}

	got, ok := rt.Connection("my-provider")
	if !ok {
		t.Fatal("custom connection not saved under slug ID")
	}
	if got.ID != "my-provider" || got.Endpoint != "http://localhost:1234/v1" {
		t.Errorf("connection = %+v (trailing slash must be trimmed by save path)", got)
	}

	cfg := &config.Config{Model: config.Model{
		Provider: "my-provider", Endpoint: "http://localhost:1234/v1", Name: "my-model",
	}}
	p, err := rt.newProvider(cfg, effort.Medium)
	if err != nil {
		t.Fatalf("newProvider(custom) error = %v", err)
	}
	if _, isCompat := p.(*providers.NvidiaProvider); !isCompat {
		t.Errorf("custom provider built %T, want shared OpenAI-compatible implementation", p)
	}
}

// TestSavedConnectionOverridesEnvironment pins precedence level 1 over
// level 3: a /connect key wins even when the env var is also set.
func TestSavedConnectionOverridesEnvironment(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer saved-key" {
			t.Errorf("Authorization = %q, want the saved key", got)
		}
		w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer server.Close()

	store, _ := userconfig.Load()
	store.Put(userconfig.Connection{
		ID: "openrouter", Endpoint: server.URL, APIKey: "saved-key",
		Class: providers.ClassOpenAICompatible,
	})

	p, err := rt.newProvider(&config.Config{Model: config.Model{
		Provider: "openrouter", Endpoint: "https://openrouter.ai/api/v1",
		Name: "m", APIKey: "env-key",
	}}, effort.Medium)
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}
	compat := p.(*providers.NvidiaProvider)
	if compat.Endpoint != server.URL || compat.APIKey != "saved-key" {
		t.Errorf("provider endpoint/key = %s/%s, want saved values", compat.Endpoint, compat.APIKey)
	}
}

func TestIsConfigured(t *testing.T) {
	rt, _ := newTestRuntimeIsolated(t)
	isolateUserConfig(t)

	if !rt.IsConfigured("ollama") {
		t.Error("local providers are always configured (offline-first)")
	}
	if rt.IsConfigured("openrouter") {
		t.Error("hosted provider without key or connection must not be configured")
	}
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	if !rt.IsConfigured("openrouter") {
		t.Error("environment fallback should satisfy configuration")
	}
}

func TestRefreshConnectionModelsReportsProblems(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"fresh/model"}]}`))
	}))
	defer live.Close()

	store, _ := userconfig.Load()
	store.Put(userconfig.Connection{
		ID: "9router", Name: "9Router", Endpoint: live.URL,
		Class: providers.ClassOpenAICompatible,
	})
	store.Put(userconfig.Connection{
		ID: "omniroute", Name: "OmniRoute", Endpoint: "http://127.0.0.1:1/v1",
		Class: providers.ClassOpenAICompatible,
	})

	refreshed, problems := rt.RefreshConnectionModels()
	if refreshed != 1 || len(problems) != 1 {
		t.Fatalf("refreshed = %d problems = %v, want 1 and 1", refreshed, problems)
	}
	saved, _ := rt.Connection("9router")
	if len(saved.Models) != 1 || saved.Models[0].ID != "fresh/model" {
		t.Errorf("cached models not updated: %+v", saved.Models)
	}
}

// TestFinalizeBackfillsRegisteredEndpoints pins the save-path safety
// net: no registered provider may ever reach validation with a blank
// endpoint (the manual-test failure produced `Get "/models":
// unsupported protocol scheme ""`).
func TestFinalizeBackfillsRegisteredEndpoints(t *testing.T) {
	for _, id := range []string{"openrouter", "nvidia", "9router", "omniroute", "ollama"} {
		info, ok := providers.ByID(id)
		if !ok {
			t.Fatalf("provider %s missing from registry", id)
		}
		got := finalizeConnection(userconfig.Connection{ID: id})
		if got.Endpoint == "" {
			t.Errorf("finalizeConnection(%s) left the endpoint empty", id)
		}
		if got.Endpoint != info.Endpoint {
			t.Errorf("finalizeConnection(%s) = %q, want registry default %q", id, got.Endpoint, info.Endpoint)
		}
	}
}

func newTestRuntimeIsolated(t *testing.T) (*Runtime, *scriptedProvider) {
	t.Helper()
	p := &scriptedProvider{turns: [][]providers.StreamEvent{{{Text: "ok"}, {Done: true}}}}
	return newTestRuntime(p), p
}

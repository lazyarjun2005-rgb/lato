package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lato/internal/config"
	"lato/internal/providers"
	"lato/internal/userconfig"
)

// TestRegisterModelOn9Router covers the milestone's headline flow:
// register a dashboard-only model ID under the connected 9Router and
// find it in the provider's model list afterwards.
func TestRegisterModelOn9Router(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	store, _ := userconfig.Load()
	store.Put(userconfig.Connection{
		ID: "9router", Name: "9Router", Class: providers.ClassOpenAICompatible,
		Endpoint: "http://localhost:20128/v1", APIKey: "sk-nine",
		Models: []userconfig.Model{{ID: "kr/auto"}, {ID: "kr/glm-5"}},
	})

	if err := rt.RegisterModel("9router", "kr/qwen3-coder-next", "Qwen3 Coder Next"); err != nil {
		t.Fatalf("RegisterModel() error = %v", err)
	}

	conn, _ := rt.Connection("9router")
	found := false
	for _, m := range conn.Models {
		if m.ID == "kr/qwen3-coder-next" {
			found = true
			if !m.Custom || m.Name != "Qwen3 Coder Next" {
				t.Errorf("model = %+v", m)
			}
		}
	}
	if !found {
		t.Fatalf("custom model not registered: %+v", conn.Models)
	}
}

func TestRegisterModelRequiresConfiguredProvider(t *testing.T) {
	rt, _ := newConnectedRuntime(t)
	err := rt.RegisterModel("openrouter", "vendor/model", "")
	if err == nil || !strings.Contains(err.Error(), "/connect") {
		t.Fatalf("RegisterModel(unconfigured) error = %v, want /connect guidance", err)
	}
}

// TestRegisterModelOpaqueIDsWithSpecialCharacters verifies no
// transformation of unusual IDs across save and reload.
func TestRegisterModelOpaqueIDsWithSpecialCharacters(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	store, _ := userconfig.Load()
	store.Put(userconfig.Connection{ID: "9router", Name: "9Router", Class: providers.ClassOpenAICompatible})

	for _, id := range []string{"provider/model", "vendor/model:variant", "model.with.special/characters"} {
		if err := rt.RegisterModel("9router", id, ""); err != nil {
			t.Fatalf("RegisterModel(%q) error = %v", id, err)
		}
	}
	// Connection re-reads the store file from disk, so this asserts
	// verbatim persistence across a reload.
	conn, _ := rt.Connection("9router")
	var got []string
	for _, m := range conn.Models {
		got = append(got, m.ID)
	}
	want := "provider/model|vendor/model:variant|model.with.special/characters"
	if strings.Join(got, "|") != want {
		t.Errorf("stored IDs = %q, want %q (verbatim)", strings.Join(got, "|"), want)
	}
}

// TestRefreshPreservesCustomModels pins requirement 15: refreshing a
// provider's live list must not delete manually registered models.
func TestRefreshPreservesCustomModels(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"kr/auto"},{"id":"kr/new-from-server"}]}`))
	}))
	defer live.Close()

	store, _ := userconfig.Load()
	store.Put(userconfig.Connection{
		ID: "9router", Name: "9Router", Class: providers.ClassOpenAICompatible,
		Endpoint: live.URL,
	})
	rt.RegisterModel("9router", "kr/qwen3-coder-next", "")

	refreshed, problems := rt.RefreshConnectionModels()
	if refreshed != 1 || len(problems) != 0 {
		t.Fatalf("refreshed = %d problems = %v", refreshed, problems)
	}

	conn, _ := rt.Connection("9router")
	ids := map[string]bool{}
	for _, m := range conn.Models {
		ids[m.ID] = true
	}
	if !ids["kr/auto"] || !ids["kr/new-from-server"] {
		t.Errorf("discovered models lost: %v", ids)
	}
	if !ids["kr/qwen3-coder-next"] {
		t.Errorf("/model refresh deleted the custom model: %v", ids)
	}
}

// TestCustomModelIDSentVerbatim proves selection sends the exact opaque
// ID to the OpenAI-compatible API.
func TestCustomModelIDSentVerbatim(t *testing.T) {
	rt, _ := newConnectedRuntime(t)

	var requestedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		requestedModel = body.Model

		w.Header().Set("Content-Type", "text/event-stream")
		sseLine(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	rt.provider = providers.NewOpenAICompatible(server.URL, "placeholder-model", "", nil)
	// SetModel persists config and rebuilds the provider; give the bare
	// test runtime a real one plus the 9Router connection a real user
	// would have made via /connect (HOME is already isolated).
	store, _ := userconfig.Load()
	store.Put(userconfig.Connection{
		ID: "9router", Name: "9Router", Class: providers.ClassOpenAICompatible,
		Endpoint: server.URL, // connection config wins over config.yaml
	})
	rt.cfg = &config.Config{Model: config.Model{
		Provider: "9router", Endpoint: "http://localhost:20128/v1", Name: "kr/auto",
	}}

	if err := rt.SetModel("kr/qwen3-coder-next"); err != nil {
		t.Fatalf("SetModel() error = %v", err)
	}
	response, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "hi"}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Content != "ok" {
		t.Fatalf("response = %q", response.Content)
	}
	if requestedModel != "kr/qwen3-coder-next" {
		t.Errorf("wire model ID = %q, want kr/qwen3-coder-next verbatim", requestedModel)
	}
}

// TestCustomModelRegistrationNoKeyLeakage checks that registration
// errors never contain a stored key.
func TestCustomModelRegistrationNoKeyLeakage(t *testing.T) {
	rt, _ := newConnectedRuntime(t)
	err := rt.RegisterModel("not-configured", "m/1", "")
	if err == nil {
		t.Fatal("expected error for unconfigured provider")
	}
	if strings.Contains(err.Error(), "sk-") {
		t.Errorf("error message mentions a key: %q", err)
	}
}

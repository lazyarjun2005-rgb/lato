package userconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

func newStoreWith9Router(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.json")
	s, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	conn := Connection{
		ID: "9router", Name: "9Router", Class: "openai-compatible",
		Endpoint: "http://localhost:20128/v1", APIKey: "sk-store-secret",
		Models: []Model{{ID: "kr/auto"}, {ID: "kr/glm-5"}},
	}
	if err := s.Put(conn); err != nil {
		t.Fatal(err)
	}
	return s, path
}

// TestAddCustomModelPersistsAcrossReload covers the restart requirement:
// a registered model must still be in the store after reloading it from
// disk.
func TestAddCustomModelPersistsAcrossReload(t *testing.T) {
	s, path := newStoreWith9Router(t)

	if err := s.AddCustomModel("9router", "kr/qwen3-coder-next", "Qwen3 Coder Next"); err != nil {
		t.Fatalf("AddCustomModel() error = %v", err)
	}

	reloaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	conn, _ := reloaded.Get("9router")

	var found *Model
	for i := range conn.Models {
		if conn.Models[i].ID == "kr/qwen3-coder-next" {
			found = &conn.Models[i]
		}
	}
	if found == nil {
		t.Fatalf("custom model missing after reload: %+v", conn.Models)
	}
	if !found.Custom || found.Name != "Qwen3 Coder Next" {
		t.Errorf("custom model = %+v", *found)
	}
}

// TestCustomModelIDStaysVerbatim pins opacity for awkward IDs.
func TestCustomModelIDStaysVerbatim(t *testing.T) {
	cases := []string{
		"kr/qwen3-coder-next",
		"provider/model",
		"vendor/model:variant",
		"model.with.special/characters",
		"  kr/spaced  ", // surrounding whitespace is trimmed, inner text untouched
	}
	for _, id := range cases {
		s, _ := newStoreWith9Router(t)
		want := strings.TrimSpace(id)
		if err := s.AddCustomModel("9router", id, ""); err != nil {
			t.Fatalf("AddCustomModel(%q) error = %v", id, err)
		}
		conn, _ := s.Get("9router")
		last := conn.Models[len(conn.Models)-1]
		if last.ID != want || last.Name != want {
			t.Errorf("model = {%q %q}, want ID/name %q (default name = ID)", last.ID, last.Name, want)
		}
	}
}

func TestAddCustomModelDuplicateRejected(t *testing.T) {
	s, _ := newStoreWith9Router(t)
	if err := s.AddCustomModel("9router", "kr/auto", ""); err == nil {
		t.Fatal("duplicate of a discovered model must be rejected")
	}
	s.AddCustomModel("9router", "kr/custom-x", "")
	if err := s.AddCustomModel("9router", "kr/custom-x", ""); err == nil {
		t.Fatal("duplicate custom model must be rejected")
	}
	conn, _ := s.Get("9router")
	if n := len(conn.Models); n != 3 { // 2 discovered + 1 custom
		t.Errorf("models = %d entries, want 3 (no duplicates)", n)
	}
}

func TestAddCustomModelRequiresConfiguredProvider(t *testing.T) {
	s := &Store{}
	if err := s.AddCustomModel("unknown-provider", "m/1", ""); err == nil ||
		!strings.Contains(err.Error(), "/connect") {
		t.Fatalf("error = %v, want not-configured guidance", err)
	}
	if err := s.AddCustomModel("9router", "", ""); err == nil {
		t.Fatal("empty model ID must be rejected")
	}
}

func TestMultipleCustomModelsPerProvider(t *testing.T) {
	s, _ := newStoreWith9Router(t)
	for _, id := range []string{"kr/deepseek-3.2", "kr/qwen3-coder-next-agentic", "glm/turbo"} {
		if err := s.AddCustomModel("9router", id, ""); err != nil {
			t.Fatalf("AddCustomModel(%q) error = %v", id, err)
		}
	}
	conn, _ := s.Get("9router")
	customs := 0
	for _, m := range conn.Models {
		if m.Custom {
			customs++
		}
	}
	if customs != 3 {
		t.Errorf("custom count = %d, want 3", customs)
	}
}

func TestAddCustomModelOpenRouter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	s, _ := LoadFrom(path)
	s.Put(Connection{ID: "openrouter", Name: "OpenRouter", Class: "openai-compatible",
		Endpoint: "https://openrouter.ai/api/v1", APIKey: "sk-or"})
	if err := s.AddCustomModel("openrouter", "vendor/dashboard-only:variant", "Dashboard Only"); err != nil {
		t.Fatalf("AddCustomModel() error = %v", err)
	}
	conn, _ := s.Get("openrouter")
	if last := conn.Models[len(conn.Models)-1]; last.ID != "vendor/dashboard-only:variant" || !last.Custom {
		t.Errorf("openrouter custom model = %+v", last)
	}
}

// TestMergeDiscoveredModelsPreservesCustoms covers /model refresh:
// refreshing replaces discovery results but never deletes manual models,
// and IDs that moved from custom to discovered are not duplicated.
func TestMergeDiscoveredModelsPreservesCustoms(t *testing.T) {
	s, _ := newStoreWith9Router(t)
	s.AddCustomModel("9router", "kr/qwen3-coder-next", "")

	discovered := []Model{
		{ID: "kr/auto"}, {ID: "kr/glm-5"}, // same as before
		{ID: "kr/brand-new"},        // newly added server-side
		{ID: "kr/qwen3-coder-next"}, // dashboard model now also in /models
	}
	if err := s.MergeDiscoveredModels("9router", discovered); err != nil {
		t.Fatalf("MergeDiscoveredModels() error = %v", err)
	}

	conn, _ := s.Get("9router")
	counts := map[string]int{}
	for _, m := range conn.Models {
		counts[m.ID]++
		if m.Custom && m.ID == "kr/qwen3-coder-next" {
			t.Error("custom flag kept although the ID is now discovered; would double-list")
		}
	}
	for _, id := range []string{"kr/auto", "kr/glm-5", "kr/brand-new", "kr/qwen3-coder-next"} {
		if counts[id] != 1 {
			t.Errorf("model %q appears %d times, want exactly 1", id, counts[id])
		}
	}
}

// TestMergeKeepsCustomNotInDiscovery covers requirement 16: a manually
// registered model absent from live /models stays selectable.
func TestMergeKeepsCustomNotInDiscovery(t *testing.T) {
	s, _ := newStoreWith9Router(t)
	s.AddCustomModel("9router", "kr/dashboard-only", "Dashboard Only")

	if err := s.MergeDiscoveredModels("9router", []Model{{ID: "kr/auto"}}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	conn, _ := s.Get("9router")
	var kept bool
	for _, m := range conn.Models {
		if m.ID == "kr/dashboard-only" && m.Custom && m.Name == "Dashboard Only" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("refresh deleted a custom model: %+v", conn.Models)
	}
}

package userconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "providers.json")
	s, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(missing) error = %v", err)
	}
	if len(s.Connections) != 0 {
		t.Fatal("missing file should yield an empty store")
	}

	conn := Connection{
		ID: "openrouter", Name: "OpenRouter", Class: "openai-compatible",
		Endpoint: "https://openrouter.ai/api/v1", APIKey: "sk-secret",
		Models: []Model{{ID: "vendor/model:variant", Name: "Variant"}},
	}
	if err := s.Put(conn); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	reloaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := reloaded.Get("openrouter")
	if !ok {
		t.Fatal("saved connection not found after reload")
	}
	if got.APIKey != "sk-secret" || len(got.Models) != 1 || got.Models[0].ID != "vendor/model:variant" {
		t.Errorf("reloaded connection = %+v", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("store permissions = %o, want 600 (file holds secrets)", info.Mode().Perm())
	}
}

func TestStorePutUpdatesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	s, _ := LoadFrom(path)
	s.Put(Connection{ID: "x", Name: "X"})
	s.Put(Connection{ID: "x", Name: "X2", Endpoint: "http://x/v1"})

	reloaded, _ := LoadFrom(path)
	got, ok := reloaded.Get("x")
	if !ok || got.Name != "X2" || len(reloaded.Connections) != 1 {
		t.Errorf("Put did not update in place: %+v (%d entries)", got, len(reloaded.Connections))
	}
}

func TestStoreDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	s, _ := LoadFrom(path)
	s.Put(Connection{ID: "a"})
	s.Put(Connection{ID: "b"})
	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("a"); ok {
		t.Error("deleted connection still present")
	}
}

func TestCustomNameToID(t *testing.T) {
	cases := map[string]string{
		"My Provider!": "my-provider",
		"  Acme  AI  ": "-acme-ai", // leading dashes are trimmed below
		"glm4-x":       "glm4-x",
		"!!!":          "custom",
		"":             "custom",
	}
	for name, want := range cases {
		want = strings.Trim(want, "-")
		if want == "" {
			want = "custom"
		}
		if got := CustomNameToID(name); got != want {
			t.Errorf("CustomNameToID(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact(""); got != "(not set)" {
		t.Errorf("Redact(empty) = %q", got)
	}
	if got := Redact("sk-super-secret"); got == "sk-super-secret" || got == "" {
		t.Errorf("Redact leaked the key: %q", got)
	}
}

func TestSanitize(t *testing.T) {
	out := Sanitize("connect failed: bad key sk-alice at position 3", "sk-alice")
	if strings.Contains(out, "sk-alice") {
		t.Errorf("Sanitize left the key in %q", out)
	}
	if Sanitize("plain", "") != "plain" {
		t.Error("Sanitize with empty secret must be a no-op")
	}
}

// TestResolvePrecedence pins the documented order: saved connection
// (explicit or imported) beats environment variables, which beat
// fallbacks.
func TestResolvePrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("OPENROUTER_API_KEY", "env-key")

	path := filepath.Join(dir, "providers.json")

	// No connection: environment wins over defaults.
	r := Resolve(nil, "openrouter", "https://fallback.example/v1")
	if r.Found || r.APIKey != "env-key" || r.Endpoint != "https://fallback.example/v1" {
		t.Errorf("env-level resolve = %+v", r)
	}

	// Local provider without key env: just the fallback endpoint.
	r = Resolve(nil, "ollama", "http://localhost:11434")
	if r.APIKey != "" || r.Found {
		t.Errorf("ollama resolve = %+v", r)
	}

	// Saved connection overrides both env and config.yaml fallback.
	s, _ := LoadFrom(path)
	s.Put(Connection{ID: "openrouter", Endpoint: "https://saved.example/v1", APIKey: "saved-key"})
	r = Resolve(s, "openrouter", "https://fallback.example/v1")
	if !r.Found || r.Endpoint != "https://saved.example/v1" || r.APIKey != "saved-key" {
		t.Errorf("connection-level resolve = %+v", r)
	}
}

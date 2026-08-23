package providers

import "testing"

// TestRegistryEntries pins the Milestone 9 provider catalog: every
// supported provider is registered with its class, default endpoint,
// and API-key environment variable.
func TestRegistryEntries(t *testing.T) {
	cases := []struct {
		id        string
		class     string
		endpoint  string
		apiKeyEnv string
	}{
		{"ollama", ClassOllama, "http://localhost:11434", ""},
		{"lmstudio", ClassOpenAICompatible, "http://localhost:1234/v1", ""},
		{"nvidia", ClassOpenAICompatible, "https://integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"},
		{"openrouter", ClassOpenAICompatible, "https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"},
		{"9router", ClassOpenAICompatible, "http://localhost:20128/v1", "NINEROUTER_KEY"},
		{"omniroute", ClassOpenAICompatible, "http://localhost:8787/v1", "OMNIROUTE_KEY"},
	}
	for _, c := range cases {
		p, ok := ByID(c.id)
		if !ok {
			t.Errorf("provider %q missing from registry", c.id)
			continue
		}
		if p.Class != c.class {
			t.Errorf("provider %q class = %q, want %q", c.id, p.Class, c.class)
		}
		if p.Endpoint != c.endpoint {
			t.Errorf("provider %q endpoint = %q, want %q", c.id, p.Endpoint, c.endpoint)
		}
		if p.APIKeyEnv != c.apiKeyEnv {
			t.Errorf("provider %q APIKeyEnv = %q, want %q", c.id, p.APIKeyEnv, c.apiKeyEnv)
		}
		if p.RequiresAPIKey() != (c.apiKeyEnv != "") {
			t.Errorf("provider %q RequiresAPIKey() = %v, inconsistent with env %q", c.id, p.RequiresAPIKey(), c.apiKeyEnv)
		}
	}
}

func TestByIDUnknownProvider(t *testing.T) {
	if _, ok := ByID("no-such-provider"); ok {
		t.Error("ByID reported an unknown provider as found")
	}
}

func TestDisplayName(t *testing.T) {
	if got := DisplayName("openrouter"); got != "OpenRouter" {
		t.Errorf("DisplayName(openrouter) = %q", got)
	}
	if got := DisplayName("custom-thing"); got != "custom-thing" {
		t.Errorf("unregistered ID should fall through raw, got %q", got)
	}
}

// TestNewNvidiaProviderCompatibilityWrapper pins that the legacy
// constructor still yields a working provider with identical fields.
func TestNewNvidiaProviderCompatibilityWrapper(t *testing.T) {
	viaOld := NewNvidiaProvider("https://example.com/v1/", "m", "k", nil)
	viaNew := NewOpenAICompatible("https://example.com/v1/", "m", "k", nil)
	if viaOld.Endpoint != viaNew.Endpoint || viaOld.Model != viaNew.Model || viaOld.APIKey != viaNew.APIKey {
		t.Errorf("compat wrapper diverged: old=%+v new=%+v", viaOld, viaNew)
	}
	if viaOld.Endpoint != "https://example.com/v1" {
		t.Errorf("trailing slash not trimmed: %q", viaOld.Endpoint)
	}
}

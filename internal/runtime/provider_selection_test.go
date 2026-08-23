package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lato/internal/config"
	"lato/internal/effort"
	"lato/internal/providers"
)

// sseLine writes one server-sent event payload.
func sseLine(w http.ResponseWriter, payload string) {
	w.Write([]byte("data: " + payload + "\n\n"))
}

func decodeJSONBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// TestNewProviderSelectsImplementation pins that each registered
// provider ID constructs the right implementation with the configured
// endpoint, model, and key. User-level paths are isolated so a real
// saved /connect connection on the dev machine cannot leak into the
// credential precedence.
func TestNewProviderSelectsImplementation(t *testing.T) {
	isolateUserConfig(t)
	cases := []struct {
		provider string
		endpoint string
		apiKey   string
		wantType any
	}{
		{"ollama", "http://localhost:11434", "", &providers.OllamaProvider{}},
		{"lmstudio", "http://localhost:1234/v1", "", &providers.NvidiaProvider{}},
		{"nvidia", "https://integrate.api.nvidia.com/v1", "nv", &providers.NvidiaProvider{}},
		{"openrouter", "https://openrouter.ai/api/v1", "or", &providers.NvidiaProvider{}},
		{"9router", "http://localhost:20128/v1", "nr", &providers.NvidiaProvider{}},
		{"omniroute", "http://localhost:8787/v1", "om", &providers.NvidiaProvider{}},
	}
	for _, c := range cases {
		rt := newTestRuntime(nil)
		cfg := &config.Config{Model: config.Model{
			Provider: c.provider,
			Endpoint: c.endpoint,
			Name:     "vendor/model:variant",
			APIKey:   c.apiKey,
		}}
		p, err := rt.newProvider(cfg, effort.Medium)
		if err != nil {
			t.Errorf("newProvider(%s) error = %v", c.provider, err)
			continue
		}
		switch c.wantType.(type) {
		case *providers.OllamaProvider:
			got, ok := p.(*providers.OllamaProvider)
			if !ok {
				t.Errorf("provider %s built %T, want OllamaProvider", c.provider, p)
				continue
			}
			if got.Endpoint != c.endpoint || got.Model != "vendor/model:variant" {
				t.Errorf("ollama endpoint/model = %s/%s", got.Endpoint, got.Model)
			}
		case *providers.NvidiaProvider:
			got, ok := p.(*providers.NvidiaProvider)
			if !ok {
				t.Errorf("provider %s built %T, want OpenAI-compatible", c.provider, p)
				continue
			}
			if got.Endpoint != c.endpoint || got.APIKey != c.apiKey || got.Model != "vendor/model:variant" {
				// Never include the API key value in failure output.
				t.Errorf("provider %s endpoint/key-match/model = %s/%v/%s",
					c.provider, got.Endpoint, got.APIKey == c.apiKey, got.Model)
			}
		}
	}
}

// TestNewProviderMissingAPIKeyFailsFast verifies a hosted provider
// without its environment key fails construction with a clear message
// naming the variable — before any HTTP call happens. User-level paths
// are isolated so a saved /connect connection cannot satisfy the
// requirement.
func TestNewProviderMissingAPIKeyFailsFast(t *testing.T) {
	isolateUserConfig(t)
	rt := newTestRuntime(nil)
	for _, id := range []string{"openrouter", "9router", "omniroute", "nvidia"} {
		info, _ := providers.ByID(id)
		cfg := &config.Config{Model: config.Model{
			Provider: id,
			Endpoint: info.Endpoint,
			Name:     "m",
			APIKey:   "",
		}}
		_, err := rt.newProvider(cfg, effort.Medium)
		if err == nil {
			t.Errorf("newProvider(%s) without key succeeded, want error", id)
			continue
		}
		if !strings.Contains(err.Error(), info.APIKeyEnv) {
			t.Errorf("newProvider(%s) error = %q, want it to name %s", id, err, info.APIKeyEnv)
		}
	}
}

// TestNewProviderUnknownID verifies unknown provider IDs still fail
// fast with guidance.
func TestNewProviderUnknownID(t *testing.T) {
	rt := newTestRuntime(nil)
	cfg := &config.Config{Model: config.Model{Provider: "warp-drive", Endpoint: "http://x", Name: "m"}}
	if _, err := rt.newProvider(cfg, effort.Medium); err == nil || !strings.Contains(err.Error(), "warp-drive") {
		t.Errorf("newProvider(unknown) error = %v, want unsupported-provider error", err)
	}
}

// TestOpenAICompatibleToolLoop drives the full agent loop against an
// httptest OpenAI-compatible server: turn 1 streams tool-call fragments
// for `echo`, Lato executes it, turn 2 returns the final answer. This is
// the exact flow online providers must support.
func TestOpenAICompatibleToolLoop(t *testing.T) {
	var authHeader string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		calls++
		w.Header().Set("Content-Type", "text/event-stream")

		if calls == 1 {
			var body struct {
				Tools []jsonToolDef `json:"tools"`
			}
			if err := decodeJSONBody(r, &body); err != nil {
				t.Errorf("decode request: %v", err)
			}
			if len(body.Tools) == 0 {
				t.Error("first request sent no tool definitions; tools must stay enabled online")
			}
			sseLine(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-9","function":{"name":"echo","arguments":"{\"value\":\"from openai-compat\"}"}}]}}]}`)
			sseLine(w, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}

		sseLine(w, `{"choices":[{"delta":{"content":"tool said: from openai-compat"}}]}`)
		sseLine(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := providers.NewOpenAICompatible(server.URL, "test-model", "loop-key", nil)
	rt := newTestRuntime(p)

	response, err := rt.RunContext(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "use a tool"},
	})
	if err != nil {
		t.Fatalf("RunContext() error = %v", err)
	}
	if response.Content != "tool said: from openai-compat" {
		t.Errorf("final response = %q, want tool-informed answer", response.Content)
	}
	if calls != 2 {
		t.Errorf("provider calls = %d, want 2 (tool call + continuation)", calls)
	}
	if authHeader != "Bearer loop-key" {
		t.Errorf("Authorization = %q, want bearer key", authHeader)
	}
}

type jsonToolDef struct {
	Name string `json:"name"`
}

// TestOpenRouterAndRoutersUseSharedImplementation documents that all
// OpenAI-shaped providers share one HTTP implementation: constructing
// them yields the same concrete type.
func TestOpenRouterAndRoutersUseSharedImplementation(t *testing.T) {
	for _, id := range []string{"openrouter", "9router", "omniroute", "nvidia", "lmstudio"} {
		info, ok := providers.ByID(id)
		if !ok {
			t.Fatalf("provider %s missing from registry", id)
		}
		if info.Class != providers.ClassOpenAICompatible {
			t.Errorf("provider %s class = %q, want shared OpenAI-compatible class", id, info.Class)
		}
	}
}

package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lato/internal/effort"
)

// TestEffortCapabilityForUnknownProviders pins the conservative default:
// providers without a declared entry — including custom /connect
// providers — resolve to no mechanism at all.
func TestEffortCapabilityForUnknownProviders(t *testing.T) {
	for _, id := range []string{"ollama", "lmstudio", "nvidia", "9router", "omniroute", "my-custom-provider", ""} {
		cap := EffortCapabilityFor(id)
		if cap.Mechanism != EffortNone || len(cap.Supported) != 0 {
			t.Errorf("capability for %q = %+v, want no mechanism", id, cap)
		}
	}
}

// TestResolveProviderEffortClampsToDeclaredSet pins that mapping is
// derived from the provider's declared token set, never hardcoded: a
// three-token set clamps Ultra/Lato-X at its strongest entry.
func TestResolveProviderEffortClampsToDeclaredSet(t *testing.T) {
	mech, token, ok := ResolveProviderEffort("openrouter", effort.Low)
	if !ok || mech != EffortReasoningObject || token != "low" {
		t.Errorf("low = (%v,%q,%v)", mech, token, ok)
	}
	mech, token, _ = ResolveProviderEffort("openrouter", effort.High)
	if token != "high" {
		t.Errorf("high token = %q, want high", token)
	}
	for _, level := range []effort.Level{effort.Ultra, effort.LatoX} {
		mech, token, ok := ResolveProviderEffort("openrouter", level)
		if !ok || mech != EffortReasoningObject || token != "high" {
			t.Errorf("%v clamps to (%v,%q,%v), want reasoning/high", level, mech, token, ok)
		}
	}
}

// TestResolveProviderEffortUsesAdvertisedTokens proves a wider declared
// set automatically lets higher ladder levels reach stronger tokens
// without code changes.
func TestResolveProviderEffortUsesAdvertisedTokens(t *testing.T) {
	effortCapabilities["test-wide"] = EffortCapability{
		Mechanism: EffortReasoningField,
		Supported: []string{"low", "medium", "high", "xhigh", "max"},
	}
	defer delete(effortCapabilities, "test-wide")

	cases := map[effort.Level]string{
		effort.Low:    "low",
		effort.Medium: "medium",
		effort.High:   "high",
		effort.Ultra:  "xhigh",
		effort.LatoX:  "max",
	}
	for level, want := range cases {
		mech, token, ok := ResolveProviderEffort("test-wide", level)
		if !ok || mech != EffortReasoningField || token != want {
			t.Errorf("level %v = (%v,%q,%v), want field/%q", level, mech, token, ok, want)
		}
	}
}

// TestUnsupportedEffortNeverResolved guards the core safety rule: no
// mechanism → nothing may be sent upstream.
func TestUnsupportedEffortNeverResolved(t *testing.T) {
	for _, id := range []string{"9router", "ollama"} {
		if mech, token, ok := ResolveProviderEffort(id, effort.LatoX); ok {
			t.Errorf("%s resolved (%q,%q); unsupported providers must resolve nothing", id, mech, token)
		}
	}
}

// captureChatRequest runs one streaming request against a stub server
// and decodes the JSON body the provider actually sent.
func captureChatRequest(t *testing.T, p *NvidiaProvider) map[string]any {
	t.Helper()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(r.Body)
		_ = dec.Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	p.Endpoint = server.URL
	p.client = server.Client()

	events, err := p.StreamChat(t.Context(), []Message{{Role: UserRole, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	return body
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasKey(m map[string]any, key string) bool {
	for _, k := range keysOf(m) {
		if k == key {
			return true
		}
	}
	return false
}

// TestEffortFieldOnTheWire pins exact wire behavior per mechanism.
func TestEffortFieldOnTheWire(t *testing.T) {
	t.Run("reasoning object mechanism sends only the object form", func(t *testing.T) {
		p := NewOpenAICompatible("http://x", "m", "", nil)
		p.ApplyEffort(string(EffortReasoningObject), "high")
		body := captureChatRequest(t, p)

		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "high" {
			t.Errorf("reasoning object missing/wrong: %v", body["reasoning"])
		}
		if hasKey(body, "reasoning_effort") {
			t.Errorf("reasoning_effort field must not be sent under the object mechanism: %v", keysOf(body))
		}
	})

	t.Run("field mechanism sends only the flat field", func(t *testing.T) {
		p := NewOpenAICompatible("http://x", "m", "", nil)
		p.ApplyEffort(string(EffortReasoningField), "low")
		body := captureChatRequest(t, p)

		if v, _ := body["reasoning_effort"].(string); v != "low" {
			t.Errorf("reasoning_effort = %v, want low", body["reasoning_effort"])
		}
		if hasKey(body, "reasoning") {
			t.Errorf("reasoning object must not be sent under the field mechanism: %v", keysOf(body))
		}
	})

	t.Run("no mechanism sends neither field", func(t *testing.T) {
		p := NewOpenAICompatible("http://x", "m", "", nil)
		body := captureChatRequest(t, p)
		if hasKey(body, "reasoning") || hasKey(body, "reasoning_effort") {
			t.Errorf("unsupported provider got effort fields: %v", keysOf(body))
		}
	})

	t.Run("unknown mechanism is ignored entirely", func(t *testing.T) {
		p := NewOpenAICompatible("http://x", "m", "", nil)
		p.ApplyEffort("telepathy", "maximum")
		body := captureChatRequest(t, p)
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), "telepathy") || strings.Contains(string(raw), "maximum") {
			t.Errorf("unknown mechanism leaked onto the wire: %s", raw)
		}
	})
}

package providers

import (
	"testing"
)

func TestLatoFreeRegistryEntry(t *testing.T) {
	info, ok := ByID("lato-free")
	if !ok {
		t.Fatal("lato-free not found in registry")
	}
	if info.ID != "lato-free" {
		t.Errorf("ID = %q, want lato-free", info.ID)
	}
	if info.Name != "Lato Free" {
		t.Errorf("Name = %q, want Lato Free", info.Name)
	}
	if info.Class != ClassOpenAICompatible {
		t.Errorf("Class = %q, want %q", info.Class, ClassOpenAICompatible)
	}
	if info.Endpoint != "https://openrouter.ai/api/v1" {
		t.Errorf("Endpoint = %q, want https://openrouter.ai/api/v1", info.Endpoint)
	}
	if info.APIKeyEnv != "LATO_FREE_OPENROUTER_API_KEY" {
		t.Errorf("APIKeyEnv = %q, want LATO_FREE_OPENROUTER_API_KEY", info.APIKeyEnv)
	}
	if !info.RequiresAPIKey() {
		t.Error("RequiresAPIKey() should be true")
	}
}
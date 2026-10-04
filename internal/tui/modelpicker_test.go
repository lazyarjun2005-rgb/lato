package tui

import (
	"strings"
	"testing"

	"lato/internal/effort"
	"lato/internal/providers"
)

func testModelGroups() []modelGroup {
	return []modelGroup{
		{ID: "ollama", Name: "Ollama", Models: []providers.ModelInfo{
			{ID: "llama3.2", Name: "Llama 3.2"},
			{ID: "qwen2.5-coder", Name: "Qwen Coder"},
		}},
		{ID: "openrouter", Name: "OpenRouter", Models: []providers.ModelInfo{
			{ID: "anthropic/claude-sonnet", Name: "Claude Sonnet"},
		}},
	}
}

func TestModelPickerSearchMatchesIDNameAndProvider(t *testing.T) {
	p := newSearchableModelPicker(testModelGroups(), "ollama", "llama3.2", effort.Default)
	checks := []struct{ query, want string }{{"QWEN", "qwen2.5-coder"}, {"claude", "anthropic/claude-sonnet"}, {"OPENROUTER", "anthropic/claude-sonnet"}}
	for _, tc := range checks {
		p.input.SetValue(tc.query)
		p.filter()
		choice, ok := p.selected()
		if !ok || choice.model.ID != tc.want {
			t.Errorf("query %q selected %+v, want %q", tc.query, choice, tc.want)
		}
	}
}

func TestModelPickerStartsOnCurrentModelAndNavigatesBounds(t *testing.T) {
	p := newSearchableModelPicker(testModelGroups(), "ollama", "llama3.2", effort.Default)
	choice, ok := p.selected()
	if !ok || !choice.current || choice.model.ID != "llama3.2" {
		t.Fatalf("initial choice = %+v, %v", choice, ok)
	}
	for i := 0; i < len(p.matches)+2; i++ {
		p.move(1)
	}
	if p.cursor < 0 || p.cursor >= len(p.matches) {
		t.Fatalf("cursor %d outside %d matches", p.cursor, len(p.matches))
	}
}

func TestModelPickerEmptyResultsRenderSafely(t *testing.T) {
	p := newSearchableModelPicker(testModelGroups(), "ollama", "llama3.2", effort.Default)
	p.input.SetValue("not-a-model")
	p.filter()
	if _, ok := p.selected(); ok {
		t.Fatal("empty result unexpectedly selected a model")
	}
	if got := p.view(30, 6); !strings.Contains(got, "No models match") {
		t.Fatalf("empty view = %q", got)
	}
}

func TestModelPickerRetainsProviderMetadata(t *testing.T) {
	p := newSearchableModelPicker(testModelGroups(), "ollama", "llama3.2", effort.Default)
	p.input.SetValue("openrouter")
	p.filter()
	choice, ok := p.selected()
	if !ok || choice.providerID != "openrouter" || choice.providerName != "OpenRouter" {
		t.Fatalf("choice metadata = %+v, %v", choice, ok)
	}
}

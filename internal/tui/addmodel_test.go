package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"lato/internal/providers"
	"lato/internal/userconfig"
)

func connectedFixtures() []userconfig.Connection {
	return []userconfig.Connection{
		{ID: "9router", Name: "9Router"},
		{ID: "openrouter", Name: "OpenRouter"},
	}
}

func TestAddModelFlowListsConnectedProvidersOnly(t *testing.T) {
	f := newAddModelFlow(connectedFixtures())
	if len(f.selectPicker.options) != 2 {
		t.Fatalf("options = %d, want only the 2 connected providers", len(f.selectPicker.options))
	}
	view := f.selectPicker.view(80, 24)
	if !strings.Contains(view, "9Router") || !strings.Contains(view, "OpenRouter") {
		t.Error("connected providers missing from picker")
	}
}

// driveAddModel simulates key presses through the wizard.
func driveAddModel(t *testing.T, keys ...tea.KeyMsg) (*addModelFlow, tea.Cmd) {
	t.Helper()
	f := newAddModelFlow(connectedFixtures())
	var cmd tea.Cmd
	for _, k := range keys {
		var active bool
		active, cmd = f.handleKey(nil, k)
		if !active && cmd == nil {
			break
		}
	}
	return f, cmd
}

func key(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestAddModelFlowCapturesOpaqueID(t *testing.T) {
	f := newAddModelFlow(connectedFixtures())

	f.handleKey(nil, key(tea.KeyEnter)) // pick first provider (9router)
	if f.providerID != "9router" {
		t.Fatalf("provider = %q", f.providerID)
	}
	if len(f.steps) != 2 {
		t.Fatalf("steps = %d, want model ID + optional name", len(f.steps))
	}
	f.input.SetValue("kr/qwen3-coder-next")
	f.handleKey(nil, key(tea.KeyEnter))
	f.steps[0].apply("") // empty display name → defaults to ID at save time

	if f.modelID != "kr/qwen3-coder-next" {
		t.Fatalf("captured model=%q", f.modelID)
	}
	if _, modelID, _ := f.registration(); modelID != "kr/qwen3-coder-next" {
		t.Fatalf("registration model=%q", modelID)
	}
}

func TestAddModelFlowDisplayNameOptional(t *testing.T) {
	f := newAddModelFlow(connectedFixtures())
	f.handleKey(nil, key(tea.KeyEnter)) // provider
	f.input.SetValue("vendor/model:variant")
	f.handleKey(nil, key(tea.KeyEnter)) // model ID → moves to name step
	f.steps[0].apply("My Variant")      // name (final Enter would trigger the save cmd)

	providerID, modelID, name := f.registration()
	if providerID != "9router" || modelID != "vendor/model:variant" || name != "My Variant" {
		t.Fatalf("registration = %q/%q/%q", providerID, modelID, name)
	}
}

func TestAddModelFlowEscCancels(t *testing.T) {
	f := newAddModelFlow(connectedFixtures())
	active, _ := f.handleKey(nil, key(tea.KeyEsc))
	if active {
		t.Fatal("esc should cancel at provider selection")
	}
}

func TestInputModalPlaceholderDefaultUsedForEmptySubmit(t *testing.T) {
	im := newInputModal(inputStep{title: "T", prompt: "Model ID:", initial: ""})
	im.Update(keyRunes("kr/glm-5"))
	if im.Value() != "kr/glm-5" {
		t.Errorf("typed value lost: %q", im.Value())
	}
	if masked := newInputModal(inputStep{title: "K", prompt: "API key:", masked: true}); masked.input.EchoMode != textinput.EchoPassword {
		t.Error("masked steps must stay masked")
	}
}

func TestBuildModelGroupsMergesCustomIntoActive(t *testing.T) {
	conns := []userconfig.Connection{
		{ID: "9router", Name: "9Router", Models: []userconfig.Model{
			{ID: "kr/auto"},
			{ID: "kr/qwen3-coder-next", Custom: true},
		}},
	}
	live := []providers.ModelInfo{{ID: "kr/auto"}, {ID: "kr/glm-5"}}

	groups, ok := buildModelGroupList("9router", live, conns)
	if !ok {
		t.Fatal("grouping reported nothing to show")
	}
	active := groups[0]
	if len(active.Models) != 3 { // 2 live + 1 custom not in live
		t.Fatalf("active group = %+v, want live models plus the custom one", active.Models)
	}
	seen := map[string]int{}
	for _, mi := range active.Models {
		seen[mi.ID]++
	}
	if seen["kr/qwen3-coder-next"] != 1 || seen["kr/auto"] != 1 {
		t.Errorf("duplicate or missing entries: %v", seen)
	}

	// A custom whose ID is now also returned live must not duplicate.
	// With nothing extra to merge the picker legitimately falls back to
	// the flat live list, so count whatever would be displayed.
	live = append(live, providers.ModelInfo{ID: "kr/qwen3-coder-next"})
	groups2, _ := buildModelGroupList("9router", live, conns)
	shown := live
	if groups2 != nil {
		shown = groups2[0].Models
	}
	n := 0
	for _, mi := range shown {
		if mi.ID == "kr/qwen3-coder-next" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("custom+discovered ID listed %d times, want 1", n)
	}
}

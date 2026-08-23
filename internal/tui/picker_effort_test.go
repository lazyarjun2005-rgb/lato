package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/effort"
	"lato/internal/runtime"
)

// newPickerTestRuntime builds a real runtime against an isolated
// configuration so picker selections can be verified end to end without
// touching the developer's setup or the network.
func newPickerTestRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LATO_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))

	cfg := `model:
  provider: ollama
  endpoint: http://localhost:11434
  name: test-model
agent:
  name: default
  system_prompt: test
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	rt, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New() error = %v", err)
	}
	return rt
}

// TestPickerEffortNavigation pins the two-dimensional selector: ←/→ walk
// the ladder with clamping at both ends.
func TestPickerEffortNavigation(t *testing.T) {
	p := newGroupedModelPicker(groupsFixture(), "cc/glm-4:variant", effort.Medium)

	if got := p.selectedEffort(); got != effort.Medium {
		t.Fatalf("initial effort = %v, want medium", got)
	}

	p.moveEffortRight()
	if got := p.selectedEffort(); got != effort.High {
		t.Errorf("after → effort = %v, want high", got)
	}

	for i := 0; i < 10; i++ {
		p.moveEffortRight()
	}
	if got := p.selectedEffort(); got != effort.LatoX {
		t.Errorf("→ must clamp at lato-X, got %v", got)
	}

	for i := 0; i < 10; i++ {
		p.moveEffortLeft()
	}
	if got := p.selectedEffort(); got != effort.Low {
		t.Errorf("← must clamp at low, got %v", got)
	}
}

// TestPickerViewShowsLadder pins the visual contract: the ladder renders
// under the model list with the active level bracketed.
func TestPickerViewShowsLadder(t *testing.T) {
	p := newGroupedModelPicker(groupsFixture(), "cc/glm-4:variant", effort.High)
	view := p.view(100, 40)

	for _, want := range []string{"Effort:", "low", "medium", "[high]", "ultra", "lato-X"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker view missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "←/→ effort") {
		t.Errorf("help line missing effort hint:\n%s", view)
	}
	// The plain (non-effort) provider picker keeps its original help.
	pp := newProviderPicker("ollama")
	if strings.Contains(pp.view(100, 40), "←/→ effort") {
		t.Error("provider picker must not advertise effort controls")
	}
}

// TestPickerEnterAppliesModelAndEffort pins the end-to-end selection:
// ↑↓/←→ then Enter switches model and effort together and persists both
// as the new default.
func TestPickerEnterAppliesModelAndEffort(t *testing.T) {
	rt := newPickerTestRuntime(t)
	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}}
	m.palette = newSlashPalette(m.registry)

	m.selectPicker = newGroupedModelPicker(groupsFixture(), "", effort.Medium)
	m.selectPicker.cursor = 1 // first real model row ("qwen2.5:3b")
	m.selectPicker.skipHeader(1)
	m.selectPicker.moveEffortRight() // medium → high

	next, _ := m.handleSelectPickerKey(keyMsg(tea.KeyEnter, ""))
	done := next.(model)

	if done.modelName != "qwen2.5:3b" {
		t.Errorf("model label = %q, want qwen2.5:3b", done.modelName)
	}
	if done.effortName != "high" {
		t.Errorf("effort label = %q, want high", done.effortName)
	}
	if done.runtime.CurrentEffort() != "high" {
		t.Errorf("runtime effort = %q, want high", done.runtime.CurrentEffort())
	}

	// Persisted for future sessions.
	raw, err := os.ReadFile(filepath.Join(os.Getenv("LATO_HOME"), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "name: qwen2.5:3b") || !strings.Contains(string(raw), "effort: high") {
		t.Errorf("config not persisted correctly:\n%s", raw)
	}
}

// TestPickerSessionOnlyKeyDoesNotPersist pins the 's' behavior: state
// changes for this session while config.yaml stays untouched.
func TestPickerSessionOnlyKeyDoesNotPersist(t *testing.T) {
	rt := newPickerTestRuntime(t)
	cfgPath := filepath.Join(os.Getenv("LATO_HOME"), "config.yaml")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}}
	m.palette = newSlashPalette(m.registry)

	m.selectPicker = newGroupedModelPicker(groupsFixture(), "", effort.Medium)
	m.selectPicker.cursor = 1
	m.selectPicker.skipHeader(1)
	m.selectPicker.moveEffortRight()
	m.selectPicker.moveEffortRight() // ultra

	next, _ := m.handleSelectPickerKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	done := next.(model)

	if done.effortName != "ultra" {
		t.Errorf("session effort = %q, want ultra", done.effortName)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("session-only selection rewrote config.yaml:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestHeaderShowsEffort pins Part T: provider/model/effort are all
// visible in the header line.
func TestHeaderShowsEffort(t *testing.T) {
	m := model{
		agentName:    "default",
		providerName: "9router",
		modelName:    "oc/big-pickle",
		effortName:   "lato-X",
	}
	header := m.renderHeader()
	for _, want := range []string{"9router/oc/big-pickle", "lato-X"} {
		if !strings.Contains(header, want) {
			t.Errorf("header missing %q:\n%s", want, header)
		}
	}
}

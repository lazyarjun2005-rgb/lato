package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/command"
)

// TestRegistryContainsFast pins /fast registration in the production
// registry that powers dispatch, /help, and the palette.
func TestRegistryContainsFast(t *testing.T) {
	reg := newRegistry()
	cmd, ok := reg.Lookup("fast")
	if !ok {
		t.Fatal("/fast missing from the production registry")
	}
	if cmd.Name() != "fast" {
		t.Errorf("lookup for /fast resolved to %q", cmd.Name())
	}
}

// TestPaletteRecognizesFast checks autocomplete on a unique prefix and
// on the full name.
func TestPaletteRecognizesFast(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/fa")
	var got []string
	for _, s := range m.palette.matches {
		got = append(got, s.name)
	}
	if !containsString(got, "fast") {
		t.Errorf("/fa suggestions %v do not include fast", got)
	}

	m = typeInto(newPaletteTestModel(), "/fast")
	if len(m.palette.matches) != 1 || m.palette.matches[0].name != "fast" {
		t.Errorf("/fast suggestions = %+v, want exactly [fast]", m.palette.matches)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestFastDispatchThroughTUIContext runs /fast through the REAL
// dispatcher with the TUI model as Context: the runtime's effort moves
// to LOW for this session only, config.yaml stays untouched, and the
// header label follows — all offline.
func TestFastDispatchThroughTUIContext(t *testing.T) {
	rt := newPickerTestRuntime(t)
	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}}
	m.palette = newSlashPalette(m.registry)

	isCommand, err := command.Dispatch(&m, m.registry, "/fast")
	if err != nil {
		t.Fatalf("/fast dispatch: %v", err)
	}
	if !isCommand {
		t.Fatal("/fast not recognized as a command")
	}

	if got := rt.CurrentEffort(); got != "low" {
		t.Errorf("runtime effort = %q, want low", got)
	}
	if m.effortName != "low" {
		t.Errorf("header effort label = %q, want low", m.effortName)
	}

	var out strings.Builder
	for _, e := range m.entries {
		out.WriteString(e.Content + "\n")
	}
	if !strings.Contains(out.String(), "session only") {
		t.Errorf("confirmation missing:\n%s", out.String())
	}

	// Session-only: nothing may leak into the persisted defaults.
	raw, err := os.ReadFile(filepath.Join(os.Getenv("LATO_HOME"), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "effort:") {
		t.Errorf("config.yaml gained an effort entry from /fast:\n%s", raw)
	}
}

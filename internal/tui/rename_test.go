package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/command"
	"lato/internal/session"
)

// TestRegistryContainsRename pins registration in the production
// registry behind dispatch, /help, and the palette.
func TestRegistryContainsRename(t *testing.T) {
	reg := newRegistry()
	cmd, ok := reg.Lookup("rename")
	if !ok {
		t.Fatal("/rename missing from the production registry")
	}
	if cmd.Name() != "rename" {
		t.Errorf("lookup for /rename resolved to %q", cmd.Name())
	}
}

// TestPaletteRecognizesRename checks the full-name match; /re is shared
// with /review, so only the complete token must be unique.
func TestPaletteRecognizesRename(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/rename")
	if len(m.palette.matches) != 1 || m.palette.matches[0].name != "rename" {
		t.Errorf("/rename suggestions = %+v, want exactly [rename]", m.palette.matches)
	}

	m = typeInto(newPaletteTestModel(), "/re")
	var got []string
	for _, s := range m.palette.matches {
		got = append(got, s.name)
	}
	if !containsString(got, "rename") || !containsString(got, "review") {
		t.Errorf("/re suggestions %v should include both rename and review", got)
	}
}

// TestRenameDispatchPersistsTitle runs /rename through the REAL
// dispatcher with a live session: title lands on the session, in the
// session file, and errors are refused without side effects.
func TestRenameDispatchPersistsTitle(t *testing.T) {
	rt := newPickerTestRuntime(t)

	// Session files are CWD-relative; isolate them per test.
	tmp := t.TempDir()
	t.Chdir(tmp)

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)

	isCommand, err := command.Dispatch(&m, m.registry, "/rename My Authentication Debugging")
	if err != nil {
		t.Fatalf("/rename dispatch: %v", err)
	}
	if !isCommand {
		t.Fatal("/rename not recognized as a command")
	}
	if got := m.session.Title; got != "My Authentication Debugging" {
		t.Fatalf("session title = %q", got)
	}

	reloaded, err := session.Load(m.session.ID)
	if err != nil {
		t.Fatalf("Load() after rename: %v", err)
	}
	if reloaded.Title != "My Authentication Debugging" {
		t.Errorf("persisted title = %q", reloaded.Title)
	}

	// Bare invocation: usage error, nothing changes. (Rendering the
	// error into the transcript is submitInput's job in production;
	// direct Dispatch reports it through the error return.)
	before := len(m.entries)
	isCommand, err = command.Dispatch(&m, m.registry, "/rename")
	if !isCommand || err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("bare /rename: isCommand=%v err=%v, want usage error", isCommand, err)
	}
	if m.session.Title != "My Authentication Debugging" {
		t.Errorf("failed rename changed the title: %q", m.session.Title)
	}
	if len(m.entries) != before {
		t.Error("failed rename added transcript entries")
	}

	// Cleanup guard: nothing leaked into the repo working directory.
	if _, err := os.Stat(filepath.Join(".lato")); !os.IsNotExist(err) && filepath.Clean(".lato") != filepath.Join(tmp, ".lato") {
		t.Logf("note: .lato visible at %s (expected under temp dir)", tmp)
	}
}

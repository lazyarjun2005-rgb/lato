package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/command"
	"lato/internal/runtime"
	"lato/internal/session"
)

// newClearTestModel builds a model over a real session containing old
// conversation turns, with the session store isolated to a temp dir.
func newClearTestModel(t *testing.T) model {
	t.Helper()
	rt := newPickerTestRuntime(t)
	t.Chdir(t.TempDir()) // session files are CWD-relative

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)
	m.session.AddMessage("user", "old question")
	m.session.AddMessage("assistant", "old answer")
	m.entries = sessionEntries(m.session) // mirror production transcript state
	if err := m.session.Save(); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestClearDispatchResetsConversationAndPersists covers requirements
// 1-6: transcript, persisted Messages, identity preservation, and the
// confirmation — all through the REAL dispatcher.
func TestClearDispatchResetsConversationAndPersists(t *testing.T) {
	m := newClearTestModel(t)
	id, created, title := m.session.ID, m.session.CreatedAt, m.session.Title

	isCommand, err := command.Dispatch(&m, m.registry, "/clear")
	if err != nil {
		t.Fatalf("/clear dispatch: %v", err)
	}
	if !isCommand {
		t.Fatal("/clear not recognized as a command")
	}

	// Transcript: only the confirmation remains.
	if len(m.entries) != 1 || m.entries[0].Role != roleSystem ||
		!strings.Contains(m.entries[0].Content, "Conversation cleared") {
		t.Errorf("entries after clear = %+v, want single confirmation", m.entries)
	}

	// Session state in memory.
	if len(m.session.Messages) != 0 {
		t.Errorf("messages = %+v, want empty", m.session.Messages)
	}
	if m.session.ID != id || !m.session.CreatedAt.Equal(created) || m.session.Title != title {
		t.Errorf("identity changed: id=%q created=%v title=%q", m.session.ID, m.session.CreatedAt, m.session.Title)
	}

	// Session state on disk.
	reloaded, err := session.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Messages) != 0 || reloaded.Title != title {
		t.Errorf("reloaded session = %+v", reloaded)
	}
}

// TestClearThenNextRequestStartsFresh proves the actual bug is gone:
// after /clear, the next submitted message reaches the model with NO
// pre-clear history.
func TestClearThenNextRequestStartsFresh(t *testing.T) {
	m := newClearTestModel(t)

	if _, err := command.Dispatch(&m, m.registry, "/clear"); err != nil {
		t.Fatalf("/clear dispatch: %v", err)
	}

	const fresh = "brand new topic"
	if err := m.SubmitPrompt(fresh); err != nil {
		t.Fatalf("SubmitPrompt() error = %v", err)
	}

	got := m.session.ProviderMessages()
	if len(got) != 1 || got[0].Content != fresh {
		t.Errorf("provider messages = %+v, want only the fresh turn", got)
	}
	for _, msg := range got {
		if strings.Contains(msg.Content, "old question") || strings.Contains(msg.Content, "old answer") {
			t.Errorf("pre-clear history leaked into the request: %+v", got)
		}
	}
	// Detach the offline stream: wiring-only assertion.
	m.pendingStream = nil
	m.stream = nil
}

// TestClearRefusedWhileBusy pins stream safety: with waiting=true and a
// live pendingStream sentinel, /clear must refuse without touching any
// conversation state.
func TestClearRefusedWhileBusy(t *testing.T) {
	m := newClearTestModel(t)
	sentinel := make(chan runtime.Event, 1)
	close(sentinel)
	m.waiting = true
	m.pendingStream = sentinel

	isCommand, err := command.Dispatch(&m, m.registry, "/clear")
	if !isCommand {
		t.Fatal("/clear not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("error = %v, want busy refusal", err)
	}

	if len(m.session.Messages) != 2 {
		t.Errorf("messages = %d, want untouched 2", len(m.session.Messages))
	}
	if len(m.entries) != 2 {
		t.Errorf("transcript entries = %d, want untouched 2", len(m.entries))
	}
	if m.pendingStream != sentinel {
		t.Error("active stream was disturbed")
	}

	reloaded, err := session.Load(m.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Messages) != 2 {
		t.Errorf("persisted messages = %d, want untouched 2", len(reloaded.Messages))
	}
}

// TestClearSaveFailurePropagates: when persistence fails, /clear must
// report it instead of claiming success. The blocker: .lato exists as a
// regular FILE, so MkdirAll underneath it fails.
func TestClearSaveFailurePropagates(t *testing.T) {
	rt := newPickerTestRuntime(t)
	tmp := t.TempDir()
	t.Chdir(tmp)

	if err := os.WriteFile(filepath.Join(tmp, ".lato"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)

	isCommand, err := command.Dispatch(&m, m.registry, "/clear")
	if !isCommand {
		t.Fatal("/clear not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "save session") {
		t.Fatalf("error = %v, want save failure", err)
	}
	var success bool
	for _, e := range m.entries {
		if strings.Contains(e.Content, "Conversation cleared") {
			success = true
		}
	}
	if success {
		t.Error("success confirmation printed despite save failure")
	}
}

// TestClearOnEmptySessionSucceeds: clearing an already-empty
// conversation is safe and still confirms.
func TestClearOnEmptySessionSucceeds(t *testing.T) {
	rt := newPickerTestRuntime(t)
	t.Chdir(t.TempDir())

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)

	isCommand, err := command.Dispatch(&m, m.registry, "/clear")
	if err != nil {
		t.Fatalf("/clear on empty session: %v", err)
	}
	if !isCommand {
		t.Fatal("/clear not recognized as a command")
	}
	if len(m.entries) != 1 {
		t.Errorf("entries after clear = %+v, want single confirmation", m.entries)
	}
}

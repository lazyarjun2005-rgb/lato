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

// newRewindTestModel seeds a three-turn conversation in an isolated
// store and mirrors it into the transcript, like production state.
func newRewindTestModel(t *testing.T) model {
	t.Helper()
	rt := newPickerTestRuntime(t)
	t.Chdir(t.TempDir())

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)
	m.session.Rename("Rewind Target")
	for _, pair := range [][2]string{
		{"question A", "answer A"},
		{"question B", "answer B"},
		{"question C", "answer C"},
	} {
		m.session.AddMessage("user", pair[0])
		m.session.AddMessage("assistant", pair[1])
	}
	m.entries = sessionEntries(m.session)
	if err := m.session.Save(); err != nil {
		t.Fatal(err)
	}
	return m
}

func rewindContent(m model) string { return transcriptOf(m) }

// TestRewindDispatchRemovesTurnsAndPersists covers /rewind 1 and
// /rewind 2 through the REAL dispatcher: persisted Messages shorten,
// the transcript is REBUILT from persisted history (activity-free),
// identity survives, and the confirmation lands after success.
func TestRewindDispatchRemovesTurnsAndPersists(t *testing.T) {
	cases := []struct {
		line       string
		wantMsgs   int
		wantLast   string
		wantPhrase string
	}{
		{"/rewind 1", 4, "answer B", "✓ Rewound 1 conversation turn."},
		{"/rewind 2", 2, "answer A", "✓ Rewound 2 conversation turns."},
	}
	for _, tc := range cases {
		m := newRewindTestModel(t)
		id, title := m.session.ID, m.session.Title

		isCommand, err := command.Dispatch(&m, m.registry, tc.line)
		if err != nil {
			t.Fatalf("%s: %v", tc.line, err)
		}
		if !isCommand {
			t.Fatalf("%s not recognized", tc.line)
		}

		if len(m.session.Messages) != tc.wantMsgs {
			t.Errorf("%s: messages = %d, want %d", tc.line, len(m.session.Messages), tc.wantMsgs)
		}
		last := m.session.Messages[len(m.session.Messages)-1]
		if last.Content != tc.wantLast {
			t.Errorf("%s: last message = %q, want %q", tc.line, last.Content, tc.wantLast)
		}
		if m.session.ID != id || m.session.Title != title {
			t.Errorf("%s: identity changed", tc.line)
		}

		reloaded, err := session.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(reloaded.Messages) != tc.wantMsgs {
			t.Errorf("%s: persisted messages = %d, want %d", tc.line, len(reloaded.Messages), tc.wantMsgs)
		}

		content := rewindContent(m)
		if !strings.Contains(content, tc.wantPhrase) {
			t.Errorf("%s: confirmation missing:\n%s", tc.line, content)
		}
		if strings.Contains(content, "question C") && tc.line == "/rewind 2" {
			t.Errorf("%s: removed turns still visible:\n%s", tc.line, content)
		}
		// Transcript must be a pure rebuild: exactly N message entries +
		// one confirmation — no stale activity lines.
		if got := len(m.entries); got != tc.wantMsgs+1 {
			t.Errorf("%s: entries = %d, want %d (rebuilt + confirmation)", tc.line, got, tc.wantMsgs+1)
		}
	}
}

// TestRewindIncompleteFinalTurnThroughTUI: an unanswered final request
// is rewound by itself.
func TestRewindIncompleteFinalTurnThroughTUI(t *testing.T) {
	m := newRewindTestModel(t)
	m.session.AddMessage("user", "question D") // response never persisted
	m.entries = sessionEntries(m.session)

	isCommand, err := command.Dispatch(&m, m.registry, "/rewind")
	if err != nil {
		t.Fatalf("/rewind dispatch: %v", err)
	}
	_ = isCommand

	last := m.session.Messages[len(m.session.Messages)-1]
	if last.Content != "answer C" {
		t.Errorf("last message = %q, want answer C (only question D removed)", last.Content)
	}
}

// TestRewindRefusedWhileBusy pins stream safety with the sentinel
// pattern: error, zero mutation anywhere.
func TestRewindRefusedWhileBusy(t *testing.T) {
	m := newRewindTestModel(t)

	sentinel := make(chan runtime.Event, 1)
	close(sentinel)
	m.waiting = true
	m.pendingStream = sentinel
	beforeEntries := len(m.entries)
	beforeMsgs := len(m.session.Messages)

	isCommand, err := command.Dispatch(&m, m.registry, "/rewind 1")
	if !isCommand {
		t.Fatal("/rewind not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("error = %v, want busy refusal", err)
	}
	if len(m.session.Messages) != beforeMsgs || len(m.entries) != beforeEntries {
		t.Error("state mutated during busy refusal")
	}
	if m.pendingStream != sentinel {
		t.Error("active stream disturbed")
	}
}

// TestRewindSaveFailureRollsBack proves persistence failure cannot fake
// success: memory and transcript return to their exact prior state.
func TestRewindSaveFailureRollsBack(t *testing.T) {
	m := newRewindTestModel(t)

	// Block persistence deterministically: the session file becomes a
	// directory, so WriteFile inside Save must fail.
	sessionPath := filepath.Join(".", ".lato", "sessions", m.session.ID+".json")
	if err := os.Remove(sessionPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessionPath, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(sessionPath); err != nil {
			t.Logf("cleanup: %v", err)
		}
	}()

	beforeEntries := rewindContent(m)
	beforeMsgs := append([]session.Message(nil), m.session.Messages...)
	beforeUpdated := m.session.UpdatedAt

	isCommand, err := command.Dispatch(&m, m.registry, "/rewind 1")
	if !isCommand {
		t.Fatal("/rewind not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "save session") {
		t.Fatalf("error = %v, want save failure", err)
	}

	// In-memory session fully restored.
	if len(m.session.Messages) != len(beforeMsgs) {
		t.Fatalf("messages after failed save = %d, want restored %d", len(m.session.Messages), len(beforeMsgs))
	}
	for i := range beforeMsgs {
		if m.session.Messages[i].Content != beforeMsgs[i].Content {
			t.Errorf("message %d altered after rollback: %q", i, m.session.Messages[i].Content)
		}
	}
	if !m.session.UpdatedAt.Equal(beforeUpdated) {
		t.Error("UpdatedAt advanced despite failed save")
	}

	// No partial transcript change and no false success.
	if rewindContent(m) != beforeEntries {
		t.Error("transcript changed despite failed save")
	}
	if strings.Contains(rewindContent(m), "Rewound") {
		t.Error("false success confirmation displayed")
	}
}

// TestRewindInvalidArgumentsThroughTUI: usage errors mutate nothing.
func TestRewindInvalidArgumentsThroughTUI(t *testing.T) {
	m := newRewindTestModel(t)
	before := len(m.session.Messages)

	for _, line := range []string{"/rewind abc", "/rewind 0", "/rewind -3", "/rewind 1 2"} {
		isCommand, err := command.Dispatch(&m, m.registry, line)
		if !isCommand || err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("%s: isCommand=%v err=%v, want usage error", line, isCommand, err)
		}
		if len(m.session.Messages) != before {
			t.Fatalf("%s mutated messages", line)
		}
	}
}

// TestRewindOverCountRefused: partial rewinds are impossible.
func TestRewindOverCountRefused(t *testing.T) {
	m := newRewindTestModel(t)

	isCommand, err := command.Dispatch(&m, m.registry, "/rewind 99")
	if !isCommand {
		t.Fatal("/rewind not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "cannot rewind 99 turns; conversation contains 3 turns") {
		t.Fatalf("error = %v, want over-count refusal", err)
	}
	if len(m.session.Messages) != 6 {
		t.Errorf("messages = %d, want untouched 6", len(m.session.Messages))
	}
}

// TestRewindNeighborsStillWork runs the sibling M3 commands against the
// same harness so no regression hides behind the new code path.
func TestRewindNeighborsStillWork(t *testing.T) {
	m := newRewindTestModel(t)

	if _, err := command.Dispatch(&m, m.registry, "/clear"); err != nil {
		t.Fatalf("/clear regressed: %v", err)
	}
	if len(m.session.Messages) != 0 {
		t.Fatal("setup: clear failed")
	}

	m.session.AddMessage("user", "fresh")
	if _, err := command.Dispatch(&m, m.registry, "/rename Fresh Start"); err != nil {
		t.Fatalf("/rename regressed: %v", err)
	}
	if m.session.Title != "Fresh Start" {
		t.Error("/rename did not apply")
	}

	if _, err := command.Dispatch(&m, m.registry, "/export fresh.md"); err != nil {
		t.Fatalf("/export regressed: %v", err)
	}
	if _, err := os.Stat("fresh.md"); err != nil {
		t.Error("/export file missing")
	}

	// /resume switches away by exact title of another stored session.
	other := session.New()
	other.Rename("Other Place")
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Dispatch(&m, m.registry, "/resume Other Place"); err != nil {
		t.Fatalf("/resume regressed: %v", err)
	}
	if m.session.Title != "Other Place" {
		t.Errorf("active session = %q", m.session.Title)
	}
}

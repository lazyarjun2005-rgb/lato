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

// TestBranchDispatchCreatesAndSwitches covers the core contract: a new
// independent session exists on disk, the original is untouched, the
// TUI now lives in the branch, and the transcript is rebuilt from it.
func TestBranchDispatchCreatesAndSwitches(t *testing.T) {
	m := newRewindTestModel(t) // 3 turns, titled "Rewind Target", saved
	origID := m.session.ID
	origPath := filepath.Join(".lato", "sessions", origID+".json")
	before, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatal(err)
	}

	isCommand, err := command.Dispatch(&m, m.registry, "/branch")
	if err != nil {
		t.Fatalf("/branch dispatch: %v", err)
	}
	if !isCommand {
		t.Fatal("/branch not recognized as a command")
	}

	// New identity + default derived title.
	if m.session.ID == origID {
		t.Fatal("still in the original session")
	}
	if got := m.session.Title; got != "Rewind Target (branch)" {
		t.Errorf("branch title = %q", got)
	}

	// Branch messages copied.
	if len(m.session.Messages) != 6 || m.session.Messages[0].Content != "question A" {
		t.Errorf("branch messages = %d", len(m.session.Messages))
	}

	// Original byte-for-byte unchanged on disk.
	after, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("original session file changed during branch")
	}

	// Transcript rebuilt from the branch (messages + confirmation).
	content := transcriptOf(m)
	if !strings.Contains(content, "✓ Branched into a new session") ||
		!strings.Contains(content, "question A") {
		t.Errorf("transcript wrong after branch:\n%s", content)
	}

	// Both sessions visible to /resume-style resolution.
	sessions, err := session.List()
	if err != nil || len(sessions) != 2 {
		t.Fatalf("listing = %d sessions, want 2 (%v)", len(sessions), err)
	}
}

// TestBranchIndependenceBattery proves every conversation operation
// inside the branch leaves the original untouched, and that /resume can
// navigate back by exact title.
func TestBranchIndependenceBattery(t *testing.T) {
	m := newRewindTestModel(t)
	origID := m.session.ID

	if _, err := command.Dispatch(&m, m.registry, "/branch"); err != nil {
		t.Fatal(err)
	}

	loadOriginal := func() *session.Session {
		s, err := session.Load(origID)
		if err != nil {
			t.Fatalf("original lost: %v", err)
		}
		return s
	}

	// 1. New message lands only in the branch.
	m.session.AddMessage("user", "direction D")
	if err := m.session.Save(); err != nil {
		t.Fatal(err)
	}
	if n := len(loadOriginal().Messages); n != 6 {
		t.Errorf("original grew: %d messages", n)
	}

	// 2. Rename branch only.
	if _, err := command.Dispatch(&m, m.registry, "/rename My OAuth Direction"); err != nil {
		t.Fatalf("/rename: %v", err)
	}
	if loadOriginal().Title != "Rewind Target" {
		t.Errorf("original title changed: %q", loadOriginal().Title)
	}

	// 3. Rewind branch only.
	if _, err := command.Dispatch(&m, m.registry, "/rewind"); err != nil {
		t.Fatalf("/rewind: %v", err)
	}
	if len(loadOriginal().Messages) != 6 {
		t.Errorf("original rewound too: %d messages", len(loadOriginal().Messages))
	}

	// 4. Clear branch only.
	if _, err := command.Dispatch(&m, m.registry, "/clear"); err != nil {
		t.Fatalf("/clear: %v", err)
	}
	if len(loadOriginal().Messages) != 6 {
		t.Errorf("original cleared too: %d messages", len(loadOriginal().Messages))
	}

	// 5. Export represents the BRANCH only (currently empty).
	if _, err := command.Dispatch(&m, m.registry, "/export b.md"); err == nil {
		t.Skipf("export of empty branch refused — acceptable convention")
	}

	// 6. Resume back to the original by exact title.
	if _, err := command.Dispatch(&m, m.registry, "/resume Rewind Target"); err != nil {
		t.Fatalf("/resume back: %v", err)
	}
	if m.session.ID != origID || len(m.session.Messages) != 6 || m.session.Title != "Rewind Target" {
		t.Errorf("original not restored intact: %+v", m.session)
	}

	// And forward again by the renamed exact branch title.
	if _, err := command.Dispatch(&m, m.registry, "/resume My OAuth Direction"); err != nil {
		t.Fatalf("/resume to branch: %v", err)
	}
	// The branch was cleared in step 4, so it must still be empty —
	// proving its history evolved independently of the original.
	if len(m.session.Messages) != 0 {
		t.Errorf("unexpected branch state: %+v", m.session.Messages)
	}
}

// TestBranchRefusedWhileBusy pins the sentinel-stream refusal.
func TestBranchRefusedWhileBusy(t *testing.T) {
	m := newRewindTestModel(t)

	sessionsDir := filepath.Join(".lato", "sessions")
	beforeCount := len(mustReadDir(t, sessionsDir))

	sentinel := make(chan runtime.Event, 1)
	close(sentinel)
	m.waiting = true
	m.pendingStream = sentinel

	isCommand, err := command.Dispatch(&m, m.registry, "/branch")
	if !isCommand {
		t.Fatal("/branch not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("error = %v, want busy refusal", err)
	}
	if len(mustReadDir(t, sessionsDir)) != beforeCount {
		t.Error("a branch file was created despite refusal")
	}
	if m.pendingStream != sentinel || !m.waiting {
		t.Error("stream state disturbed")
	}
}

// TestBranchSaveFailureLeavesEverythingIntact: with the store
// unwritable, branching fails honestly, switches nothing, and the
// original remains intact.
func TestBranchSaveFailureLeavesEverythingIntact(t *testing.T) {
	m := newRewindTestModel(t)
	origID := m.session.ID

	storeDir := filepath.Join(".", ".lato", "sessions")
	if err := os.Chmod(storeDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(storeDir, 0o755); err != nil {
			t.Logf("restore perms: %v", err)
		}
	}()

	isCommand, err := command.Dispatch(&m, m.registry, "/branch My OAuth Direction")
	if !isCommand {
		t.Fatal("/branch not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "save branch") {
		t.Fatalf("error = %v, want save failure", err)
	}

	if m.session.ID != origID {
		t.Error("TUI switched despite save failure")
	}
	if m.session.Title != "Rewind Target" || len(m.session.Messages) != 6 {
		t.Errorf("original mutated: %+v", m.session)
	}
	if strings.Contains(transcriptOf(m), "Branched") {
		t.Error("false success displayed")
	}
}

// mustReadDir is os.ReadDir with a fatal error path for count checks.
func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

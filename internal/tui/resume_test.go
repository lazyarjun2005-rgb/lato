package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/command"
	"lato/internal/session"
)

// seedResumeSessions writes two titled sessions plus one legacy
// untitled session to the isolated store and returns their IDs.
func seedResumeSessions(t *testing.T) (authID, collegeID, legacyID string) {
	t.Helper()

	auth := session.New()
	auth.Rename("Demo Authentication Project")
	auth.AddMessage("user", "auth conversation")
	if err := auth.Save(); err != nil {
		t.Fatal(err)
	}

	college := session.New()
	college.Rename("College AI Project")
	if err := college.Save(); err != nil {
		t.Fatal(err)
	}
	dup := session.New()
	dup.Title = college.Title // duplicate title: ambiguity must be refused
	if err := dup.Save(); err != nil {
		t.Fatal(err)
	}

	legacy := session.New() // pre-M3.2 shape: no title
	legacy.AddMessage("user", "legacy talk")
	if err := legacy.Save(); err != nil {
		t.Fatal(err)
	}
	return auth.ID, college.ID, legacy.ID
}

// TestRegistryContainsResume pins registration behind dispatch, /help,
// and the palette.
func TestRegistryContainsResume(t *testing.T) {
	reg := newRegistry()
	cmd, ok := reg.Lookup("resume")
	if !ok || cmd.Name() != "resume" {
		t.Fatal("/resume missing from the production registry")
	}
}

// TestResumeDispatchResolvesAndSwitches runs /resume through the REAL
// dispatcher: exact title, unique ID prefix, and legacy-by-ID all switch
// the live model to the target session.
func TestResumeDispatchResolvesAndSwitches(t *testing.T) {
	rt := newPickerTestRuntime(t)
	t.Chdir(t.TempDir())
	authID, _, legacyID := seedResumeSessions(t)

	cases := []struct{ line, wantID, wantTitle string }{
		{"/resume Demo Authentication Project", authID, "Demo Authentication Project"},
		{"/resume " + authID[:8], authID, "Demo Authentication Project"},
		{"/resume " + legacyID, legacyID, ""},
	}
	for _, tc := range cases {
		m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
		m.palette = newSlashPalette(m.registry)

		isCommand, err := command.Dispatch(&m, m.registry, tc.line)
		if err != nil {
			t.Fatalf("%s: %v", tc.line, err)
		}
		if !isCommand {
			t.Fatalf("%s not recognized as a command", tc.line)
		}
		if m.session.ID != tc.wantID {
			t.Errorf("%s: active session = %q, want %q", tc.line, m.session.ID, tc.wantID)
		}
		if m.session.Title != tc.wantTitle {
			t.Errorf("%s: title = %q, want %q", tc.line, m.session.Title, tc.wantTitle)
		}
		if !strings.Contains(transcriptOf(m), "auth conversation") && tc.wantTitle != "" {
			t.Errorf("%s: transcript not rebuilt from target session", tc.line)
		}
	}
}

// TestResumeDispatchAmbiguityAndNotFound pins the never-guess errors as
// the dispatcher would surface them.
func TestResumeDispatchAmbiguityAndNotFound(t *testing.T) {
	rt := newPickerTestRuntime(t)
	t.Chdir(t.TempDir())
	seedResumeSessions(t)

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)

	isCommand, err := command.Dispatch(&m, m.registry, "/resume College AI Project")
	if !isCommand {
		t.Fatal("/resume not recognized")
	}
	if err == nil || !strings.Contains(err.Error(), "do not guess") || !strings.Contains(err.Error(), "2 sessions match") {
		t.Fatalf("ambiguity error = %v", err)
	}

	_, err = command.Dispatch(&m, m.registry, "/resume ghost-session")
	if err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("not-found error = %v", err)
	}

	// Nothing was renamed or created while resolving (auth, college,
	// dup, legacy = 4 files).
	entries, _ := os.ReadDir(filepath.Join(".lato", "sessions"))
	if len(entries) != 4 {
		t.Errorf("session store changed during resolution: %d files", len(entries))
	}
}

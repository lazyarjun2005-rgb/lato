package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func branchFixture() *Session {
	s := New()
	s.Rename("Demo Authentication")
	s.AddMessage("user", "Build authentication")
	s.AddMessage("assistant", "done")
	s.AddMessage("user", "Add JWT")
	return s
}

// TestBranchCreatesIndependentSnapshot covers the core contract: new
// ID, fresh timestamps, copied messages, original fully untouched.
func TestBranchCreatesIndependentSnapshot(t *testing.T) {
	s := branchFixture()
	origID, origCreated, origUpdated, origTitle := s.ID, s.CreatedAt, s.UpdatedAt, s.Title
	origMsgs := append([]Message(nil), s.Messages...)

	b := s.Branch("")

	if b.ID == "" || b.ID == origID {
		t.Errorf("branch ID = %q, want a fresh unique ID", b.ID)
	}
	if !b.CreatedAt.After(origCreated) && b.CreatedAt.Equal(origCreated) {
		t.Errorf("branch CreatedAt %v should be fresh", b.CreatedAt)
	}
	if len(b.Messages) != len(origMsgs) {
		t.Fatalf("branch messages = %d, want copied %d", len(b.Messages), len(origMsgs))
	}
	for i := range origMsgs {
		if b.Messages[i].Content != origMsgs[i].Content || b.Messages[i].Role != origMsgs[i].Role {
			t.Errorf("branch message %d = %+v, want %+v", i, b.Messages[i], origMsgs[i])
		}
	}
	if b.Title != "Demo Authentication (branch)" {
		t.Errorf("default branch title = %q", b.Title)
	}

	// The ORIGINAL is completely unchanged — including UpdatedAt.
	snapshot := *s
	if snapshot.ID != origID || !snapshot.CreatedAt.Equal(origCreated) ||
		!snapshot.UpdatedAt.Equal(origUpdated) || snapshot.Title != origTitle ||
		len(snapshot.Messages) != len(origMsgs) {
		t.Error("original session was mutated by Branch")
	}
}

// TestBranchMessagesAreDeepCopied proves the two sessions never share a
// backing array: writing through one slice cannot reach the other.
func TestBranchMessagesAreDeepCopied(t *testing.T) {
	s := branchFixture()
	b := s.Branch("")

	// Mutate the branch's copy in place.
	b.Messages[0].Content = "TAMPERED"
	if s.Messages[0].Content == "TAMPERED" {
		t.Fatal("branch mutation leaked into the original")
	}

	// And the reverse direction through append.
	b.AddMessage("user", "D")
	if len(s.Messages) != 3 {
		t.Errorf("original grew with branch: %d messages", len(s.Messages))
	}
	s.AddMessage("assistant", "orig-side")
	if len(b.Messages) != 4 {
		t.Errorf("branch grew with original: %d messages", len(b.Messages))
	}
}

// TestBranchPersistsAndLists: save → reload roundtrip and listing.
func TestBranchPersistsAndLists(t *testing.T) {
	isolateSessionDir(t)

	s := branchFixture()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	b := s.Branch("")
	b.AddMessage("user", "post-branch direction")
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(b.ID)
	if err != nil {
		t.Fatalf("Load(branch): %v", err)
	}
	if reloaded.Title != "Demo Authentication (branch)" || len(reloaded.Messages) != 4 {
		t.Errorf("reloaded branch = %+v", reloaded)
	}

	origAfter, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(origAfter.Messages) != 3 || origAfter.Title != "Demo Authentication" {
		t.Errorf("original changed on disk: %+v", origAfter)
	}

	listed, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Errorf("listing = %d sessions, want both", len(listed))
	}
}

// TestBranchTitles pins every title path: default from titled original,
// explicit multi-word override, untitled-with-preview, untitled-empty,
// and legacy sessions without a Title field.
func TestBranchTitles(t *testing.T) {
	cases := []struct {
		name string
		src  *Session
		want string
	}{
		{"titled", func() *Session { s := New(); s.Title = "Demo Authentication"; return s }(),
			"Demo Authentication (branch)"},
		{"untitled with preview", func() *Session {
			s := New()
			s.AddMessage("assistant", "noise")
			s.AddMessage("user", "fix the login bug please")
			return s
		}(), "fix the login bug please (branch)"},
		{"untitled empty", New(), "(untitled) (branch)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.src.Branch("")
			if b.Title != tc.want {
				t.Errorf("title = %q, want %q", b.Title, tc.want)
			}
		})
	}

	// Explicit multi-word titles win over any default.
	src := New()
	src.Title = "Base"
	b := src.Branch("My OAuth Direction")
	if b.Title != "My OAuth Direction" {
		t.Errorf("explicit title = %q", b.Title)
	}
}

// TestBranchEmptySessionAllowed: an empty-but-valid session branches
// into an equally empty new session (documented convention).
func TestBranchEmptySessionAllowed(t *testing.T) {
	isolateSessionDir(t)

	s := New()
	b := s.Branch("")
	if len(b.Messages) != 0 || b.ID == s.ID {
		t.Fatalf("empty branch = %+v", b)
	}
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(b.ID)
	if err != nil || len(reloaded.Messages) != 0 {
		t.Fatalf("empty branch reload = %+v, %v", reloaded, err)
	}
}

// TestBranchLegacyUntitledSource: a session loaded from pre-M3.2 JSON
// (no Title key) still produces a sensible branch title.
func TestBranchLegacyUntitledSource(t *testing.T) {
	isolateSessionDir(t)

	if err := os.MkdirAll(filepath.Join(".lato", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"legacy-b","created_at":"2025-01-02T03:04:05Z","updated_at":"2025-01-02T03:04:05Z",
	  "messages":[{"role":"user","content":"investigate the flaky pipeline","time":"2025-01-02T03:04:05Z"}]}`
	if err := os.WriteFile(filepath.Join(".lato", "sessions", "legacy-b.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load("legacy-b")
	if err != nil {
		t.Fatal(err)
	}

	b := s.Branch("")
	if !strings.HasSuffix(b.Title, "(branch)") || !strings.Contains(b.Title, "investigate the flaky pipeline") {
		t.Errorf("legacy branch title = %q", b.Title)
	}
}

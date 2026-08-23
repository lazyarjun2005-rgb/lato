package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolateSessionDir points the CWD-relative session store at a temp
// directory for the duration of one test.
func isolateSessionDir(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

// TestOldSessionJSONWithoutTitleLoads pins M3.2's compatibility rule:
// pre-M3.2 session files have no "title" key and must keep loading with
// a safely empty title — no migration, no error.
func TestOldSessionJSONWithoutTitleLoads(t *testing.T) {
	isolateSessionDir(t)

	if err := os.MkdirAll(filepath.Join(".lato", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := `{
	  "id": "legacy-1",
	  "created_at": "2025-01-02T03:04:05Z",
	  "updated_at": "2025-01-02T03:04:05Z",
	  "messages": [
	    {"role": "user", "content": "hello", "time": "2025-01-02T03:04:05Z"}
	  ]
	}`
	path := filepath.Join(".lato", "sessions", "legacy-1.json")
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load("legacy-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if s.Title != "" {
		t.Errorf("title = %q, want empty for legacy files", s.Title)
	}
	if len(s.Messages) != 1 || s.Messages[0].Content != "hello" {
		t.Errorf("messages = %+v", s.Messages)
	}
}

// TestRenameUpdatesTitleAndTimestamp covers the in-memory primitive:
// title is set (trimmed), UpdatedAt moves forward.
func TestRenameUpdatesTitleAndTimestamp(t *testing.T) {
	s := New()
	before := s.UpdatedAt

	time.Sleep(2 * time.Millisecond) // timestamps are wall-clock
	s.Rename("  My Authentication Debugging  ")

	if s.Title != "My Authentication Debugging" {
		t.Errorf("title = %q, want trimmed multi-word title", s.Title)
	}
	if !s.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt = %v, want advanced past %v", s.UpdatedAt, before)
	}
}

// TestClearMessagesPreservesIdentity pins the M3.3 contract at the
// session layer: Messages reset, everything else — ID, CreatedAt,
// Title, and the non-nil empty slice shape — preserved; UpdatedAt
// advances.
func TestClearMessagesPreservesIdentity(t *testing.T) {
	s := New()
	s.Rename("Flaky test hunt")
	created := s.CreatedAt
	s.AddMessage("user", "before clear one")
	s.AddMessage("assistant", "before clear two")
	before := s.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	s.ClearMessages()

	if len(s.Messages) != 0 || s.Messages == nil {
		t.Errorf("messages = %#v, want empty non-nil slice", s.Messages)
	}
	if s.ID == "" {
		t.Error("ID lost")
	}
	if s.CreatedAt != created {
		t.Errorf("CreatedAt = %v, want unchanged %v", s.CreatedAt, created)
	}
	if s.Title != "Flaky test hunt" {
		t.Errorf("Title = %q, want preserved", s.Title)
	}
	if !s.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt = %v, want advanced past %v", s.UpdatedAt, before)
	}
}

// TestClearMessagesPersistsAcrossReload: cleared history is durable.
func TestClearMessagesPersistsAcrossReload(t *testing.T) {
	isolateSessionDir(t)

	s := New()
	s.AddMessage("user", "old question")
	s.AddMessage("assistant", "old answer")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	s.ClearMessages()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Messages) != 0 {
		t.Errorf("persisted messages after clear = %+v, want empty", reloaded.Messages)
	}
}

// TestProviderMessagesAfterClear mirrors the real request path: after a
// clear plus one new message, exactly that message reaches the model.
func TestProviderMessagesAfterClear(t *testing.T) {
	s := New()
	s.AddMessage("user", "old question")
	s.AddMessage("assistant", "old answer")

	s.ClearMessages()
	s.AddMessage("user", "fresh question")

	got := s.ProviderMessages()
	if len(got) != 1 || got[0].Role != "user" || got[0].Content != "fresh question" {
		t.Fatalf("provider messages = %+v, want only the fresh user turn", got)
	}
}

// TestRenamePersistsAcrossReload is the durability contract: rename →
// Save → Load returns the same title from disk.
func TestRenamePersistsAcrossReload(t *testing.T) {
	isolateSessionDir(t)

	s := New()
	s.AddMessage("user", "hi")
	s.Rename("Investigate flaky test")
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := Load(s.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Title != "Investigate flaky test" {
		t.Errorf("persisted title = %q", reloaded.Title)
	}
}

// TestUntitledSaveOmitsTitleKey keeps untitled files clean and proves
// omitempty behavior explicitly.
func TestUntitledSaveOmitsTitleKey(t *testing.T) {
	isolateSessionDir(t)

	s := New()
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(".lato", "sessions", s.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"title"`) {
		t.Errorf("untitled session JSON contains a title key:\n%s", raw)
	}
}

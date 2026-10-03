package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidSessionID(t *testing.T) {
	good := []string{"a1b2c3d4", "legacy-1", "550e8400-e29b-41d4-a716-446655440000", "under_score-ok"}
	for _, id := range good {
		if !validSessionID(id) {
			t.Errorf("validSessionID(%q) = false, want true", id)
		}
	}
	bad := []string{"", "..", ".", "../etc", "a/b", `a\b`, "/abs", "a\\b", "id with spaces", "id.json", "a*b"}
	for _, id := range bad {
		if validSessionID(id) {
			t.Errorf("validSessionID(%q) = true, want false", id)
		}
	}
}

func TestLoadRejectsInvalidID(t *testing.T) {
	isolateSessionDir(t)
	if _, err := Load("../../etc/passwd"); err == nil {
		t.Error("Load with traversal id should fail")
	}
	if _, err := Load(""); err == nil {
		t.Error("Load with empty id should fail")
	}
}

func TestSaveRejectsInvalidID(t *testing.T) {
	isolateSessionDir(t)
	s := New()
	s.ID = "../escape"
	if err := s.Save(); err == nil {
		t.Error("Save with traversal id should fail")
	}
}

func TestLoadRepairsTruncatedJSON(t *testing.T) {
	isolateSessionDir(t)
	dir := filepath.Join(".lato", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	full := `{"id":"trunc-1","created_at":"2025-01-02T03:04:05Z","updated_at":"2025-01-02T03:04:05Z","messages":[{"role":"user","content":"hello","time":"2025-01-02T03:04:05Z"}]}`
	truncated := full[:len(full)-2] // cut off the trailing ]}` while keeping the message body
	path := filepath.Join(dir, "trunc-1.json")
	if err := os.WriteFile(path, []byte(truncated), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Load("trunc-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if s.ID != "trunc-1" {
		t.Errorf("recovered id = %q", s.ID)
	}
	if len(s.Messages) != 1 || s.Messages[0].Content != "hello" {
		t.Errorf("recovered messages = %+v", s.Messages)
	}

	// Original corrupted bytes must be preserved.
	if _, err := os.Stat(path + ".corrupt.bak"); err != nil {
		t.Errorf("expected corrupt backup at %s", path+".corrupt.bak")
	}
	// Original file is left unchanged until a Save occurs.
	data, _ := os.ReadFile(path)
	if string(data) != truncated {
		t.Error("original corrupted file should remain unchanged after Load")
	}
}

func TestLoadRepairsTruncationMidKey(t *testing.T) {
	isolateSessionDir(t)
	dir := filepath.Join(".lato", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	full := `{"id":"trunc-2","messages":[{"role":"user","content":"hello","time":"2025-01-02T03:04:05Z"}]}`
	// Cut immediately after a key token: the dangling key must be stripped
	// and the surviving prefix recovered.
	idx := strings.Index(full, `"content"`)
	truncated := full[:idx+len(`"content"`)]
	path := filepath.Join(dir, "trunc-2.json")
	os.WriteFile(path, []byte(truncated), 0o600)

	s, err := Load("trunc-2")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if s.ID != "trunc-2" {
		t.Errorf("recovered id = %q", s.ID)
	}
	// The role survives; content was never fully written.
	if len(s.Messages) != 1 || s.Messages[0].Role != "user" {
		t.Errorf("recovered messages = %+v", s.Messages)
	}
	if _, err := os.Stat(path + ".corrupt.bak"); err != nil {
		t.Errorf("expected corrupt backup")
	}
}

func TestLoadPreservesCorruptedFile(t *testing.T) {
	isolateSessionDir(t)
	dir := filepath.Join(".lato", "sessions")
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "bad-1.json")
	os.WriteFile(path, []byte("{not valid json at all,,,"), 0o600)

	if _, err := Load("bad-1"); err == nil {
		t.Fatal("Load should fail for unrecoverable JSON")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{not valid json at all,,," {
		t.Error("corrupted file was modified")
	}
	if _, err := os.Stat(path + ".corrupt.bak"); err == nil {
		t.Error("no backup should be created for unrecoverable data")
	}
}

func TestListSkipsCorruptedFiles(t *testing.T) {
	isolateSessionDir(t)
	dir := filepath.Join(".lato", "sessions")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "ok-1.json"), []byte(`{"id":"ok-1","messages":[]}`), 0o600)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o600)

	sessions, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "ok-1" {
		t.Errorf("List() = %+v, want only ok-1", sessions)
	}
}

func TestSessionSaveLoadRoundTripAtomic(t *testing.T) {
	isolateSessionDir(t)
	s := New()
	s.AddMessage("user", "hello")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != s.ID || len(loaded.Messages) != 1 {
		t.Errorf("round trip mismatch")
	}
	// No temp files are left behind.
	entries, _ := os.ReadDir(filepath.Join(".lato", "sessions"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".lato-tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

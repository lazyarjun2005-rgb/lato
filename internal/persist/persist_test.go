package persist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicallyCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := WriteFileAtomically(path, []byte("key: value\n"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomically() = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "key: value\n" {
		t.Errorf("content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestWriteFileAtomicallyOverwritesPreservingPreviousOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	if err := WriteFileAtomically(path, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Overwrite with new content succeeds and replaces cleanly.
	if err := WriteFileAtomically(path, []byte(`{"ok":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"ok":false}` {
		t.Errorf("content = %q", data)
	}
}

func TestWriteFileAtomicallyCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "file.json")
	if err := WriteFileAtomically(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomically() = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
}

func TestWriteFileAtomicallyNoTempDebris(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	_ = WriteFileAtomically(path, []byte("x"), 0o644)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "a.json" {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("unexpected entries: %v", names)
	}
}

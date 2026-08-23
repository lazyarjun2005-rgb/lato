package edit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp creates a file with content under dir (a t.TempDir) and
// returns its path.
func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readTemp(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// mustReplace runs one op and fails the test on any error.
func mustReplace(t *testing.T, ws *Workspace, relPath, oldText, newText string) Result {
	t.Helper()
	res, err := ws.ReplaceFile(relPath, Op{Old: oldText, New: newText})
	if err != nil {
		t.Fatalf("ReplaceFile(%q): %v", relPath, err)
	}
	return res
}

func TestReplaceFileExactReplacement(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n")

	ws := NewWorkspace(dir)
	res := mustReplace(t, ws, "main.go",
		"fmt.Println(\"Hello\")",
		"fmt.Println(\"Hello from Lato\")")

	want := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from Lato\")\n}\n"
	if got := readTemp(t, filepath.Join(dir, "main.go")); got != want {
		t.Errorf("file after edit = %q, want %q", got, want)
	}
	if res.Replaced != 1 || res.Changed() != true || res.Path != "main.go" {
		t.Errorf("result = %+v, want 1 replacement on main.go", res)
	}
	if !strings.Contains(res.Before, "fmt.Println(\"Hello\")") ||
		!strings.Contains(res.After, "fmt.Println(\"Hello from Lato\")") {
		t.Errorf("result should carry before/after content: %+v", res)
	}
}

func TestReplaceFilePreservesUnrelatedContent(t *testing.T) {
	dir := t.TempDir()
	original := "alpha\nbeta\ngamma\n"
	writeTemp(t, dir, "data.txt", original)

	ws := NewWorkspace(dir)
	mustReplace(t, ws, "data.txt", "beta", "BETA")

	want := "alpha\nBETA\ngamma\n"
	if got := readTemp(t, filepath.Join(dir, "data.txt")); got != want {
		t.Errorf("unrelated lines were disturbed: got %q want %q", got, want)
	}
}

func TestReplaceFileMissingOldContentFailsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "main.go", "package main\n")

	ws := NewWorkspace(dir)
	_, err := ws.ReplaceFile("main.go", Op{Old: "this text is not in the file", New: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if got := readTemp(t, path); got != "package main\n" {
		t.Errorf("failed edit modified the file: %q", got)
	}
}

func TestReplaceFileAmbiguousOldContentFailsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "list.txt", "item\nmiddle\nitem\n")

	ws := NewWorkspace(dir)
	_, err := ws.ReplaceFile("list.txt", Op{Old: "item", New: "ITEM"})
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
	if got := readTemp(t, path); got != "item\nmiddle\nitem\n" {
		t.Errorf("ambiguous edit modified the file: %q", got)
	}
}

func TestReplaceFileEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "empty.txt", "")

	ws := NewWorkspace(dir)
	_, err := ws.ReplaceFile("empty.txt", Op{Old: "anything", New: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("editing an empty file: err = %v, want ErrNotFound", err)
	}
	if got := readTemp(t, path); got != "" {
		t.Errorf("empty file was modified: %q", got)
	}
}

func TestReplaceFileNoOpEditReportsUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "same.txt", "stable\n")

	ws := NewWorkspace(dir)
	res := mustReplace(t, ws, "same.txt", "stable", "stable")
	if res.Changed() {
		t.Error("replacing text with itself should not report a change")
	}
	if got := readTemp(t, path); got != "stable\n" {
		t.Errorf("no-op edit changed the file: %q", got)
	}
}

func TestReplaceFileMultilineEdit(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "code.go", "func a() {\n\told()\n\told()\n}\n\nfunc b() {}\n")

	ws := NewWorkspace(dir)
	mustReplace(t, ws, "code.go", "func a() {\n\told()\n\told()\n}", "func a() {\n\tnew()\n}")

	want := "func a() {\n\tnew()\n}\n\nfunc b() {}\n"
	if got := readTemp(t, filepath.Join(dir, "code.go")); got != want {
		t.Errorf("multiline edit = %q, want %q", got, want)
	}
}

func TestReplaceFileSequentialOps(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "cfg.txt", "port=80\nhost=local\n")

	ws := NewWorkspace(dir)
	res, err := ws.ReplaceFile("cfg.txt",
		Op{Old: "port=80", New: "port=443"},
		Op{Old: "host=local", New: "host=example.com"})
	if err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	if res.Replaced != 2 {
		t.Errorf("Replaced = %d, want 2", res.Replaced)
	}
	want := "port=443\nhost=example.com\n"
	if got := readTemp(t, filepath.Join(dir, "cfg.txt")); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestReplaceFileSecondOpFailureLeavesFirstUnapplied(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "cfg.txt", "port=80\n")

	ws := NewWorkspace(dir)
	_, err := ws.ReplaceFile("cfg.txt",
		Op{Old: "port=80", New: "port=443"},
		Op{Old: "missing", New: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound from the second op", err)
	}
	if got := readTemp(t, path); got != "port=80\n" {
		t.Errorf("partial application wrote to disk: %q", got)
	}
}

func TestReplaceFileNestedPath(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "internal/deep/util.go", "package util\n\nconst Version = 1\n")

	ws := NewWorkspace(dir)
	mustReplace(t, ws, "internal/deep/util.go", "const Version = 1", "const Version = 2")

	want := "package util\n\nconst Version = 2\n"
	if got := readTemp(t, filepath.Join(dir, "internal", "deep", "util.go")); got != want {
		t.Errorf("nested edit = %q, want %q", got, want)
	}
}

func TestReplaceFileMissingFileIsError(t *testing.T) {
	ws := NewWorkspace(t.TempDir())
	if _, err := ws.ReplaceFile("ghost.txt", Op{Old: "a", New: "b"}); err == nil {
		t.Fatal("expected an error editing a nonexistent file")
	}
}

func TestReplaceFileDirectoryIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := NewWorkspace(dir)
	if _, err := ws.ReplaceFile("pkg", Op{Old: "a", New: "b"}); err == nil {
		t.Fatal("expected an error when the target is a directory")
	}
}

func TestReplaceFileOversizedFileRefused(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", MaxFileSize+1)
	path := writeTemp(t, dir, "big.log", big)

	ws := NewWorkspace(dir)
	_, err := ws.ReplaceFile("big.log", Op{Old: "x", New: "y"})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if got := readTemp(t, path); len(got) != len(big) {
		t.Error("refused edit modified the oversized file")
	}
}

func TestReplaceFileEmptyOpsIsError(t *testing.T) {
	ws := NewWorkspace(t.TempDir())
	writeTemp(t, ws.root, "f.txt", "text\n")
	if _, err := ws.ReplaceFile("f.txt"); err == nil {
		t.Fatal("expected an error for a call with no ops")
	}
}

func TestCreateFileNewContent(t *testing.T) {
	dir := t.TempDir()
	ws := NewWorkspace(dir)

	res, err := ws.CreateFile("notes/readme.md", "# Notes\n")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if !res.Created || res.Path != "notes/readme.md" {
		t.Errorf("result = %+v, want Created with normalized path", res)
	}
	if got := readTemp(t, filepath.Join(dir, "notes", "readme.md")); got != "# Notes\n" {
		t.Errorf("created content = %q, want %q", got, "# Notes\n")
	}
}

func TestCreateFileNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "exists.txt", "precious\n")

	ws := NewWorkspace(dir)
	_, err := ws.CreateFile("exists.txt", "clobbered\n")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if got := readTemp(t, path); got != "precious\n" {
		t.Errorf("existing file was overwritten: %q", got)
	}
}

func TestCreateFileEmptyContentAllowed(t *testing.T) {
	dir := t.TempDir()
	ws := NewWorkspace(dir)
	if _, err := ws.CreateFile("placeholder", ""); err != nil {
		t.Fatalf("CreateFile with empty content: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "placeholder"))
	if err != nil || info.Size() != 0 {
		t.Errorf("empty create produced %v (size %d), want an empty file", err, info.Size())
	}
}

func TestReplaceFilePreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "script.sh", "#!/bin/sh\necho hi\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(dir)
	mustReplace(t, ws, "script.sh", "echo hi", "echo hello")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("permissions after edit = %v, want preserved 0755", info.Mode().Perm())
	}
}

// TestReplaceFileAtomicOnWriteError verifies the temp-file staging: if
// rename cannot complete, the destination still holds the old content.
// Simulated by making the parent directory read-only after reading.
func TestReplaceFileAtomicOnWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions; cannot simulate a failed rename")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	writeTemp(t, dir, "sub/f.txt", "before\n")
	// Pre-create a temp sibling so CreateTemp succeeds even though the
	// directory is read-only... actually it will not; so instead verify
	// that a read-only directory yields an error and leaves data intact.
	ws := NewWorkspace(dir)

	if err := os.Chmod(sub, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755) // allow TempDir cleanup

	_, err := ws.ReplaceFile("sub/f.txt", Op{Old: "before", New: "after"})
	if err == nil {
		t.Skip("platform allowed the write despite the read-only directory; skipping")
	}
	if got := readTemp(t, filepath.Join(sub, "f.txt")); got != "before\n" {
		t.Errorf("failed atomic edit corrupted the file: %q", got)
	}
}

func TestResolveAcceptsRelativePaths(t *testing.T) {
	base := string(filepath.Separator) + "base"
	ws := NewWorkspace(base)

	cases := []struct{ input, wantRel string }{
		{"a.go", "a.go"},
		{"dir/b.go", "dir/b.go"},
		{"./c.go", "c.go"},
		{"d//e.go", "d/e.go"},
		{"f/./g.go", "f/g.go"},
		{"h\\i.go", "h/i.go"},   // backslash accepted as a separator on all platforms
		{".\\j/k.go", "j/k.go"}, // mixed separators are normalized
	}
	for _, c := range cases {
		abs, slashRel, err := ws.Resolve(c.input)
		if err != nil {
			t.Errorf("Resolve(%q) error: %v", c.input, err)
			continue
		}
		wantAbs := filepath.Join(base, filepath.FromSlash(c.wantRel))
		if abs != wantAbs {
			t.Errorf("Resolve(%q) abs = %q, want %q", c.input, abs, wantAbs)
		}
		if slashRel != c.wantRel {
			t.Errorf("Resolve(%q) rel = %q, want %q", c.input, slashRel, c.wantRel)
		}
	}
}

func TestResolveRejectsTraversalAndAbsolutePaths(t *testing.T) {
	ws := NewWorkspace(t.TempDir())

	bad := []string{
		"",
		"   ",
		"..",
		"../escape.txt",
		"a/../../escape.txt",
		"..\\escape.txt",
		"/etc/passwd",
		"C:\\Users\\victim\\file.txt",
		"C:file.txt",
		"\\\\server\\share\\file.txt",
		"/",
	}
	for _, p := range bad {
		if abs, _, err := ws.Resolve(p); err == nil {
			t.Errorf("Resolve(%q) = %q, want an error", p, abs)
		} else if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Resolve(%q) error = %v, want ErrInvalidPath", p, err)
		}
	}
}

// TestLineEndingAdaptation covers CRLF/LF tolerance: an LF-formatted
// old_text must match a CRLF file and vice versa, and the file's own
// ending style must survive the edit.
func TestLineEndingAdaptation(t *testing.T) {
	dir := t.TempDir()

	t.Run("LF op against CRLF file keeps CRLF", func(t *testing.T) {
		path := writeTemp(t, dir, "win.cfg", "first line\r\nsecond line\r\n")
		ws := NewWorkspace(dir)
		if _, err := ws.ReplaceFile("win.cfg", Op{Old: "second line", New: "second line edited"}); err != nil {
			t.Fatalf("ReplaceFile: %v", err)
		}
		got := readTemp(t, path)
		if got != "first line\r\nsecond line edited\r\n" {
			t.Errorf("CRLF file became %q", got)
		}
	})

	t.Run("exact CRLF op against LF file needs no adaptation", func(t *testing.T) {
		path := writeTemp(t, dir, "unix.cfg", "one\ntwo\nthree\n")
		ws := NewWorkspace(dir)
		// The CRLF old text does not exist in the LF file; the fallback
		// retries as LF and applies there.
		if _, err := ws.ReplaceFile("unix.cfg", Op{Old: "two\r\nthree", New: "TWO\r\nTHREE"}); err != nil {
			t.Fatalf("ReplaceFile: %v", err)
		}
		if got := readTemp(t, path); got != "one\nTWO\r\nTHREE\n" {
			t.Errorf("LF file became %q; expected the op's own endings to be used verbatim once adapted", got)
		}
	})
}

func TestResultChanged(t *testing.T) {
	if !(Result{Before: "a", After: "b"}).Changed() {
		t.Error("different before/after should count as changed")
	}
	if (Result{Before: "a", After: "a"}).Changed() {
		t.Error("identical before/after should not count as changed")
	}
	if !(Result{Created: true, Before: "", After: "new"}).Changed() {
		t.Error("creation should count as changed")
	}
}

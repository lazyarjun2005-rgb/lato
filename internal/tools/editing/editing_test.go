package editing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/edit"
	"lato/internal/tools"
)

// newToolWorkspace builds a Workspace over a fresh temp directory
// containing one seeded file, mirroring how the runtime constructs the
// editing tools.
func newToolWorkspace(t *testing.T) (*edit.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return edit.NewWorkspace(dir), dir
}

func seed(t *testing.T, ws *edit.Workspace, rel, content string) {
	t.Helper()
	if _, err := ws.CreateFile(rel, content); err != nil {
		t.Fatalf("seed %s: %v", rel, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestEditFileAppliesReplacementWithDiff(t *testing.T) {
	ws, dir := newToolWorkspace(t)
	seed(t, ws, "main.go", "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n")

	tool := NewEditFile(ws)
	res, err := tool.Execute(context.Background(), map[string]any{
		"path":     "main.go",
		"old_text": "fmt.Println(\"Hello\")",
		"new_text": "fmt.Println(\"Hello from Lato\")",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Content)
	}

	want := "package main\n\nfunc main() {\n\tfmt.Println(\"Hello from Lato\")\n}\n"
	if got := read(t, filepath.Join(dir, "main.go")); got != want {
		t.Errorf("file after edit = %q, want %q", got, want)
	}
	for _, part := range []string{"edited main.go", "--- main.go", "-\tfmt.Println(\"Hello\")", "+\tfmt.Println(\"Hello from Lato\")"} {
		if !strings.Contains(res.Content, part) {
			t.Errorf("tool output missing %q:\n%s", part, res.Content)
		}
	}
}

func TestEditFileMissingOldTextIsSoftError(t *testing.T) {
	ws, dir := newToolWorkspace(t)
	seed(t, ws, "main.go", "package main\n")

	tool := NewEditFile(ws)
	res, err := tool.Execute(context.Background(), map[string]any{
		"path": "main.go", "old_text": "no such text here", "new_text": "x",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a soft error result for missing old text")
	}
	if !strings.Contains(res.Content, "could not edit main.go") ||
		!strings.Contains(res.Content, "read_repo_file") {
		t.Errorf("error result lacks recovery hint: %s", res.Content)
	}
	if got := read(t, filepath.Join(dir, "main.go")); got != "package main\n" {
		t.Errorf("failed edit modified the file: %q", got)
	}
}

func TestEditFileAmbiguousOldTextIsSoftError(t *testing.T) {
	ws, _ := newToolWorkspace(t)
	seed(t, ws, "list.txt", "item\nmid\nitem\n")

	tool := NewEditFile(ws)
	res, err := tool.Execute(context.Background(), map[string]any{
		"path": "list.txt", "old_text": "item", "new_text": "ITEM",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a soft error for ambiguous old text")
	}
	if !strings.Contains(res.Content, "surrounding lines") {
		t.Errorf("ambiguity hint missing from output: %s", res.Content)
	}
}

func TestCreateFileCreatesAndReports(t *testing.T) {
	ws, dir := newToolWorkspace(t)

	tool := NewCreateFile(ws)
	res, err := tool.Execute(context.Background(), map[string]any{
		"path": "pkg/new/file.go", "content": "package file\n",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Content)
	}
	if got := read(t, filepath.Join(dir, "pkg", "new", "file.go")); got != "package file\n" {
		t.Errorf("created content = %q", got)
	}
	if !strings.Contains(res.Content, "created pkg/new/file.go") {
		t.Errorf("output missing creation summary:\n%s", res.Content)
	}
}

func TestCreateFileOverExistingIsSoftError(t *testing.T) {
	ws, dir := newToolWorkspace(t)
	seed(t, ws, "exists.txt", "original\n")

	tool := NewCreateFile(ws)
	res, err := tool.Execute(context.Background(), map[string]any{
		"path": "exists.txt", "content": "replacement\n",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "edit_file") {
		t.Fatalf("want soft error suggesting edit_file, got: %s", res.Content)
	}
	if got := read(t, filepath.Join(dir, "exists.txt")); got != "original\n" {
		t.Errorf("create clobbered an existing file: %q", got)
	}
}

// TestToolsConfinedToWorkspace pins the safety boundary at the tool
// layer: neither tool may touch anything outside the workspace root,
// whatever separator style the model uses.
func TestToolsConfinedToWorkspace(t *testing.T) {
	ws, dir := newToolWorkspace(t)
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	if err := os.WriteFile(outside, []byte("do not touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	editTool := NewEditFile(ws)
	createTool := NewCreateFile(ws)

	attempts := []struct {
		name string
		run  func() (string, bool)
	}{
		{"edit ../outside.txt", func() (string, bool) {
			r, err := editTool.Execute(context.Background(), map[string]any{
				"path": "../outside.txt", "old_text": "do not touch", "new_text": "pwned",
			})
			return r.Content, err == nil && r.IsError
		}},
		{"edit C:\\temp\\x.txt", func() (string, bool) {
			r, err := editTool.Execute(context.Background(), map[string]any{
				"path": "C:\\temp\\x.txt", "old_text": "a", "new_text": "b",
			})
			return r.Content, err == nil && r.IsError
		}},
		{"create /tmp/lato-escape", func() (string, bool) {
			r, err := createTool.Execute(context.Background(), map[string]any{
				"path": "/tmp/lato-escape", "content": "nope",
			})
			return r.Content, err == nil && r.IsError
		}},
		{"create ..\\escape.md", func() (string, bool) {
			r, err := createTool.Execute(context.Background(), map[string]any{
				"path": "..\\escape.md", "content": "nope",
			})
			return r.Content, err == nil && r.IsError
		}},
	}

	for _, a := range attempts {
		content, ok := a.run()
		if !ok {
			t.Errorf("%s: expected a soft-error result, got: %q (err path)", a.name, content)
		}
	}

	if got := read(t, outside); got != "do not touch\n" {
		t.Errorf("file outside the workspace was modified: %q", got)
	}
	if _, err := os.Stat("/tmp/lato-escape"); err == nil {
		os.Remove("/tmp/lato-escape")
		t.Error("create_file wrote to an absolute path outside the workspace")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.md")); err == nil {
		t.Error("create_file escaped via a parent-relative path")
	}
}

func TestMissingRequiredArgsAreHardErrors(t *testing.T) {
	ws, _ := newToolWorkspace(t)

	cases := []struct {
		name string
		run  func() error
	}{
		{"edit without path", func() error {
			_, err := NewEditFile(ws).Execute(context.Background(), map[string]any{"old_text": "a", "new_text": "b"})
			return err
		}},
		{"edit without old_text", func() error {
			_, err := NewEditFile(ws).Execute(context.Background(), map[string]any{"path": "f", "new_text": "b"})
			return err
		}},
		{"edit without new_text", func() error {
			_, err := NewEditFile(ws).Execute(context.Background(), map[string]any{"path": "f", "old_text": "a"})
			return err
		}},
		{"create without content", func() error {
			_, err := NewCreateFile(ws).Execute(context.Background(), map[string]any{"path": "f"})
			return err
		}},
	}
	for _, c := range cases {
		if err := c.run(); err == nil {
			t.Errorf("%s: expected a Go-level argument error", c.name)
		}
	}
}

func TestRegisterAddsBothEditingTools(t *testing.T) {
	ws, dir := newToolWorkspace(t)
	m := tools.NewManager(tools.NewRegistry())
	if err := Register(m, ws); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	names := map[string]bool{}
	for _, d := range m.Definitions() {
		names[d.Name] = true
	}
	if !names["edit_file"] || !names["create_file"] {
		t.Errorf("registered tools = %v, want edit_file and create_file", names)
	}

	// End-to-end through the manager, exactly as the runtime invokes it.
	res, err := m.Execute(context.Background(), "create_file", map[string]any{
		"path": "via-manager.txt", "content": "hi\n",
	})
	if err != nil || res.IsError {
		t.Fatalf("manager execute failed: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Content, "created via-manager.txt") {
		t.Errorf("manager output missing summary:\n%s", res.Content)
	}
	if got := read(t, filepath.Join(dir, "via-manager.txt")); got != "hi\n" {
		t.Errorf("file via manager = %q", got)
	}
}

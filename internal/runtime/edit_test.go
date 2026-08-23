package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/index"
	"lato/internal/workspace"
)

// newEditTestRuntime builds a Runtime pointed at a real temp workspace
// with the editing tools registered against it. No provider is needed:
// tool execution goes through the manager directly.
func newEditTestRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	dir := t.TempDir()

	rt := newTestRuntime(&scriptedProvider{})
	rt.workspace = workspace.DiscoverDir(dir)
	if err := rt.RegisterEditTools(); err != nil {
		t.Fatalf("RegisterEditTools: %v", err)
	}
	return rt, dir
}

func TestEditToolsRegisteredOnRuntime(t *testing.T) {
	rt, _ := newEditTestRuntime(t)

	names := map[string]bool{}
	for _, d := range rt.manager.Definitions() {
		names[d.Name] = true
	}
	for _, want := range []string{"edit_file", "create_file"} {
		if !names[want] {
			t.Errorf("runtime tools missing %q (have %v)", want, names)
		}
	}
}

func TestRuntimeCreateFileEndToEnd(t *testing.T) {
	rt, dir := newEditTestRuntime(t)

	res, err := rt.manager.Execute(context.Background(), "create_file", map[string]any{
		"path":    "cmd/app/main.go",
		"content": "package main\n\nfunc main() {}\n",
	})
	if err != nil || res.IsError {
		t.Fatalf("create_file failed: res=%+v err=%v", res, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "cmd", "app", "main.go"))
	if err != nil {
		t.Fatalf("created file unreadable: %v", err)
	}
	if !strings.Contains(string(got), "func main()") {
		t.Errorf("created file content = %q", got)
	}
}

func TestRuntimeEditFileEndToEnd(t *testing.T) {
	rt, dir := newEditTestRuntime(t)
	seed := filepath.Join(dir, "greet.go")
	if err := os.WriteFile(seed, []byte("package main\n\nconst Greeting = \"Hello from Lato test\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := rt.manager.Execute(context.Background(), "edit_file", map[string]any{
		"path":     "greet.go",
		"old_text": "\"Hello from Lato test\"",
		"new_text": "\"Hello from Lato\"",
	})
	if err != nil || res.IsError {
		t.Fatalf("edit_file failed: res=%+v err=%v", res, err)
	}
	raw, _ := os.ReadFile(seed)
	if !strings.Contains(string(raw), `"Hello from Lato"`) {
		t.Errorf("edit did not apply: %q", raw)
	}
	if !strings.Contains(res.Content, "--- greet.go") {
		t.Errorf("edit result missing diff:\n%s", res.Content)
	}
}

// TestEditInvalidatesCachedIndex pins the invalidation contract: search
// must reflect post-edit content without any manual /index refresh.
func TestEditInvalidatesCachedIndex(t *testing.T) {
	rt, dir := newEditTestRuntime(t)
	mainGo := filepath.Join(dir, "main.go")
	original := "package main\n\nfunc main() {\n\tfmt.Println(\"NeedleBefore\")\n}\n"
	if err := os.WriteFile(mainGo, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Warm the index cache with the pre-edit content.
	before := rt.Index()
	if f, ok := before.Lookup("main.go"); !ok || !strings.Contains(f.Body, "NeedleBefore") {
		t.Fatal("pre-edit index does not contain the seeded content")
	}

	res, err := rt.manager.Execute(context.Background(), "edit_file", map[string]any{
		"path":     "main.go",
		"old_text": "NeedleBefore",
		"new_text": "NeedleAfter",
	})
	if err != nil || res.IsError {
		t.Fatalf("edit_file failed: res=%+v err=%v", res, err)
	}

	after := rt.Index()
	if after == before {
		t.Fatal("cached index was reused across an edit; expected invalidation")
	}
	f, ok := after.Lookup("main.go")
	if !ok || !strings.Contains(f.Body, "NeedleAfter") {
		t.Errorf("rebuilt index shows stale content: %+v", f)
	}

	found, err := rt.Search(index.Search{Query: "NeedleAfter", Contents: true, Max: 5})
	if err != nil || found.Count == 0 {
		t.Errorf("post-edit search failed: count=%d err=%v", found.Count, err)
	}
}

// TestEditOutsideWorkspaceIsRejected verifies the runtime-registered
// tools inherit the workspace confinement.
func TestEditOutsideWorkspaceIsRejected(t *testing.T) {
	rt, dir := newEditTestRuntime(t)

	sibling := filepath.Join(filepath.Dir(dir), "sibling-secret.txt")
	if err := os.WriteFile(sibling, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(sibling)

	res, err := rt.manager.Execute(context.Background(), "edit_file", map[string]any{
		"path":     "../sibling-secret.txt",
		"old_text": "secret",
		"new_text": "owned",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("edit outside the workspace must fail, got: %s", res.Content)
	}
	raw, _ := os.ReadFile(sibling)
	if string(raw) != "secret\n" {
		t.Errorf("outside file was modified: %q", raw)
	}
}

// TestEditHookNotFiredOnNoOp makes sure a no-op replacement doesn't
// needlessly drop the cached index.
func TestEditHookNotFiredOnNoOp(t *testing.T) {
	rt, dir := newEditTestRuntime(t)
	path := filepath.Join(dir, "stable.txt")
	os.WriteFile(path, []byte("same\n"), 0o644)

	first := rt.Index()
	res, err := rt.manager.Execute(context.Background(), "edit_file", map[string]any{
		"path": "stable.txt", "old_text": "same", "new_text": "same",
	})
	if err != nil || res.IsError {
		t.Fatalf("no-op edit failed: res=%+v err=%v", res, err)
	}
	if rt.Index() != first {
		t.Error("no-op edit invalidated the index; only real writes should")
	}
}

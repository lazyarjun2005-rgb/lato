package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBoundaryContainsWorkspaceChildren(t *testing.T) {
	root := t.TempDir()
	b := NewBoundary(root)

	cases := []string{
		"main.go",
		"src/helper.go",
		"./src/helper.go",
		"src/nested/deep/file.txt",
		root,                       // the root itself
		root + "/main.go",          // absolute inside
		filepath.Join(root, "src"), // OS-native join
	}
	for _, p := range cases {
		if _, ok := b.Contains(p); !ok {
			t.Errorf("Contains(%q) = false, want true", p)
		}
	}
}

func TestBoundaryBlocksEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	b := NewBoundary(root)

	cases := []string{
		"../outside.txt",
		"../../something",
		"src/../../escape.go",
		outside + "/file.txt",
		filepath.Dir(root) + "/sibling.txt",
		"/etc/passwd",
		"/",
		"C:/Windows/system32",
		"C:\\Users\\x",
		"",
		"   ",
	}
	for _, p := range cases {
		if abs, ok := b.Contains(p); ok {
			t.Errorf("Contains(%q) = (%q, true), want false", p, abs)
		}
	}
}

func TestBoundaryNonexistentChildAllowed(t *testing.T) {
	root := t.TempDir()
	b := NewBoundary(root)

	// Files about to be created must be judged by their deepest
	// existing ancestor, so new nested paths are legitimate children.
	if _, ok := b.Contains("new/dir/created-by-lato.txt"); !ok {
		t.Fatal("nonexistent workspace child rejected")
	}
	if _, ok := b.Contains("../new-parent/file.txt"); ok {
		t.Fatal("nonexistent path outside the workspace accepted")
	}
}

// TestBoundaryRejectsUNCPaths pins the M14 Windows hardening: network
// share paths are never treated as workspace-relative content, in
// either their native backslash or folded slash form.
func TestBoundaryRejectsUNCPaths(t *testing.T) {
	b := NewBoundary(t.TempDir())

	for _, p := range []string{
		`\\host\share\file.txt`,
		`\\.\C:\some\device\path`,
		"//host/share/file.txt",
		"//server/quota",
	} {
		if abs, ok := b.Contains(p); ok {
			t.Errorf("Contains(%q) = (%q, true), want false", p, abs)
		}
	}
}

// TestBoundarySiblingPrefixConfusion verifies canonical containment,
// not string prefixes: a workspace at /base/proj must never claim
// paths under its sibling /base/proj-other.
func TestBoundarySiblingPrefixConfusion(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "proj")
	sibling := filepath.Join(base, "proj-other")
	for _, d := range []string{root, sibling} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	b := NewBoundary(root)

	outside := filepath.Join(sibling, "secret.txt")
	if _, ok := b.Contains(outside); ok {
		t.Errorf("absolute sibling path %q accepted", outside)
	}
	if _, ok := b.Contains("../proj-other/secret.txt"); ok {
		t.Error("relative traversal into a same-prefixed sibling accepted")
	}
	if _, ok := b.Contains(`..\proj-other\secret.txt`); ok {
		t.Error("backslash traversal into a same-prefixed sibling accepted")
	}
	// The workspace itself keeps working.
	if _, ok := b.Contains("proj-file.txt"); !ok {
		t.Error("own workspace child rejected while testing siblings")
	}
}

// TestBoundaryWindowsStyleFormsOnAnyOS exercises Windows path syntax
// against the boundary rules that apply identically everywhere.
func TestBoundaryWindowsStyleFormsOnAnyOS(t *testing.T) {
	b := NewBoundary(t.TempDir())

	cases := []struct {
		path string
		want bool
	}{
		{`src\main.go`, true},          // backslash separators fold
		{`src\nested\new.txt`, true},   // nested + nonexistent
		{`..\outside.txt`, false},      // backslash traversal
		{`C:\Windows\system32`, false}, // drive letter
		{`c:/windows/system32`, false}, // lowercase drive letter
		{`\abs\from\root`, false},      // rooted absolute form
	}
	for _, c := range cases {
		if _, got := b.Contains(c.path); got != c.want {
			t.Errorf("Contains(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestBoundaryFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	b := NewBoundary(root)

	insideTarget := filepath.Join(root, "real.txt")
	if err := os.WriteFile(insideTarget, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkInside := filepath.Join(root, "link-inside")
	if err := os.Symlink(insideTarget, linkInside); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	outsideTarget := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideTarget, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkOutside := filepath.Join(root, "link-outside")
	if err := os.Symlink(outsideTarget, linkOutside); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, ok := b.Contains("link-inside"); !ok {
		t.Error("symlink to a workspace file was rejected")
	}
	if _, ok := b.Contains("link-outside"); ok {
		t.Error("symlink escaping the workspace was accepted")
	}

	// A symlinked directory pointing out must not become a traversal base.
	dirOutside := filepath.Join(outside, "dir")
	if err := os.Mkdir(dirOutside, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "dirlink")
	if err := os.Symlink(dirOutside, linkDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, ok := b.Contains("dirlink/file.txt"); ok {
		t.Error("path through an outward directory symlink was accepted")
	}
}

func TestBoundaryRootCanonicalization(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// Trailing separators and dot segments must not create a second,
	// looser boundary identity.
	b1 := NewBoundary(root)
	b2 := NewBoundary(root + string(filepath.Separator) + "." + string(filepath.Separator))
	if b1.Root() != b2.Root() {
		t.Fatalf("roots differ: %q vs %q", b1.Root(), b2.Root())
	}

	// Relative root input is anchored at the process working directory;
	// both forms must still agree on containment of children.
	if _, ok := b1.Contains("anything.txt"); !ok {
		t.Error("child of canonical root rejected")
	}
}

func TestBackslashSeparatorsFold(t *testing.T) {
	root := t.TempDir()
	b := NewBoundary(root)

	if _, ok := b.Contains(`src\main.go`); !ok {
		t.Error("backslash-separated relative path rejected")
	}
	if _, ok := b.Contains(`..\..\escape.txt`); ok {
		t.Error("backslash traversal accepted")
	}
}

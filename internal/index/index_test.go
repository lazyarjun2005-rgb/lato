package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree creates files (and parent directories) at the given
// slash-separated relative paths under a fresh temp dir, returning it.
// It is platform-neutral: filepath.FromSlash converts on Windows.
func writeTree(t *testing.T, name string, paths ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	for _, p := range paths {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("placeholder"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildEmptyRepository covers an empty workspace root: no files and
// no errors.
func TestBuildEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	files, stats := Build(dir)
	if len(files) != 0 {
		t.Errorf("files = %d, want 0", len(files))
	}
	if stats.Files != 0 || stats.Directories != 0 || stats.Symbols != 0 {
		t.Errorf("stats = %+v, want zeroed", stats)
	}
}

// TestBuildGoProject covers a typical Go layout: go.mod, a main package,
// an internal package, README, and a nested file. It verifies metadata,
// language, symbols, and determinism.
func TestBuildGoProject(t *testing.T) {
	dir := writeTree(t, "repo", "go.mod", "main.go", "internal/app/app.go")
	writeFile(t, dir, "go.mod", "module github.com/acme/demo\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", `package main

func main() { _ = helper() }
`)
	writeFile(t, dir, "internal/app/app.go", `package app

type Service struct{}

func (s Service) Serve() {}

func helper() {}
`)

	files, stats := Build(dir)

	if stats.Files != 3 {
		t.Errorf("stats.Files = %d, want 3 (go.mod, main.go, app.go)", stats.Files)
	}
	if !containsLang(stats.Languages, "Go") {
		t.Errorf("Languages = %v, want Go present", stats.Languages)
	}
	if stats.Symbols == 0 {
		t.Error("stats.Symbols = 0, want extracted Go symbols")
	}

	// Determinism: rebuilding yields the same file set.
	files2, _ := Build(dir)
	if len(files2) != len(files) {
		t.Errorf("rebuild produced %d files, want %d", len(files2), len(files))
	}
	for i := range files {
		if files[i].Path != files2[i].Path {
			t.Errorf("rebuild file %d = %q, want %q (deterministic)", i, files2[i].Path, files[i].Path)
		}
	}

	// Symbol kinds are present and correct for the Go files.
	app := lookupByPath(t, files, "internal/app/app.go")
	if app == nil {
		t.Fatal("internal/app/app.go not indexed")
	}
	if app.Lang != "Go" {
		t.Errorf("app.Lang = %q, want Go", app.Lang)
	}
	if !hasSymbolKind(app, "struct", "Service") {
		t.Errorf("app.go symbols missing struct Service: %v", app.Symbols)
	}
	if !hasSymbolKind(app, "method", "Serve") {
		t.Errorf("app.go symbols missing method Serve: %v", app.Symbols)
	}
	if !hasSymbolKind(app, "function", "helper") {
		t.Errorf("app.go symbols missing function helper: %v", app.Symbols)
	}
	if len(app.Packages) == 0 || app.Packages[0] != "app" {
		t.Errorf("app.Packages = %v, want [app]", app.Packages)
	}
}

// TestBuildNestedDirectories verifies that nested paths produce
// slash-separated relative paths and a correct directory count.
func TestBuildNestedDirectories(t *testing.T) {
	dir := writeTree(t, "nested",
		"a/one.go", "a/b/two.go", "a/b/c/three.go", "top.go")
	for _, f := range []string{"a/one.go", "a/b/two.go", "a/b/c/three.go"} {
		writeFile(t, dir, f, "package p\n")
	}

	files, stats := Build(dir)

	if stats.Files != 4 {
		t.Errorf("stats.Files = %d, want 4", stats.Files)
	}
	// a, a/b, a/b/c => 3 directories entered.
	if stats.Directories != 3 {
		t.Errorf("stats.Directories = %d, want 3", stats.Directories)
	}
	wantPaths := []string{"a/b/c/three.go", "a/b/two.go", "a/one.go", "top.go"}
	if len(files) != len(wantPaths) {
		t.Fatalf("indexed %d files, want %d", len(files), len(wantPaths))
	}
	for i, want := range wantPaths {
		if files[i].Path != want {
			t.Errorf("files[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
}

// TestBuildIgnoresVendorAndGit verifies that default ignored directories
// are skipped and reported, while their containing files never appear.
func TestBuildIgnoresVendorAndGit(t *testing.T) {
	dir := writeTree(t, "ignored",
		"main.go", "vendor/dep/dep.go", "node_modules/pkg/index.js", "dist/bundle.js", ".git/config")
	writeFile(t, dir, "main.go", "package main\n")

	files, stats := Build(dir)

	if stats.Files != 1 {
		t.Errorf("stats.Files = %d, want 1 (only main.go)", stats.Files)
	}
	for _, f := range files {
		if strings.Contains(f.Path, "vendor") || strings.Contains(f.Path, "node_modules") ||
			strings.Contains(f.Path, "dist") || strings.Contains(f.Path, ".git") {
			t.Errorf("ignored file indexed: %q", f.Path)
		}
	}
	if len(stats.SkippedDirs) == 0 {
		t.Error("SkippedDirs empty, want the ignored directories listed")
	}
}

// TestBuildRespectsGitignore verifies that a root .gitignore controls
// which files are indexed, with basic negation and directory rules.
func TestBuildRespectsGitignore(t *testing.T) {
	dir := writeTree(t, "gitignored", ".gitignore", "keep.go", "skip/secret.go", "skip/log.txt", "build.out")
	writeFile(t, dir, ".gitignore", "skip/\n/build.out\n")
	writeFile(t, dir, "keep.go", "package p\n")

	files, stats := Build(dir)

	if stats.Files != 1 {
		t.Errorf("stats.Files = %d, want 1 (skip/ and build.out ignored)", stats.Files)
	}
	for _, f := range files {
		if f.Path == "skip/secret.go" || f.Path == "skip/log.txt" || f.Path == "build.out" {
			t.Errorf("gitignored file indexed: %q", f.Path)
		}
	}
}

// TestFileMetadata verifies the per-file metadata fields.
func TestFileMetadata(t *testing.T) {
	dir := writeTree(t, "meta", "main.go")
	writeFile(t, dir, "main.go", "package main\n")

	files, _ := Build(dir)
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	f := files[0]
	if f.Path != "main.go" || f.Name != "main.go" {
		t.Errorf("Path/Name = %q/%q", f.Path, f.Name)
	}
	if f.Ext != ".go" {
		t.Errorf("Ext = %q, want .go", f.Ext)
	}
	if f.Lang != "Go" {
		t.Errorf("Lang = %q, want Go", f.Lang)
	}
	if f.Size != int64(len("package main\n")) {
		t.Errorf("Size = %d, want %d", f.Size, len("package main\n"))
	}
	if f.Binary {
		t.Error("main.go marked binary")
	}
	if len(f.Body) == 0 {
		t.Error("main.go Body empty")
	}
}

// TestBinaryDetection verifies binary files are listed but not read as
// text.
func TestBinaryDetection(t *testing.T) {
	dir := writeTree(t, "bin", "logo.png")
	full := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(full, []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}

	files, _ := Build(dir)
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	if !files[0].Binary {
		t.Error("logo.png should be marked binary")
	}
	if files[0].Body != "" {
		t.Error("binary file must not keep a text Body")
	}
}

// TestLargeFileBoundedBody verifies an over-sized file is still listed,
// keeps only a bounded prefix of its text, and is flagged Truncated so
// search knows to stream the rest from disk.
func TestLargeFileBoundedBody(t *testing.T) {
	dir := writeTree(t, "big", "huge.txt")
	huge := strings.Repeat("x\n", maxTextBytes) // 2 × maxTextBytes > maxTextBytes
	writeFile(t, dir, "huge.txt", huge)

	files, stats := Build(dir)
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	f := files[0]
	if f.Body == "" {
		t.Fatal("over-max file should retain a bounded text prefix")
	}
	if int64(len(f.Body)) > maxTextBytes {
		t.Errorf("body length = %d, want at most %d", len(f.Body), maxTextBytes)
	}
	if !f.Truncated {
		t.Error("over-max file must be marked Truncated")
	}
	if f.Binary {
		t.Error("text file over the size bound must not be marked binary")
	}
	if stats.Files != 1 {
		t.Errorf("stats.Files = %d, want 1 (listed with bounded content)", stats.Files)
	}
}

// TestGoInterfaceDetection verifies interface symbols are extracted.
func TestGoInterfaceDetection(t *testing.T) {
	dir := writeTree(t, "iface", "iface.go")
	writeFile(t, dir, "iface.go", `package p

type Storer interface {
	Store() error
}

type Generic[T any] struct{}
`)
	files, _ := Build(dir)
	f := lookupByPath(t, files, "iface.go")
	if f == nil {
		t.Fatal("iface.go not indexed")
	}
	if !hasSymbolKind(f, "interface", "Storer") {
		t.Errorf("missing interface Storer: %v", f.Symbols)
	}
	if !hasSymbolKind(f, "struct", "Generic") {
		t.Errorf("missing struct Generic (generic type): %v", f.Symbols)
	}
}

// TestWindowsSafePaths verifies that path handling is separator-agnostic:
// relative paths are reported with forward slashes and absolute paths are
// resolvable regardless of OS separator conventions.
func TestWindowsSafePaths(t *testing.T) {
	dir := writeTree(t, "win", "a/b/c.go")
	writeFile(t, dir, "a/b/c.go", "package p\n")

	files, _ := Build(dir)
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	// The relative path must use forward slashes on every platform.
	if files[0].Path != "a/b/c.go" {
		t.Errorf("Path = %q, want a/b/c.go (forward slashes)", files[0].Path)
	}

	// Absolute-path handling: index built from an absolute path keeps a
	// forward-slash relative path and an absolute Root.
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	idx := NewBuilder(abs).Build()
	if idx.Info.Root == "" {
		t.Error("absolute root not captured")
	}
	if f, ok := idx.Lookup("a/b/c.go"); !ok || f.Name != "c.go" {
		t.Errorf("Lookup with slash path failed: %+v, %v", f, ok)
	}
}

func containsLang(m map[string]int, lang string) bool {
	_, ok := m[lang]
	return ok
}

func lookupByPath(t *testing.T, files []File, path string) *File {
	t.Helper()
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	return nil
}

func hasSymbolKind(f *File, kind, name string) bool {
	for _, s := range f.Symbols {
		if s.Kind == kind && s.Name == name {
			return true
		}
	}
	return false
}

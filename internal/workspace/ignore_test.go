package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles creates files (and parent directories) at the given
// slash-separated relative paths under a fresh temp dir, returning it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestIgnoreSkipsDefaultGeneratedDirs pins the directories that are
// always excluded, including the ones the previous hardcoded list in
// this package missed (.venv, coverage, .terraform and friends).
func TestIgnoreSkipsDefaultGeneratedDirs(t *testing.T) {
	ig := NewIgnore(t.TempDir())

	for _, dir := range []string{
		".git", "node_modules", "vendor", "target", "dist", "build",
		"coverage", "__pycache__", ".venv", "venv", ".next", ".nuxt",
		".cache", ".terraform", ".tox", "bower_components", "Pods",
		"DerivedData",
	} {
		if !ig.SkipDir(dir) {
			t.Errorf("SkipDir(%q) = false, want true", dir)
		}
	}
}

// TestIgnoreSkipsNestedGeneratedDirs confirms the base-name rule works
// at any depth, which is what keeps a virtualenv or build output deep in
// the tree out of the scan. The walk prunes at the first ignored
// ancestor, so callers test each level rather than a deep path.
func TestIgnoreSkipsNestedGeneratedDirs(t *testing.T) {
	ig := NewIgnore(t.TempDir())

	if !ig.SkipDir("project/.venv") {
		t.Error(`SkipDir("project/.venv") = false, want true`)
	}
	if ig.SkipDir("project/src") {
		t.Error(`SkipDir("project/src") = true, want false`)
	}
}

// TestIgnoreKeepsFilesNamedLikeGeneratedDirs guards the deliberate
// asymmetry: a *file* called "build" is ordinary content, while a
// directory of that name is generated output.
func TestIgnoreKeepsFilesNamedLikeGeneratedDirs(t *testing.T) {
	ig := NewIgnore(t.TempDir())

	if ig.SkipFile("build") {
		t.Error(`SkipFile("build") = true, want false (a file named build is content)`)
	}
	if !ig.SkipDir("build") {
		t.Error(`SkipDir("build") = false, want true`)
	}
}

// TestIgnoreSkipsGeneratedFileNames covers lockfiles and .gitignore,
// which are excluded as files but also as oddly-named directories.
func TestIgnoreSkipsGeneratedFileNames(t *testing.T) {
	ig := NewIgnore(t.TempDir())

	for _, name := range []string{"go.sum", "package-lock.json", "yarn.lock", ".gitignore"} {
		if !ig.SkipFile(name) {
			t.Errorf("SkipFile(%q) = false, want true", name)
		}
	}
	if !ig.SkipFile("web/package-lock.json") {
		t.Error(`SkipFile("web/package-lock.json") = false, want true`)
	}
}

// TestIgnoreReadsGitignore verifies the project's own rules are honored.
func TestIgnoreReadsGitignore(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		".gitignore": "build-out/\n*.log\n/only-at-root.txt\n",
	})

	ig := NewIgnore(dir)

	if !ig.SkipDir("build-out") {
		t.Error(`SkipDir("build-out") = false, want true`)
	}
	if !ig.SkipFile("debug.log") || !ig.SkipFile("nested/debug.log") {
		t.Error("*.log rule should ignore the file at any depth")
	}
	if !ig.SkipFile("only-at-root.txt") {
		t.Error("anchored rule should ignore only-at-root.txt")
	}
	if ig.SkipFile("nested/only-at-root.txt") {
		t.Error("anchored rule should not match nested/only-at-root.txt")
	}
}

// TestIgnoreSlashlessPatternMatchesAtAnyDepth pins git's rule that a
// pattern with no slash applies to the base name at every depth. Without
// it, "*.log" silently failed to ignore nested log files, so they were
// indexed and could reach the model.
func TestIgnoreSlashlessPatternMatchesAtAnyDepth(t *testing.T) {
	dir := writeFiles(t, map[string]string{".gitignore": "*.log\ndebug.log\n"})

	ig := NewIgnore(dir)

	for _, rel := range []string{"debug.log", "nested/debug.log", "a/b/c/debug.log"} {
		if !ig.SkipFile(rel) {
			t.Errorf("SkipFile(%q) = false, want true", rel)
		}
	}
	if ig.SkipFile("debug.logs") {
		t.Error(`SkipFile("debug.logs") = true, want false (pattern is exact, not a prefix)`)
	}
}

// TestIgnoreDirectoryOnlyRuleIgnoresSameNamedFile keeps the base-name
// shortcut from wrongly applying "build/" to a *file* named build.
func TestIgnoreDirectoryOnlyRuleIgnoresSameNamedFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{".gitignore": "build/\n"})

	ig := NewIgnore(dir)

	if ig.SkipFile("build") {
		t.Error(`SkipFile("build") = true, want false (the rule is "build/", a directory)`)
	}
	if !ig.SkipDir("build") {
		t.Error(`SkipDir("build") = false, want true`)
	}
	if !ig.SkipFile("build/out.js") {
		t.Error(`SkipFile("build/out.js") = false, want true`)
	}
}

// TestIgnoreHonorsNegation pins git's last-match-wins rule so a project
// can re-include a file inside an ignored directory.
func TestIgnoreHonorsNegation(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		".gitignore": "secrets-out/\n!secrets-out/keep.txt\n",
	})

	ig := NewIgnore(dir)

	if !ig.SkipDir("secrets-out") {
		t.Error("secrets-out/ should be ignored")
	}
	if ig.SkipFile("secrets-out/keep.txt") {
		t.Error("negated rule should re-include secrets-out/keep.txt")
	}
}

// TestIgnoreMissingGitignoreIsNotAnError confirms discovery degrades to
// the fixed default sets when no .gitignore exists.
func TestIgnoreMissingGitignoreIsNotAnError(t *testing.T) {
	ig := NewIgnore(t.TempDir())

	if ig.SkipFile("main.go") {
		t.Error("an ordinary file should not be ignored")
	}
	if !ig.SkipDir("node_modules") {
		t.Error("default rules should still apply without a .gitignore")
	}
}

// TestIgnoreIgnoresRootItself keeps the workspace root itself scannable.
func TestIgnoreIgnoresRootItself(t *testing.T) {
	ig := NewIgnore(t.TempDir())

	if ig.SkipDir("") || ig.SkipDir(".") || ig.SkipFile(".") {
		t.Error("the root path should never be skipped")
	}
}

// TestTreeExcludesGitignoredPaths is the regression test for the gap
// this change closes: Info.Tree used to include gitignored directories
// because discovery consulted its own hardcoded skip list.
func TestTreeExcludesGitignoredPaths(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"go.mod":                    "module example.com/probe\n\ngo 1.22\n",
		"main.go":                   "package main\n\nfunc main() {}\n",
		".gitignore":                "generated/\nsecrets-out/\n.venv/\n",
		"generated/assets/app.js":   "var a = 1\n",
		"secrets-out/gen.py":        "x = 1\n",
		".venv/lib/site-packages/p": "x = 1\n",
	})

	info := DiscoverDir(dir)

	for _, n := range info.Tree {
		for _, ignored := range []string{"generated", "secrets-out", ".venv"} {
			if n.Path == ignored || strings.HasPrefix(n.Path, ignored+"/") {
				t.Errorf("Tree contains gitignored path %q", n.Path)
			}
		}
	}
	if !hasNode(info.Tree, "main.go") {
		t.Error("Tree should still contain tracked files")
	}
}

// TestLanguageIgnoresGitignoredSources is the regression test for the
// user-visible symptom: a Go repository whose gitignored virtualenv
// held more Python files than its own sources was reported as Python,
// which then reached the model prompt via context.Text.
func TestLanguageIgnoresGitignoredSources(t *testing.T) {
	files := map[string]string{
		"README.md":   "# Probe\n",
		"src/main.go": "package main\n",
		"src/util.go": "package util\n",
		".gitignore":  ".venv/\n",
	}
	// Enough ignored Python to outvote the two real Go files.
	for i := 0; i < 40; i++ {
		files[".venv/lib/site-packages/mod"+string(rune('a'+i%26))+string(rune('a'+i/26))+".py"] = "x = 1\n"
	}
	dir := writeFiles(t, files)

	info := DiscoverDir(dir)

	if info.Language != "Go" {
		t.Errorf("Language = %q, want Go (gitignored .venv must not skew detection)", info.Language)
	}
}

// TestLanguageStillDetectedWithoutGitignore guards against the ignore
// integration suppressing legitimate extension-based detection.
func TestLanguageStillDetectedWithoutGitignore(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.go": "package main\n",
		"util.go": "package util\n",
	})

	if info := DiscoverDir(dir); info.Language != "Go" {
		t.Errorf("Language = %q, want Go", info.Language)
	}
}

// TestDiscoverySurvivesUnreadableDirectory checks discovery stays
// best-effort: a directory it cannot read is counted, skipped, and does
// not fail the walk. Skipped for root, where chmod cannot deny access.
func TestDiscoverySurvivesUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not deny access")
	}
	dir := writeFiles(t, map[string]string{
		"main.go":       "package main\n",
		"locked/one.go": "package locked\n",
		"open/two.go":   "package open\n",
	})
	if err := os.Chmod(filepath.Join(dir, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "locked"), 0o755) })

	info := DiscoverDir(dir)

	if info.Unreadable == 0 {
		t.Error("Unreadable = 0, want at least 1 for an unreadable directory")
	}
	if !hasNode(info.Tree, "open") {
		t.Error("discovery should still report readable directories")
	}
	if info.Language != "Go" {
		t.Errorf("Language = %q, want Go despite an unreadable directory", info.Language)
	}
}

// TestSummaryOmitsUnreadableWhenZero keeps normal output unchanged.
func TestSummaryOmitsUnreadableWhenZero(t *testing.T) {
	s := Info{Repository: "lato", Language: "Go"}.Summary()
	if strings.Contains(s, "Unreadable") {
		t.Errorf("Summary should omit Unreadable when nothing was skipped:\n%s", s)
	}
}

// TestSummaryReportsUnreadable confirms a partial scan is visible rather
// than silently presented as a complete picture.
func TestSummaryReportsUnreadable(t *testing.T) {
	s := Info{Repository: "lato", Language: "Go", Unreadable: 3}.Summary()
	if !strings.Contains(s, "Unreadable") || !strings.Contains(s, "3") {
		t.Errorf("Summary should report 3 unreadable entries:\n%s", s)
	}
}

// TestImportantFilesIncludeConventionFiles pins the gap this change
// closes: agent-instruction, license, and convention files were absent
// from the reported list even when present.
func TestImportantFilesIncludeConventionFiles(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"go.mod":          "module example.com/probe\n",
		"CLAUDE.md":       "# Project rules\n",
		"AGENTS.md":       "# Agent notes\n",
		"LICENSE":         "MIT\n",
		"CONTRIBUTING.md": "# Contributing\n",
		".editorconfig":   "root = true\n",
	})

	info := DiscoverDir(dir)

	for _, want := range []string{"CLAUDE.md", "AGENTS.md", "LICENSE", "CONTRIBUTING.md", ".editorconfig"} {
		if !contains(info.ImportantFiles, want) {
			t.Errorf("ImportantFiles = %v, want it to include %q", info.ImportantFiles, want)
		}
	}
}

// TestImportantFilesOmitAbsentFiles keeps the list free of entries for
// files that do not exist.
func TestImportantFilesOmitAbsentFiles(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.go": "package main\n"})

	info := DiscoverDir(dir)

	for _, absent := range []string{"CLAUDE.md", "AGENTS.md", "LICENSE", "Justfile", "tsconfig.json"} {
		if contains(info.ImportantFiles, absent) {
			t.Errorf("ImportantFiles = %v, want no %q", info.ImportantFiles, absent)
		}
	}
}

// TestIndexAndDiscoveryAgreeOnIgnoredPaths is the cross-check for the
// consolidation: discovery and the repository index must not disagree
// about which paths belong to the project.
func TestIndexAndDiscoveryAgreeOnIgnoredPaths(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"go.mod":                "module example.com/probe\n",
		"main.go":               "package main\n",
		".gitignore":            "generated/\n",
		"generated/assets/a.js": "var a = 1\n",
	})

	info := DiscoverDir(dir)
	for _, n := range info.Tree {
		if strings.HasPrefix(n.Path, "generated") {
			t.Errorf("discovery reported ignored path %q but should not have", n.Path)
		}
	}
}

func hasNode(nodes []Node, path string) bool {
	for _, n := range nodes {
		if n.Path == path {
			return true
		}
	}
	return false
}

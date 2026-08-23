package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/workspace"
)

// mkdir creates a temp dir for a test and returns its path.
func mkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// write writes a file with content under dir, creating parents.
func write(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildWithReadmeAndGoMod(t *testing.T) {
	dir := mkdir(t)
	write(t, dir, "go.mod", "module github.com/acme/lato\n\ngo 1.26\n\nrequire (\n\tgopkg.in/yaml.v3 v3.0.1\n\tgithub.com/spf13/cobra v1.10.2 // indirect\n)\n")
	write(t, dir, "README.md", "# Lato\n\nA local-first agent harness.\n")
	write(t, dir, "cmd/lato/main.go", "package main\n")
	write(t, dir, "internal/runtime/runtime.go", "package runtime\n")
	write(t, dir, "internal/tools/tool.go", "package tools\n")
	write(t, dir, "internal/providers/ollama.go", "package providers\n")

	b := NewBuilder(workspaceInfo(dir))
	c := b.Build()

	if c.Workspace.Language != "Go" {
		t.Errorf("Language = %q, want Go", c.Workspace.Language)
	}
	if c.Readme == "" || !strings.Contains(c.Readme, "Lato") {
		t.Errorf("Readme = %q, want to contain Lato", c.Readme)
	}
	if c.GoMod == nil {
		t.Fatal("GoMod is nil, want parsed go.mod")
	}
	if c.GoMod.Module != "github.com/acme/lato" {
		t.Errorf("GoMod.Module = %q, want github.com/acme/lato", c.GoMod.Module)
	}
	if c.GoMod.Go != "1.26" {
		t.Errorf("GoMod.Go = %q, want 1.26", c.GoMod.Go)
	}
	if len(c.GoMod.Requires) != 2 {
		t.Errorf("GoMod.Requires = %v, want 2 deps", c.GoMod.Requires)
	}
}

func TestBuildWithoutReadme(t *testing.T) {
	dir := mkdir(t)
	write(t, dir, "go.mod", "module m\n")

	c := NewBuilder(workspaceInfo(dir)).Build()

	if c.Readme != "" {
		t.Errorf("Readme = %q, want empty", c.Readme)
	}
	if c.GoMod == nil {
		t.Fatal("GoMod is nil, want parsed go.mod")
	}
}

func TestBuildWithoutGoMod(t *testing.T) {
	dir := mkdir(t)
	write(t, dir, "README.md", "# Hello\n")

	c := NewBuilder(workspaceInfo(dir)).Build()

	if c.GoMod != nil {
		t.Errorf("GoMod = %+v, want nil for non-Go project", c.GoMod)
	}
	if c.Readme == "" {
		t.Error("Readme empty, want README content")
	}
}

func TestLargeReadmeTruncated(t *testing.T) {
	dir := mkdir(t)
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("line of the readme content\n")
	}
	write(t, dir, "README.md", sb.String())

	c := NewBuilder(workspaceInfo(dir)).Build()

	lineCount := len(strings.Split(c.Readme, "\n"))
	if lineCount > readmeLines {
		t.Errorf("Readme has %d lines, want <= %d", lineCount, readmeLines)
	}
}

func TestDirectoryTreeGeneration(t *testing.T) {
	dir := mkdir(t)
	write(t, dir, "go.mod", "module m\n")
	write(t, dir, "cmd/lato/main.go", "package main\n")
	write(t, dir, "internal/runtime/runtime.go", "package runtime\n")
	write(t, dir, "internal/providers/ollama.go", "package providers\n")
	write(t, dir, "pkg/util/util.go", "package util\n")
	write(t, dir, "api/handler.go", "package api\n")

	c := NewBuilder(workspaceInfo(dir)).Build()

	tree := packageList(c.Workspace)
	want := []string{"api", "cmd", "internal", "pkg"}
	if len(tree) != len(want) {
		t.Fatalf("packageList = %v, want %v", tree, want)
	}
	for i := range want {
		if tree[i] != want[i] {
			t.Errorf("tree[%d] = %q, want %q", i, tree[i], want[i])
		}
	}
}

func TestContextFormatting(t *testing.T) {
	dir := mkdir(t)
	write(t, dir, "go.mod", "module github.com/acme/lato\n\ngo 1.26\n\nrequire (\n\tgopkg.in/yaml.v3 v3.0.1\n)\n")
	write(t, dir, "README.md", "# Lato\n")
	write(t, dir, "cmd/lato/main.go", "package main\n")
	write(t, dir, "internal/runtime/runtime.go", "package runtime\n")

	c := NewBuilder(workspaceInfo(dir)).Build()
	text := c.Text()

	for _, want := range []string{
		"Repository:", "Language:", "Module:", "Build:",
		"Tree:", "- cmd", "- internal",
		"README Summary:", "# Lato",
		"go.mod:", "Module: github.com/acme/lato", "Go: 1.26",
		"Important Files:", "go.mod", "README.md",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Text() missing %q:\n%s", want, text)
		}
	}
}

func TestContextFormattingEmptyWorkspace(t *testing.T) {
	dir := mkdir(t)
	// Empty dir: no language, no readme, no go.mod.
	c := NewBuilder(workspaceInfo(dir)).Build()
	text := c.Text()

	// Empty workspace still carries the repository name; empty sections
	// (language, build, tree, …) are dropped by design.
	if text == "" {
		t.Error("Text() empty for an empty workspace")
	}
	if !strings.Contains(text, "Repository:") {
		t.Errorf("Text() missing Repository: for empty workspace:\n%s", text)
	}
	if strings.Contains(text, "Language:") || strings.Contains(text, "Build:") {
		t.Errorf("Text() should drop empty Language/Build sections:\n%s", text)
	}
}

func TestRepositoryQuestionDetection(t *testing.T) {
	yes := []string{
		"Explain this repository",
		"explain this project",
		"how does this project work?",
		"what architecture is used here?",
		"Describe this codebase",
		"How is this repository structured?",
	}
	no := []string{
		"Hello there",
		"Fix this bug",
		"Write a unit test for this function",
		"Explain how the HTTP client works", // mentions neither repo nor project
		"",
	}

	for _, q := range yes {
		if !RepositoryQuestion(q) {
			t.Errorf("RepositoryQuestion(%q) = false, want true", q)
		}
	}
	for _, q := range no {
		if RepositoryQuestion(q) {
			t.Errorf("RepositoryQuestion(%q) = true, want false", q)
		}
	}
}

// workspaceInfo discovers a workspace rooted at dir.
func workspaceInfo(dir string) workspace.Info {
	return workspace.DiscoverDir(dir)
}

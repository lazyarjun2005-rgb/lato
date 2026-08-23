package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree creates files at the given relative paths under a fresh temp
// directory and returns that directory.
func writeTree(t *testing.T, paths ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// writeFile writes a file with the given content into dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverGoProject(t *testing.T) {
	dir := writeTree(t, "go.mod", "internal/app/main.go", "cmd/lato/main.go", "README.md")
	writeFile(t, dir, "go.mod", "module github.com/example/lato\n\ngo 1.26\n")

	info := DiscoverDir(dir)

	if info.Repository != "lato" {
		t.Errorf("Repository = %q, want lato", info.Repository)
	}
	if info.Language != "Go" {
		t.Errorf("Language = %q, want Go", info.Language)
	}
	if info.Module != "github.com/example/lato" {
		t.Errorf("Module = %q, want github.com/example/lato", info.Module)
	}
	if info.BuildSystem != "Go modules" {
		t.Errorf("BuildSystem = %q, want Go modules", info.BuildSystem)
	}
	if info.PackageManager != "Go modules" {
		t.Errorf("PackageManager = %q, want Go modules", info.PackageManager)
	}
	if info.Root != dir {
		t.Errorf("Root = %q, want %q", info.Root, dir)
	}
	if !contains(info.ImportantFiles, "README.md") || !contains(info.ImportantFiles, "go.mod") {
		t.Errorf("ImportantFiles = %v, want README.md and go.mod", info.ImportantFiles)
	}
}

func TestDiscoverPythonProject(t *testing.T) {
	dir := writeTree(t, "pyproject.toml", "src/hello.py", "tests/test_hello.py", "README.md")
	writeFile(t, dir, "pyproject.toml", "[project]\nname = \"mypkg\"\n")

	info := DiscoverDir(dir)

	if info.Language != "Python" {
		t.Errorf("Language = %q, want Python", info.Language)
	}
	if info.Module != "mypkg" {
		t.Errorf("Module = %q, want mypkg", info.Module)
	}
	if info.BuildSystem != "PEP 517" {
		t.Errorf("BuildSystem = %q, want PEP 517", info.BuildSystem)
	}
	if info.PackageManager != "pip" {
		t.Errorf("PackageManager = %q, want pip", info.PackageManager)
	}
}

func TestDiscoverRustProject(t *testing.T) {
	dir := writeTree(t, "Cargo.toml", "src/main.rs")
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"mytool\"\n")

	info := DiscoverDir(dir)

	if info.Language != "Rust" {
		t.Errorf("Language = %q, want Rust", info.Language)
	}
	if info.Module != "mytool" {
		t.Errorf("Module = %q, want mytool", info.Module)
	}
	if info.BuildSystem != "Cargo" || info.PackageManager != "Cargo" {
		t.Errorf("BuildSystem = %q, PackageManager = %q, want Cargo/Cargo", info.BuildSystem, info.PackageManager)
	}
}

func TestDiscoverTypeScriptProject(t *testing.T) {
	dir := writeTree(t, "package.json", "tsconfig.json", "src/index.ts", "package-lock.json")
	writeFile(t, dir, "package.json", `{"name":"webapp","dependencies":{"react":"^18"}}`)

	info := DiscoverDir(dir)

	if info.Language != "TypeScript" {
		t.Errorf("Language = %q, want TypeScript", info.Language)
	}
	if info.Module != "webapp" {
		t.Errorf("Module = %q, want webapp", info.Module)
	}
	if info.PackageManager != "npm" {
		t.Errorf("PackageManager = %q, want npm", info.PackageManager)
	}
}

func TestDiscoverJavaProject(t *testing.T) {
	dir := writeTree(t, "pom.xml", "src/main/java/com/example/App.java")
	writeFile(t, dir, "pom.xml", "<project><groupId>com.example</groupId><artifactId>myapp</artifactId></project>")

	info := DiscoverDir(dir)

	if info.Language != "Java" {
		t.Errorf("Language = %q, want Java", info.Language)
	}
	if info.BuildSystem != "Maven" {
		t.Errorf("BuildSystem = %q, want Maven", info.BuildSystem)
	}
}

func TestDiscoverEmptyFolder(t *testing.T) {
	info := DiscoverDir(t.TempDir())

	if info.Repository == "" {
		t.Error("Repository should fall back to the directory name")
	}
	if info.Language != "" {
		t.Errorf("Language = %q, want empty", info.Language)
	}
	if info.BuildSystem != "" || info.PackageManager != "" {
		t.Errorf("BuildSystem = %q, PackageManager = %q, want empty", info.BuildSystem, info.PackageManager)
	}
	if len(info.ImportantFiles) != 0 {
		t.Errorf("ImportantFiles = %v, want empty", info.ImportantFiles)
	}
	if info.IsGitRepo {
		t.Error("IsGitRepo = true, want false for an empty folder")
	}
}

func TestDiscoverWithoutGit(t *testing.T) {
	dir := writeTree(t, "go.mod", "main.go")

	info := DiscoverDir(dir)

	if info.IsGitRepo {
		t.Error("IsGitRepo = true, want false")
	}
	if info.Branch != "" {
		t.Errorf("Branch = %q, want empty", info.Branch)
	}
}

func TestDiscoverGitBranch(t *testing.T) {
	dir := writeTree(t, ".git/HEAD", ".git/config", "go.mod")
	writeFile(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	writeFile(t, dir, ".git/config", `[remote "origin"]
	url = https://github.com/acme/widget.git
`)

	info := DiscoverDir(dir)

	if !info.IsGitRepo {
		t.Fatal("IsGitRepo = false, want true")
	}
	if info.Branch != "main" {
		t.Errorf("Branch = %q, want main", info.Branch)
	}
	if info.Repository != "widget" {
		t.Errorf("Repository = %q, want widget", info.Repository)
	}
}

func TestDiscoverMissingReadme(t *testing.T) {
	dir := writeTree(t, "go.mod", "main.go")

	info := DiscoverDir(dir)

	if contains(info.ImportantFiles, "README.md") {
		t.Errorf("ImportantFiles = %v, want no README.md", info.ImportantFiles)
	}
}

func TestDiscoverMissingPackageFiles(t *testing.T) {
	dir := writeTree(t, "main.go", "util.go", "internal/x.go")

	info := DiscoverDir(dir)

	if info.Language != "Go" {
		t.Errorf("Language = %q, want Go (from extension fallback)", info.Language)
	}
	if info.BuildSystem != "" {
		t.Errorf("BuildSystem = %q, want empty without a manifest", info.BuildSystem)
	}
	if info.PackageManager != "" {
		t.Errorf("PackageManager = %q, want empty without a manifest", info.PackageManager)
	}
	if len(info.ImportantFiles) != 0 {
		t.Errorf("ImportantFiles = %v, want empty", info.ImportantFiles)
	}
}

func TestDiscoverFindsRootUpward(t *testing.T) {
	dir := writeTree(t, "go.mod", "sub/project/main.go")

	sub := filepath.Join(dir, "sub", "project")
	info := DiscoverDir(sub)

	if info.Root != dir {
		t.Errorf("Root = %q, want parent %q (found via upward search)", info.Root, dir)
	}
	if info.Language != "Go" {
		t.Errorf("Language = %q, want Go", info.Language)
	}
}

func TestDiscoverSkipsVendoredDirs(t *testing.T) {
	dir := writeTree(t, "go.mod", "main.go", "node_modules/pkg/index.js", "vendor/foo/bar.go")

	info := DiscoverDir(dir)

	for _, n := range info.Tree {
		if n.Path == "node_modules" || n.Path == "vendor" {
			t.Errorf("Tree should skip %q, got node %q", n.Path, n.Path)
		}
	}
}

func TestSummaryRendersKnownFields(t *testing.T) {
	info := Info{
		Repository:  "lato",
		Language:    "Go",
		Module:      "lato",
		Branch:      "main",
		BuildSystem: "Go modules",
	}
	s := info.Summary()

	for _, want := range []string{"Workspace", "Repository", "Language", "Go", "Build System", "Go modules", "Git Branch"} {
		if !containsStr(s, want) {
			t.Errorf("Summary missing %q:\n%s", want, s)
		}
	}
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

func containsStr(hay, needle string) bool {
	return strings.Contains(hay, needle)
}

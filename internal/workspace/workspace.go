// Package workspace discovers and describes the repository Lato is
// running inside. It is pure filesystem inspection: no AI, no network,
// no external commands. The runtime captures the result once at
// startup, so later milestones (context building, planning, indexing)
// can read a workspace description without re-scanning the disk.
package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Node is one entry in a workspace's directory tree.
type Node struct {
	Name  string
	Path  string // path relative to the workspace root
	IsDir bool
}

// Info is a complete description of the workspace Lato is running
// inside. Discover produces it once; treat it as read-only afterwards.
type Info struct {
	Repository     string // name from the git remote, falling back to the root directory name
	Root           string // absolute path of the detected project root
	CWD            string // absolute path Lato was started from
	OS             string // friendly operating-system name
	IsGitRepo      bool
	Branch         string   // current git branch, "" when detached or not a repo
	Language       string   // primary programming language, "" if undetected
	Framework      string   // detected framework, "" if none
	Module         string   // module/package identifier from the manifest, "" if none
	BuildSystem    string   // detected build system, "" if none
	PackageManager string   // detected package manager, "" if none
	ImportantFiles []string // present root files from the well-known list
	Tree           []Node   // bounded directory tree below Root
}

// wellKnownFiles are the root files Lato looks for and reports. Files
// that do not exist are simply omitted.
var wellKnownFiles = []string{
	"README.md", "go.mod", "go.work", "package.json", "Cargo.toml",
	"requirements.txt", "pyproject.toml", "Makefile", "Dockerfile",
	"docker-compose.yml", "compose.yaml",
}

// rootMarkers are files that, when present in a directory, mark it as a
// project root for upward discovery.
var rootMarkers = []string{
	"go.mod", "go.work", "Cargo.toml", "package.json", "tsconfig.json",
	"pyproject.toml", "requirements.txt", "setup.py",
	"pom.xml", "build.gradle", "build.gradle.kts",
	"package-lock.json", "yarn.lock", "pnpm-lock.yaml", ".git",
}

const (
	maxRootSearch  = 8   // levels walked upward when hunting for a project root
	maxTreeDepth   = 3   // directory-tree depth reported in Info.Tree
	maxTreeEntries = 200 // cap on Info.Tree size
)

// Discover inspects the current working directory.
func Discover() Info {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return DiscoverDir(cwd)
}

// DiscoverDir inspects dir, walking upward to find a project root.
func DiscoverDir(dir string) Info {
	root := findRoot(dir)

	present := map[string]bool{}
	names := append(append([]string(nil), wellKnownFiles...), rootMarkers...)
	for _, name := range names {
		if pathExists(filepath.Join(root, name)) {
			present[name] = true
		}
	}
	if anyMatching(root, "*.csproj") {
		present[".csproj"] = true
	}
	if anyMatching(root, "*.sln") {
		present[".sln"] = true
	}

	tree, extCount := walkTree(root)
	gd := resolveGitDir(root)

	var important []string
	for _, name := range wellKnownFiles {
		if present[name] {
			important = append(important, name)
		}
	}

	lang := detectLanguage(present, extCount)

	repo := filepath.Base(filepath.Clean(root))
	if name := gitRemoteName(gd); name != "" {
		repo = name
	} else if m := detectModule(lang, root); m != "" {
		// No git remote: prefer the module/package name over the bare
		// directory name so the repo is identified by its owner+name path.
		if seg := filepath.Base(filepath.Clean(m)); seg != "." && seg != "/" && seg != "" {
			repo = seg
		}
	}

	return Info{
		Repository:     repo,
		Root:           root,
		CWD:            dir,
		OS:             osName(),
		IsGitRepo:      gd != "",
		Branch:         gitBranch(gd),
		Language:       lang,
		Framework:      detectFramework(lang, root),
		Module:         detectModule(lang, root),
		BuildSystem:    detectBuildSystem(lang, root, present),
		PackageManager: detectPackageManager(lang, root, present),
		ImportantFiles: important,
		Tree:           tree,
	}
}

// findRoot walks upward from start looking for a project-marker file,
// returning the first directory that has one. If none is found within
// maxRootSearch levels, start is returned unchanged.
func findRoot(start string) string {
	dir := start
	for i := 0; i < maxRootSearch; i++ {
		for _, marker := range rootMarkers {
			if pathExists(filepath.Join(dir, marker)) {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return start
}

// resolveGitDir returns the git metadata directory for root, or "" if
// root is not a git repository. It handles both a plain .git directory
// and a .git file pointing at a worktree's gitdir.
func resolveGitDir(root string) string {
	p := filepath.Join(root, ".git")
	fi, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return p
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
	if line == "" {
		return ""
	}
	if !filepath.IsAbs(line) {
		line = filepath.Join(root, line)
	}
	return filepath.Clean(line)
}

// gitBranch reads the current branch from the git HEAD file. Returns ""
// for a detached HEAD that cannot be summarized, or when there is no git
// metadata.
func gitBranch(gd string) string {
	if gd == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(gd, "HEAD"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	if ref := strings.TrimPrefix(s, "ref: refs/heads/"); ref != s {
		return ref
	}
	if len(s) > 7 {
		return s[:7] // detached HEAD: show a short commit id
	}
	return s
}

// gitRemoteName extracts the repository name from the origin remote's
// URL in the git config, returning "" when there is no origin.
func gitRemoteName(gd string) string {
	if gd == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(gd, "config"))
	if err != nil {
		return ""
	}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if strings.HasPrefix(section, `[remote "origin"]`) && strings.HasPrefix(line, "url") {
			url := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
			return repoNameFromURL(url)
		}
	}
	return ""
}

// repoNameFromURL returns the repository name from a git remote URL,
// handling both https://host/user/repo.git and git@host:user/repo forms.
func repoNameFromURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 && i < len(url)-1 {
		return url[i+1:]
	}
	return url
}

// walkTree lists the directory tree below root, bounded by maxTreeDepth
// and maxTreeEntries, and counts source-file extensions for language
// detection. Unreadable entries are skipped; discovery never fails.
func walkTree(root string) ([]Node, map[string]int) {
	var nodes []Node
	ext := map[string]int{}

	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
		} else if e := filepath.Ext(d.Name()); e != "" {
			ext[strings.ToLower(e)]++
		}

		if depth := strings.Count(rel, "/"); depth > maxTreeDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(nodes) >= maxTreeEntries {
			return nil
		}

		nodes = append(nodes, Node{Name: d.Name(), Path: rel, IsDir: d.IsDir()})
		return nil
	})

	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return nodes[i].Path < nodes[j].Path
	})
	return nodes, ext
}

// shouldSkipDir reports whether a directory is a version-control or
// build-artifact directory that should not appear in the tree.
func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor", "target",
		"dist", "build", ".idea", ".vscode", "__pycache__", ".next", ".cache":
		return true
	}
	return false
}

// osName maps the Go runtime GOOS to a friendly display name.
func osName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

// Summary renders a compact two-column description of the workspace for
// the /workspace command. Empty fields print as "-".
func (i Info) Summary() string {
	rows := [][2]string{
		{"Repository", i.Repository},
		{"Language", i.Language},
		{"Framework", i.Framework},
		{"Module", i.Module},
		{"Git Branch", i.Branch},
		{"Build System", i.BuildSystem},
		{"Package Manager", i.PackageManager},
		{"Root", i.Root},
		{"Working Dir", i.CWD},
		{"OS", i.OS},
	}
	var b strings.Builder
	b.WriteString("Workspace\n")
	for _, r := range rows {
		val := r[1]
		if val == "" {
			val = "-"
		}
		fmt.Fprintf(&b, "  %-16s %s\n", r[0], val)
	}
	return strings.TrimRight(b.String(), "\n")
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func anyMatching(root string, patterns ...string) bool {
	for _, p := range patterns {
		matches, _ := filepath.Glob(filepath.Join(root, p))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

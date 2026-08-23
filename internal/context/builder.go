package context

import (
	"path/filepath"
	"strings"

	"lato/internal/workspace"
)

// Builder assembles a Context from a discovered workspace. It owns the
// logic of which files to read and how far to bound them; the workspace
// package only discovers, this package only reads.
type Builder struct {
	root string
}

// NewBuilder returns a Builder for the given workspace. It does no file
// I/O; Build does the work.
func NewBuilder(ws workspace.Info) *Builder {
	return &Builder{root: ws.Root}
}

// Build re-discovers the workspace from the Builder's root (so a Builder
// is fully standalone) and reads the repository files relevant to
// context: README and go.mod. Missing or unreadable files yield empty
// fields; it never returns an error.
func (b *Builder) Build() Context {
	return Context{
		Workspace: workspace.DiscoverDir(b.root),
		Readme:    b.readme(),
		GoMod:     b.goMod(),
	}
}

// readme returns up to readmeLines lines of README.md, or "" if the file
// is missing or unreadable.
func (b *Builder) readme() string {
	return firstLines(filepath.Join(b.root, "README.md"), readmeLines)
}

// goMod parses go.mod into a *GoMod, or returns nil when the project is
// not Go-based or the file is unreadable.
func (b *Builder) goMod() *GoMod {
	return parseGoMod(filepath.Join(b.root, "go.mod"))
}

// packageList returns the top-level package directories shown in the
// workspace tree, i.e. the directory names one level below the root (for
// example cmd/, internal/, pkg/, api/).
func packageList(w workspace.Info) []string {
	seen := map[string]bool{}
	var out []string

	for _, n := range w.Tree {
		if !n.IsDir {
			continue
		}
		dir, ok := topLevelDir(n.Path)
		if !ok || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// topLevelDir returns the first path segment of a slash-separated
// relative path when it is a single-level directory (i.e. exactly one
// segment). ok is false for nested paths and files.
func topLevelDir(path string) (string, bool) {
	if path == "" || path == "." {
		return "", false
	}
	if strings.Contains(path, "/") {
		return "", false
	}
	return path, true
}

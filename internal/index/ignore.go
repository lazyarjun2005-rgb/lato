package index

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// defaultIgnoreDirs are directories always skipped during traversal,
// regardless of the project or machine. They cover version-control
// metadata and the common heavy dependency, build, and cache
// directories. Only base names are matched (the traversal looks at a
// directory's name before descending into it), so a source file named
// e.g. "vendor" is never affected.
var defaultIgnoreDirs = map[string]bool{
	".git":             true,
	".hg":              true,
	".svn":             true,
	"node_modules":     true,
	"vendor":           true,
	"target":           true,
	"dist":             true,
	"build":            true,
	"coverage":         true,
	"__pycache__":      true,
	".venv":            true,
	"venv":             true,
	".idea":            true,
	".vscode":          true,
	".next":            true,
	".nuxt":            true,
	".cache":           true,
	".terraform":       true,
	".tox":             true,
	"bower_components": true,
	"Pods":             true,
	"DerivedData":      true,
}

// defaultIgnoreNames is a small set of files skipped outright because
// they are large, generated, or uninteresting for search.
var defaultIgnoreNames = map[string]bool{
	"go.sum":            true,
	"package-lock.json": true,
	"pnpm-lock.yaml":    true,
	"yarn.lock":         true,
	"composer.lock":     true,
	"Gemfile.lock":      true,
	".gitignore":        true, // repo metadata, not searchable content
}

// size- and content-related bounds for the file walk.
const (
	// maxTextBytes is the largest file whose content is read into the
	// index. Anything larger is still listed and searchable by name and
	// path, but its text is not kept, which bounds memory on huge or
	// minified files.
	maxTextBytes = 4 << 20 // 4 MiB

	// maxBinaryScanBytes is how much of a file is scanned when deciding
	// whether it is binary. Binary detection never reads more than one
	// buffer, so even a multi-gigabyte file costs a single bounded read.
	maxBinaryScanBytes = 8192

	// maxIndexFiles caps how many files the index records, so an
	// enormous repository cannot exhaust memory. The walk itself still
	// completes; files past the cap are not indexed.
	maxIndexFiles = 200_000
)

// gitignorer loads .gitignore rules from the workspace root and reports
// whether a directory or file is ignored. A missing or unreadable rule
// file yields no rules: indexing then only skips defaultIgnoreDirs and
// defaultIgnoreNames, which is deterministic by design.
type gitignorer struct {
	patterns []gitPattern
}

// gitPattern is one normalized .gitignore rule.
type gitPattern struct {
	negated  bool
	dirOnly  bool
	anchored bool
	glob     string // slash-separated, with ** preserved
}

// newGitignorer loads .gitignore from root. It never fails; unreadable
// or absent files simply mean no rules.
func newGitignorer(root string) *gitignorer {
	g := &gitignorer{}
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return g
	}

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimRight(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		g.patterns = append(g.patterns, parseGitPattern(line))
	}
	return g
}

// parseGitPattern normalizes a single .gitignore line. filepath.ToSlash
// converts Windows backslash separators to forward slashes so matching
// uses one consistent separator on every platform.
func parseGitPattern(line string) gitPattern {
	s := filepath.ToSlash(line)
	p := gitPattern{}
	if strings.HasPrefix(s, "!") {
		p.negated = true
		s = s[1:]
	}
	if strings.HasSuffix(s, "/") {
		p.dirOnly = true
		s = strings.TrimSuffix(s, "/")
	}
	if strings.HasPrefix(s, "/") {
		p.anchored = true
		s = strings.TrimPrefix(s, "/")
	}
	p.glob = s
	return p
}

// ignored reports whether relPath (slash-separated, relative to the
// workspace root) is ignored. Negation rules override earlier matching
// rules per git semantics: the last match wins.
func (g *gitignorer) ignored(relPath string, isDir bool) bool {
	if relPath == "" || relPath == "." {
		return false
	}
	ignored := false
	for _, p := range g.patterns {
		if p.glob == "" {
			continue
		}
		if !g.patternMatches(p, relPath, isDir) {
			continue
		}
		ignored = !p.negated
	}
	return ignored
}

// patternMatches applies one rule to relPath. Anchored rules (starting
// with "/") match only the path itself relative to the root. Unanchored
// directory rules match the path and every ancestor directory, so
// "build/" also ignores "build/x.go". Directory-only rules never match a
// file at the exact path, but do match their directory ancestors.
func (g *gitignorer) patternMatches(p gitPattern, relPath string, isDir bool) bool {
	if p.anchored {
		if p.dirOnly && !isDir {
			return false
		}
		return g.matchGlob(p.glob, true, relPath)
	}
	for _, sub := range allSubpaths(relPath) {
		if p.dirOnly && sub == relPath && !isDir {
			continue
		}
		if g.matchGlob(p.glob, false, sub) {
			return true
		}
	}
	return false
}

// matchGlob matches pattern (slash-separated, possibly containing **)
// against a single path or segment, honoring anchored semantics.
func (g *gitignorer) matchGlob(pattern string, anchored bool, relPath string) bool {
	if pattern == "" {
		return false
	}

	switch {
	case pattern == "**":
		return true
	case strings.HasPrefix(pattern, "**/"):
		// "**/foo" matches foo at any depth.
		rest := strings.TrimPrefix(pattern, "**/")
		return g.matchGlob(rest, false, relPath)
	case strings.HasSuffix(pattern, "/**"):
		// "foo/**" matches everything inside foo.
		prefix := strings.TrimSuffix(pattern, "/**")
		return pathHasPrefix(relPath, prefix)
	case strings.Contains(pattern, "/**/"):
		// "a/**/b" matches b inside a at any depth.
		parts := strings.SplitN(pattern, "/**/", 2)
		return g.matchGlob(parts[0], false, relPath) && g.matchGlob(parts[1], false, relPath)
	default:
		ok, err := path.Match(pattern, relPath)
		return err == nil && ok
	}
}

// pathHasPrefix reports whether path equals or descends from prefix,
// where prefix itself is treated as matching everything beneath it.
func pathHasPrefix(pathStr, prefix string) bool {
	return pathStr == prefix || strings.HasPrefix(pathStr, prefix+"/")
}

// allSubpaths returns relPath itself followed by each of its ancestor
// paths, e.g. "a/b/c.go" -> ["a/b/c.go", "a/b", "a"].
func allSubpaths(relPath string) []string {
	parts := strings.Split(relPath, "/")
	out := make([]string, len(parts))
	for i := range parts {
		out[i] = strings.Join(parts[:i+1], "/")
	}
	return out
}

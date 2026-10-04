package workspace

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// This file is the single source of truth for which repository paths Lato
// excludes from a workspace scan. Discovery (Info.Tree, language
// detection) and the repository index both consult it, so the two can
// never disagree about what belongs to the project.

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
// they are large, generated, or uninteresting as repository content.
var defaultIgnoreNames = map[string]bool{
	"go.sum":            true,
	"package-lock.json": true,
	"pnpm-lock.yaml":    true,
	"yarn.lock":         true,
	"composer.lock":     true,
	"Gemfile.lock":      true,
	".gitignore":        true, // repo metadata, not project content
}

// Ignore reports whether a workspace-relative path is excluded from
// repository scans. It combines two rules: a fixed set of directories
// and generated files that are always skipped, and the project's own
// .gitignore rules. An Ignore is read-only after construction and safe
// for concurrent use.
//
// A missing or unreadable .gitignore yields no rules: scans then skip
// only the fixed sets, which is deterministic by design.
type Ignore struct {
	patterns []ignorePattern
}

// ignorePattern is one normalized .gitignore rule.
type ignorePattern struct {
	negated  bool
	dirOnly  bool
	anchored bool
	glob     string // slash-separated, with ** preserved
}

// NewIgnore loads the ignore rules for root. It never fails; an absent
// or unreadable .gitignore simply means no project-specific rules.
func NewIgnore(root string) *Ignore {
	ig := &Ignore{}
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return ig
	}

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimRight(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ig.patterns = append(ig.patterns, parseIgnorePattern(line))
	}
	return ig
}

// SkipDir reports whether a directory should be left out of a scan and
// not descended into. relDir is slash-separated and relative to the
// workspace root.
func (ig *Ignore) SkipDir(relDir string) bool {
	if relDir == "" || relDir == "." {
		return false
	}
	base := path.Base(relDir)
	// A directory sharing a generated file's name (e.g. a stray
	// "vendor" folder) is suspicious, so it is skipped too.
	if defaultIgnoreDirs[base] || defaultIgnoreNames[base] {
		return true
	}
	return ig.ignored(relDir, true)
}

// SkipFile reports whether a file should be left out of a scan. relFile
// is slash-separated and relative to the workspace root.
func (ig *Ignore) SkipFile(relFile string) bool {
	if relFile == "" || relFile == "." {
		return false
	}
	// defaultIgnoreDirs is deliberately not consulted here: a *file*
	// named "build" or "target" is ordinary project content, whereas a
	// directory of that name is generated output.
	if defaultIgnoreNames[path.Base(relFile)] {
		return true
	}
	return ig.ignored(relFile, false)
}

// parseIgnorePattern normalizes a single .gitignore line.
// filepath.ToSlash converts Windows backslash separators to forward
// slashes so matching uses one consistent separator on every platform.
func parseIgnorePattern(line string) ignorePattern {
	s := filepath.ToSlash(line)
	p := ignorePattern{}
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
func (ig *Ignore) ignored(relPath string, isDir bool) bool {
	if relPath == "" || relPath == "." {
		return false
	}
	ignored := false
	for _, p := range ig.patterns {
		if p.glob == "" {
			continue
		}
		if !patternMatches(p, relPath, isDir) {
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
func patternMatches(p ignorePattern, relPath string, isDir bool) bool {
	if p.anchored {
		if p.dirOnly && !isDir {
			return false
		}
		return matchGlob(p.glob, true, relPath)
	}

	// A pattern containing no slash is matched against the base name at
	// every depth, which is git's rule: "*.log" ignores "debug.log" and
	// "nested/debug.log" alike. The ancestor loop below cannot do this on
	// its own, because path.Match's "*" never crosses a separator. A
	// directory-only rule is not applied to a same-named file here; that
	// case belongs to the ancestor loop, which checks isDir.
	if (!p.dirOnly || isDir) && !strings.Contains(p.glob, "/") {
		if matchGlob(p.glob, false, path.Base(relPath)) {
			return true
		}
	}

	for _, sub := range allSubpaths(relPath) {
		if p.dirOnly && sub == relPath && !isDir {
			continue
		}
		if matchGlob(p.glob, false, sub) {
			return true
		}
	}
	return false
}

// matchGlob matches pattern (slash-separated, possibly containing **)
// against a single path or segment, honoring anchored semantics.
func matchGlob(pattern string, anchored bool, relPath string) bool {
	if pattern == "" {
		return false
	}

	switch {
	case pattern == "**":
		return true
	case strings.HasPrefix(pattern, "**/"):
		// "**/foo" matches foo at any depth.
		rest := strings.TrimPrefix(pattern, "**/")
		return matchGlob(rest, false, relPath)
	case strings.HasSuffix(pattern, "/**"):
		// "foo/**" matches everything inside foo.
		prefix := strings.TrimSuffix(pattern, "/**")
		return pathHasPrefix(relPath, prefix)
	case strings.Contains(pattern, "/**/"):
		// "a/**/b" matches b inside a at any depth.
		parts := strings.SplitN(pattern, "/**/", 2)
		return matchGlob(parts[0], false, relPath) && matchGlob(parts[1], false, relPath)
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

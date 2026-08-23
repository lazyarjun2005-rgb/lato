// Workspace boundary checks. Every path a model-requested action touches
// is resolved against the active workspace root before the action may
// run. Resolution is canonical — cleaned, absolutized, and resolved
// through symlinks where practical — never a string prefix comparison,
// so "../" traversal, absolute paths outside the workspace, and symlink
// indirection cannot slip past it.
package permissions

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Boundary describes the workspace a policy protects.
type Boundary struct {
	root string // canonical absolute workspace root
}

// NewBoundary returns a Boundary rooted at root. The root itself is
// canonicalized once here; if it cannot be resolved, the cleaned
// absolute form is used so construction never fails.
func NewBoundary(root string) Boundary {
	return Boundary{root: canonicalRoot(root)}
}

// Root returns the canonical absolute workspace root.
func (b Boundary) Root() string { return b.root }

// Contains reports whether p resolves to a location inside the
// workspace, returning its canonical absolute location when it does.
//
// Accepted forms include relative paths ("src/main.go", "./x"),
// nested directories, and paths whose final components do not exist yet
// — a file about to be created must be judged by the deepest existing
// ancestor. Absolute paths are accepted only when they resolve inside
// the workspace. Symlinked components are followed: a link that points
// out of the workspace makes the target outside too.
func (b Boundary) Contains(p string) (string, bool) {
	if isUNCPath(p) {
		// Windows network shares (\\host\share\...) are never inside a
		// local workspace; rejecting them outright beats silently
		// mangling them into oddly named subdirectories.
		return "", false
	}

	s := normalizeSeparators(p)
	if s == "" {
		return "", false
	}

	abs := s
	switch {
	case strings.HasPrefix(s, "/"):
		abs = path.Clean(s)
	case isDriveColon(s):
		return "", false // Windows drive letter: never inside a POSIX-style root
	default:
		abs = path.Join(b.root, s)
	}

	resolved, ok := b.resolve(abs)
	if !ok {
		return "", false
	}
	return resolved, true
}

// resolve follows symlinks in the deepest existing ancestor of abs and
// reports whether the result stays under the boundary root.
func (b Boundary) resolve(abs string) (string, bool) {
	existing := abs
	var tail []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := path.Dir(existing)
		if parent == existing {
			return "", false // reached the filesystem root without finding anything
		}
		tail = append([]string{path.Base(existing)}, tail...)
		existing = parent
	}

	real, err := filepath.EvalSymlinks(fromSlash(existing))
	if err != nil || real == "" {
		return "", false // unreadable component: judge conservatively as outside
	}
	resolved := toSlash(filepath.Clean(real))
	for _, seg := range tail {
		resolved = path.Join(resolved, seg)
	}

	rel, err := relPath(b.root, resolved)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return resolved, true
}

// canonicalRoot cleans root into the absolute slash-separated form all
// comparisons use, resolving symlinks so the boundary matches how the
// filesystem actually addresses the directory. A relative root is
// anchored at the process working directory via filepath.Abs rather
// than by string surgery, which keeps the result valid on every
// platform including Windows drive-relative forms.
func canonicalRoot(root string) string {
	abs, err := filepath.Abs(normalizeSeparators(root))
	if err != nil {
		abs = filepath.Clean(normalizeSeparators(root))
	}
	cleaned := toSlash(abs)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	if real, err := filepath.EvalSymlinks(fromSlash(cleaned)); err == nil && real != "" {
		cleaned = toSlash(filepath.Clean(real))
	}
	return cleaned
}

// relPath renders abs relative to root with forward slashes.
func relPath(root, abs string) (string, error) {
	rel, err := filepath.Rel(fromSlash(root), fromSlash(abs))
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// normalizeSeparators folds Windows separators to forward slashes so a
// model-supplied "src\main.go" and "src/main.go" mean the same thing,
// mirroring the editing engine's rule.
func normalizeSeparators(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"))
	p = strings.TrimPrefix(p, "//")
	return strings.TrimSpace(p)
}

func fromSlash(p string) string {
	if filepath.Separator == '/' {
		return p
	}
	return strings.ReplaceAll(p, "/", string(filepath.Separator))
}

func toSlash(p string) string { return filepath.ToSlash(p) }

// isDriveColon reports whether p begins with a Windows drive prefix
// such as "C:" or "d:/", which can never name a workspace-relative
// path on this platform's rules.
func isDriveColon(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
}

// isUNCPath reports whether p names a Windows network share in either
// its native ("\\host\share") or folded ("//host/share") form. Such
// paths address remote storage and are never workspace-relative.
func isUNCPath(p string) bool {
	trimmed := strings.TrimSpace(p)
	return strings.HasPrefix(trimmed, `\\`) || strings.HasPrefix(trimmed, "//")
}

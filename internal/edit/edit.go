// Package edit implements Lato's safe local editing engine: targeted
// text replacements and file creation, confined to the workspace root
// the agent is operating on.
//
// Editing is deliberately conservative. A replacement states the exact
// old text it expects to find; the engine refuses to act when that text
// is missing or matches more than one location, so a vague instruction
// can never silently modify the wrong part of a file. Whole-file
// rewrites are the model's job via write_file, not this engine's.
//
// All operations are pure filesystem work: no model calls, no network,
// no telemetry. Path handling is separator-style-independent so the
// same code serves Linux and Windows.
package edit

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaxFileSize bounds the files the engine will read into memory for
// editing. It matches the index's own text bound: files too large to
// index are also too large to rewrite wholesale from a prompt.
const MaxFileSize = 4 << 20 // 4 MiB

// Sentinel errors for the conditions a caller (or the model, through a
// tool result) needs to distinguish. They are always wrapped with the
// file path and surrounding context.
var (
	ErrNotFound    = errors.New("target text was not found in the file")
	ErrAmbiguous   = errors.New("target text matches multiple locations; include more surrounding lines to identify one")
	ErrExists      = errors.New("file already exists")
	ErrTooLarge    = errors.New("file is too large to edit")
	ErrInvalidPath = errors.New("invalid workspace path")
)

// Op is one targeted replacement: the exact old text to find and the
// text to put in its place. Old must be non-empty and must occur
// exactly once; New may be empty (removing the text).
type Op struct {
	Old string
	New string
}

// Result describes a completed filesystem operation. Before and After
// hold the full file contents so callers can render diffs without
// re-reading anything.
type Result struct {
	Path     string // workspace-relative, slash-separated
	Created  bool   // true for CreateFile
	Replaced int    // number of replacements applied (0 for creates)
	Before   string // contents before the operation, "" for creates
	After    string // contents after the operation
}

// Changed reports whether the operation altered the file content.
func (r Result) Changed() bool { return r.Before != r.After }

// Workspace is an editing session bound to one repository root. Every
// path it accepts is resolved against that root and verified to stay
// inside it.
type Workspace struct {
	root string

	// OnChange, when set, is called after a file was created or its
	// content changed. Owners of derived state (the runtime's cached
	// index, for example) use it to invalidate lazily; it must be cheap
	// and is called synchronously on the editing path.
	OnChange func()
}

// NewWorkspace returns a Workspace that edits files under root. Root
// should be the absolute workspace root discovered at startup.
func NewWorkspace(root string) *Workspace {
	return &Workspace{root: root}
}

// Root returns the absolute path all operations are confined to.
func (w *Workspace) Root() string { return w.root }

// Resolve validates a workspace-relative path and returns its absolute
// location plus the normalized slash-separated relative form used in
// results and diffs.
//
// Validation is style-independent: both separator styles are folded to
// forward slashes first, then the input is checked for absolute forms
// ("/x"), Windows drive letters ("C:x"), UNC shares ("\\host\share"),
// and ".." segments. All of these are rejected on every platform, so
// neither a Unix- nor a Windows-flavored path from the model can escape
// the workspace, regardless of which operating system Lato runs on.
//
// Because backslashes always mean "separator" here, a file whose name
// literally contains a backslash cannot be addressed by these tools;
// such names are effectively reserved. The check is lexical; it does
// not follow symlinks.
func (w *Workspace) Resolve(relPath string) (abs, slashRel string, err error) {
	p := strings.TrimSpace(relPath)
	if p == "" {
		return "", "", fmt.Errorf("%w: path is empty", ErrInvalidPath)
	}

	s := strings.ReplaceAll(p, "\\", "/")
	switch {
	case strings.HasPrefix(s, "/"):
		return "", "", fmt.Errorf("%w: %q is absolute; use a path relative to the workspace root", ErrInvalidPath, relPath)
	case isDriveColon(s):
		return "", "", fmt.Errorf("%w: %q contains a drive letter; use a path relative to the workspace root", ErrInvalidPath, relPath)
	case hasDotDotSegment(s):
		return "", "", fmt.Errorf("%w: %q escapes the workspace root", ErrInvalidPath, relPath)
	}

	cleaned := path.Clean(s)
	if cleaned == "." || cleaned == "" {
		return "", "", fmt.Errorf("%w: %q does not name a file inside the workspace", ErrInvalidPath, relPath)
	}

	abs = filepath.Join(w.root, filepath.FromSlash(cleaned))

	// Belt and braces: recompute the relative path from the joined
	// result so containment holds even for odd inputs Clean preserves.
	rel, relErr := filepath.Rel(w.root, abs)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: %q escapes the workspace root", ErrInvalidPath, relPath)
	}
	return abs, cleaned, nil
}

// isDriveColon reports whether p begins with a Windows drive prefix
// such as "C:" or "d:/", which must never be treated as workspace-
// relative.
func isDriveColon(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
}

// hasDotDotSegment reports whether any path segment would climb above
// the workspace root.
func hasDotDotSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// ReplaceFile applies ops to an existing file in order, writing the
// result back only if every op applied and something actually changed.
// The file is replaced atomically (write to a sibling temporary file,
// then rename), and its permissions are preserved.
func (w *Workspace) ReplaceFile(relPath string, ops ...Op) (Result, error) {
	if len(ops) == 0 {
		return Result{}, fmt.Errorf("edit %s: no replacements were requested", relPath)
	}

	abs, slash, err := w.Resolve(relPath)
	if err != nil {
		return Result{}, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Result{}, fmt.Errorf("open %s: %w", slash, err)
	}
	if info.IsDir() {
		return Result{}, fmt.Errorf("edit %s: is a directory", slash)
	}
	if info.Size() > MaxFileSize {
		return Result{}, fmt.Errorf("edit %s: %w (%d bytes, limit %d)", slash, ErrTooLarge, info.Size(), MaxFileSize)
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", slash, err)
	}

	after := string(raw)
	replaced := 0
	for _, op := range ops {
		var n int
		after, n, err = applyOne(after, op)
		if err != nil {
			return Result{}, fmt.Errorf("edit %s: %w", slash, err)
		}
		replaced += n
	}

	res := Result{Path: slash, Replaced: replaced, Before: string(raw), After: after}
	if !res.Changed() {
		return res, nil // nothing to write; no-op edit leaves the index valid
	}
	if err := writeFileAtomic(abs, []byte(after), info.Mode().Perm()); err != nil {
		return Result{}, err
	}
	w.notifyChanged()
	return res, nil
}

// CreateFile writes content to a new file, creating parent directories
// as needed. It never overwrites: an existing file is an error, so a
// "create" request can never clobber work. Empty content is allowed.
func (w *Workspace) CreateFile(relPath, content string) (Result, error) {
	abs, slash, err := w.Resolve(relPath)
	if err != nil {
		return Result{}, err
	}

	if dir := filepath.Dir(abs); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Result{}, fmt.Errorf("create %s: make parent directories: %w", slash, err)
		}
	}

	file, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return Result{}, fmt.Errorf("create %s: %w; edit it instead", slash, ErrExists)
	}
	if err != nil {
		return Result{}, fmt.Errorf("create %s: %w", slash, err)
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return Result{}, fmt.Errorf("create %s: %w", slash, err)
	}
	if err := file.Close(); err != nil {
		return Result{}, fmt.Errorf("create %s: %w", slash, err)
	}

	w.notifyChanged()
	return Result{Path: slash, Created: true, After: content}, nil
}

// notifyChanged reports a completed write to OnChange, when one is set.
func (w *Workspace) notifyChanged() {
	if w.OnChange != nil {
		w.OnChange()
	}
}

// applyOne performs a single replacement against content and returns
// the new content plus the number of replacements made (always 1 on
// success).
//
// Matching adapts to the file's line endings: when the exact text is
// absent, the op's line breaks are retried in the other style. This
// keeps edits working when a model emits "\n" against a Windows CRLF
// checkout, and the replacement is written back in the file's own
// style so the file's ending convention survives the edit.
func applyOne(content string, op Op) (string, int, error) {
	if strings.TrimSpace(op.Old) == "" {
		return "", 0, fmt.Errorf("replacement needs the exact old text to find; got empty text")
	}

	old, replacement := op.Old, op.New
	count := strings.Count(content, old)
	if count == 0 {
		switch {
		case strings.Contains(old, "\n") && !strings.Contains(old, "\r"):
			crlf := strings.ReplaceAll(old, "\n", "\r\n")
			if m := strings.Count(content, crlf); m > 0 {
				count, old = m, crlf
				replacement = strings.ReplaceAll(replacement, "\n", "\r\n")
			}
		case strings.Contains(old, "\r\n"):
			lf := strings.ReplaceAll(old, "\r\n", "\n")
			if m := strings.Count(content, lf); m > 0 {
				count, old = m, lf
			}
		}
	}

	switch {
	case count == 0:
		return "", 0, ErrNotFound
	case count > 1:
		return "", 0, fmt.Errorf("%w (%d matches)", ErrAmbiguous, count)
	}
	return strings.Replace(content, old, replacement, 1), 1, nil
}

// writeFileAtomic writes data to path by creating a temporary sibling,
// then renaming it over the destination, so an interrupted write never
// leaves a half-edited file behind. os.Rename replaces existing files
// on both Linux and Windows.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".lato-*")
	if err != nil {
		return fmt.Errorf("stage edit of %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("stage edit of %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("stage edit of %s: %w", path, err)
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("preserve permissions on %s: %w", path, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Package index builds a local, deterministic index of the workspace
// repository so the agent can find files and symbols without sending the
// whole tree to the model.
//
// Indexing is pure filesystem work: it walks the tree once, skips
// version-control and dependency/build directories, respects .gitignore,
// bounds every read, and extracts Go symbols with the standard library
// parser. It never contacts a model or the network, and it performs no
// writes — persistence/incremental indexing can be layered on later
// without touching the walk.
package index

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"lato/internal/workspace"
)

// Index is a lazy snapshot of the workspace: the discovered Info plus a
// cache of the walked files. Looking up files is O(1) by path; the raw
// Body text is held in memory for the requested size bound, so search
// works on the cached entries without re-reading disk.
type Index struct {
	Info    workspace.Info
	files   []File
	byPath  map[string]int
	stats   Stats
	built   bool
	rootKey string
}

// Builder produces an Index bound to a workspace root. It performs no
// I/O until Build is called.
type Builder struct {
	root string
}

// NewBuilder returns a Builder for the given workspace root.
func NewBuilder(root string) *Builder {
	return &Builder{root: root}
}

// ForWorkspace returns a Builder for the workspace's root.
func ForWorkspace(ws workspace.Info) *Builder {
	return NewBuilder(ws.Root)
}

// Root returns the workspace root this builder targets.
func (b *Builder) Root() string { return b.root }

// Build walks the workspace root and returns a ready-to-use Index. The
// walk never fails; an unreadable workspace yields an empty index.
func (b *Builder) Build() *Index {
	files, stats := Build(b.root)
	// Stats.Root is filled by the package-level Build for reporting.
	stats.Root = b.root

	idx := &Index{
		Info:    workspace.DiscoverDir(b.root),
		files:   files,
		byPath:  make(map[string]int, len(files)),
		stats:   stats,
		built:   true,
		rootKey: b.root,
	}
	for i, f := range files {
		idx.byPath[f.Path] = i
	}
	return idx
}

// Built reports whether the index has been constructed already.
func (i *Index) Built() bool { return i != nil && i.built }

// Files returns the indexed files.
func (i *Index) Files() []File {
	if i == nil {
		return nil
	}
	return i.files
}

// Stats returns the traversal summary captured at Build time.
func (i *Index) Stats() (Stats, bool) {
	if i == nil || !i.built {
		return Stats{}, false
	}
	return i.stats, true
}

// Lookup returns the file entry at relPath (slash-separated) by an O(1)
// map access.
func (i *Index) Lookup(relPath string) (File, bool) {
	if i == nil || !i.built {
		return File{}, false
	}
	n, ok := i.byPath[relPath]
	if !ok {
		return File{}, false
	}
	return i.files[n], true
}

// Summary renders a compact report for the /index command. It includes
// the workspace facts, file and directory counts, the language
// breakdown, Go package and symbol counts, and the ignored directory
// list. It matches the two-column style used by workspace.Summary.
func (i *Index) Summary() string {
	if i == nil || !i.built {
		return "Index: not built"
	}

	stats, _ := i.Stats()

	var b strings.Builder
	b.WriteString("Index\n")

	rows := [][2]string{
		{"Repository", i.Info.Repository},
		{"Root", i.Info.Root},
		{"Files", fmt.Sprintf("%d", stats.Files)},
		{"Directories", fmt.Sprintf("%d", stats.Directories)},
		{"Languages", formatLanguages(stats.Languages)},
		{"Source files", formatLangCount(stats.Languages)},
		{"Symbols", fmt.Sprintf("%d", stats.Symbols)},
		{"Ignored paths", formatSkipped(stats.SkippedDirs)},
	}
	if stats.ReachedLimit {
		rows = append(rows, [2]string{"Status", "index limit reached"})
	} else {
		rows = append(rows, [2]string{"Status", "built"})
	}

	for _, r := range rows {
		val := r[1]
		if val == "" {
			val = "-"
		}
		fmt.Fprintf(&b, "  %-16s %s\n", r[0], val)
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatLanguages joins a language->count map with a compact separator.
func formatLanguages(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s (%d)", k, m[k])
	}
	return strings.Join(parts, ", ")
}

// formatLangCount is the "Source files" row; it totals the source-code
// languages present rather than reusing the full language map, so the
// command reads "Source files: 5".
func formatLangCount(m map[string]int) string {
	total := 0
	for _, c := range m {
		total += c
	}
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", total)
}

// formatSkipped renders the ignored directory list, truncating once it
// exceeds a few entries so huge repos stay readable.
func formatSkipped(dirs []string) string {
	if len(dirs) == 0 {
		return "-"
	}
	const maxShow = 6
	shown := dirs
	if len(shown) > maxShow {
		shown = shown[:maxShow]
	}
	parts := make([]string, len(shown))
	for i, d := range shown {
		parts[i] = d + "/"
	}
	s := strings.Join(parts, ", ")
	if len(dirs) > maxShow {
		s += fmt.Sprintf(" (+%d more)", len(dirs)-maxShow)
	}
	return s
}

// TLD returns the top-level directory/build root of the workspace,
// using the same convention the context package relies on: the first
// slash-separated segment of the workspace root.
func TLD(root string) string {
	return filepath.Base(filepath.Clean(root))
}

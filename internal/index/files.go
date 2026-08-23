package index

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File describes one indexed file in the repository. It is the entry the
// search system works against, so it deliberately avoids holding the full
// text of large or binary files (see Body).
type File struct {
	Path      string   // slash-separated path relative to the workspace root
	Name      string   // base name
	Ext       string   // extension including the dot, lowercased, "" if none
	Lang      string   // detected language, "" if none
	Size      int64    // file size in bytes
	Binary    bool     // true when the content looked like a non-text file
	Truncated bool     // true when Body holds only the first maxTextBytes
	Ignored   bool     // true when the file is covered by ignore rules
	Body      string   // text content, "" for binary or unreadable files
	Packages  []string // Go package names, indexed for Go files
	Imports   []string // quoted Go import paths, indexed for Go files
	Symbols   []Symbol // extracted symbols, indexed for Go files
}

// Symbol is one extracted source symbol.
type Symbol struct {
	Kind string // function, method, struct, interface, type, variable, constant
	Name string
	Pos  int    // 1-based line number of the declaration start, 0 if unknown
	Pkg  string // Go package path the symbol belongs to, "" if unknown
}

// walker builds an index by walking the workspace tree once.
type walker struct {
	root       string
	ignore     *gitignorer
	files      []File
	skipped    []string // ignored directory relative paths, for reporting
	dirCount   int      // directories entered (not skipped)
	reachedCap bool
}

// Build walks root and returns the indexed files plus aggregate counts
// for reporting. It never fails: unreadable entries are skipped and the
// remaining files are indexed.
func Build(root string) ([]File, Stats) {
	w := &walker{
		root:   root,
		ignore: newGitignorer(root),
	}
	w.walk()

	stats := statsOf(w.files, w.skipped, w.dirCount, w.reachedCap)
	sort.Slice(w.files, func(i, j int) bool {
		return w.files[i].Path < w.files[j].Path
	})
	return w.files, stats
}

// walk descends the tree starting at root.
func (w *walker) walk() {
	filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == w.root {
			return nil
		}

		rel, err := filepath.Rel(w.root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if w.shouldSkipDir(rel) {
				w.skipped = append(w.skipped, rel)
				return fs.SkipDir
			}
			w.dirCount++
			return nil
		}

		w.indexFile(path, rel, d)
		return nil
	})
}

// shouldSkipDir decides whether a directory is excluded from the walk.
// Default ignored directories always win; otherwise .gitignore rules are
// consulted.
func (w *walker) shouldSkipDir(rel string) bool {
	base := filepath.Base(rel)
	if defaultIgnoreDirs[base] {
		return true
	}
	if defaultIgnoreNames[base] { // a directory sharing a lockfile name is suspicious, skip it
		return true
	}
	return w.ignore.ignored(rel, true)
}

// indexFile records one file when it is not ignored and the index is not
// full. Content is read under a bounded size and binary check.
func (w *walker) indexFile(abs, rel string, d fs.DirEntry) {
	if len(w.files) >= maxIndexFiles {
		w.reachedCap = true
		return
	}

	if defaultIgnoreNames[filepath.Base(rel)] {
		return
	}
	if w.ignore.ignored(rel, false) {
		return
	}

	info, err := d.Info()
	if err != nil {
		return
	}

	f := File{
		Path:    rel,
		Name:    filepath.Base(rel),
		Ext:     strings.ToLower(filepath.Ext(rel)),
		Lang:    languageFor(rel),
		Size:    info.Size(),
		Ignored: false,
	}
	setBody(&f, abs)
	if f.Lang == "Go" {
		extractGo(&f)
	}

	w.files = append(w.files, f)
}

// setBody reads the file's text into File when it is small enough and
// looks like text. Binary and oversized files keep Body empty but remain
// listed and searchable by name/path; oversized files are marked
// Truncated so content search knows the body is a prefix of the file.
func setBody(f *File, abs string) {
	if f.Size > maxTextBytes {
		data := readHead(abs, maxTextBytes)
		if len(data) == 0 {
			return
		}
		if isBinary(data) {
			f.Binary = true
			return
		}
		f.Truncated = true
		f.Body = string(data)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return
	}
	if isBinary(data) {
		f.Binary = true
		return
	}
	f.Body = string(data)
}

// readHead returns up to n bytes from the start of path. A short read or
// an error yields whatever could be read (possibly nothing).
func readHead(path string, n int64) []byte {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	data := make([]byte, n)
	read, err := io.ReadFull(file, data)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return data[:read]
	}
	return data[:read]
}

// isBinary reports whether the first maxBinaryScanBytes of data look
// like a text file. A NUL byte or a high proportion of non-printable
// bytes marks the input as binary.
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	n := len(data)
	if n > maxBinaryScanBytes {
		n = maxBinaryScanBytes
	}
	head := data[:n]
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	nonText := 0
	for _, b := range head {
		if b < 0x09 || (b > 0x0d && b < 0x20 && b != 0x1b) {
			nonText++
		}
	}
	return nonText*100/len(head) > 30
}

// languageFor maps a file path to a programming language for reporting.
// It only covers the common cases; unrecognized files report "".
func languageFor(rel string) string {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".go":
		return "Go"
	case ".py":
		return "Python"
	case ".rs":
		return "Rust"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".js", ".jsx":
		return "JavaScript"
	case ".cs":
		return "C#"
	case ".java":
		return "Java"
	case ".kt", ".kts":
		return "Kotlin"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	case ".c", ".h":
		return "C"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hh":
		return "C++"
	case ".swift":
		return "Swift"
	case ".sh", ".bash", ".zsh":
		return "Shell"
	case ".sql":
		return "SQL"
	case ".html", ".htm":
		return "HTML"
	case ".css", ".scss", ".sass":
		return "CSS"
	case ".json":
		return "JSON"
	case ".xml":
		return "XML"
	case ".yaml", ".yml":
		return "YAML"
	case ".toml":
		return "TOML"
	case ".md", ".markdown":
		return "Markdown"
	}
	return ""
}

// Stats is a compact summary of a Build, used by /index and tests. It is
// what the command-layer should render rather than the raw file list.
type Stats struct {
	Root         string
	Files        int
	TotalBytes   int64
	Directories  int
	Languages    map[string]int
	GoPackages   int
	Symbols      int
	SkippedDirs  []string
	ReachedLimit bool
}

// statsOf folds the walked files and skipped directories into a Stats.
func statsOf(files []File, skipped []string, dirs int, cap bool) Stats {
	s := Stats{
		Directories:  dirs,
		Languages:    map[string]int{},
		SkippedDirs:  append([]string(nil), skipped...),
		ReachedLimit: cap,
	}
	goPkgs := map[string]bool{}
	for i := range files {
		f := &files[i]
		s.Files++
		s.TotalBytes += f.Size
		if f.Lang != "" {
			s.Languages[f.Lang]++
		}
		for _, pkg := range f.Packages {
			goPkgs[pkg] = true
		}
		s.Symbols += len(f.Symbols)
	}
	s.GoPackages = len(goPkgs)
	sort.Strings(s.SkippedDirs)
	return s
}

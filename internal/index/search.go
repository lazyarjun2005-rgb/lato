package index

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Match is one search result. It describes where the query matched so a
// caller (or eventually the model) can act on it without re-scanning the
// repository.
type Match struct {
	Path   string // slash-separated path relative to the workspace root
	Line   int    // matching line number, 0 when the match is path-only
	Column int    // matching column (1-based), 0 when unknown
	Text   string // the matching line, or a symbol description
	Kind   string // match kind: content, symbol, filename, or path
}

// Search holds the query the user wants applied.
//
// The default (all flags false) searches file names and paths only,
// which never touches file bodies or the filesystem beyond what the
// index already holds. Contents enables full-text search; Symbols
// additionally matches Go symbol names. CaseSensitive opts out of the
// default case-insensitive matching.
type Search struct {
	Query         string
	Max           int  // maximum results, 0 means DefaultMax
	Contents      bool // also search file contents
	Symbols       bool // also search Go symbol names
	CaseSensitive bool
}

// Bounds for a single search. DefaultMax keeps ordinary results focused;
// hardMax bounds Max itself and maxCollected bounds how many raw matches
// are gathered before ranking, so a pathological query (a single common
// letter) over a huge tree cannot exhaust memory. When maxCollected is
// hit the result is marked Truncated.
const (
	DefaultMax   = 20
	hardMax      = 1000
	maxCollected = 10_000

	// maxPerFile caps content matches from one file so a single hot file
	// cannot dominate the result set.
	maxPerFile = 5
)

// SearchResult is the outcome of a Search over a built index.
type SearchResult struct {
	Matches   []Match
	Count     int // total matches found before the Max cap was applied
	Truncated bool
}

// Search runs the configured search over the index. It is deterministic:
// matches are ordered by kind usefulness, then path, then line, so the
// same query against the same tree always yields the same results.
func (i *Index) Search(opts Search) SearchResult {
	if i == nil || !i.built {
		return SearchResult{}
	}

	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return SearchResult{}
	}
	max := opts.Max
	if max <= 0 {
		max = DefaultMax
	}
	if max > hardMax {
		max = hardMax
	}

	matches := i.collect(query, opts)
	total := len(matches)
	truncated := total > max
	sort.SliceStable(matches, func(a, b int) bool {
		x, y := matches[a], matches[b]
		if kindRank(x.Kind) != kindRank(y.Kind) {
			return kindRank(x.Kind) < kindRank(y.Kind)
		}
		if x.Path != y.Path {
			return x.Path < y.Path
		}
		return x.Line < y.Line
	})
	if truncated {
		matches = matches[:max]
	}
	return SearchResult{
		Matches:   matches,
		Count:     total,
		Truncated: truncated,
	}
}

// kindRank orders kinds by usefulness: a concrete content hit beats a
// symbol hit, which beats filename/path-only matches.
func kindRank(kind string) int {
	switch kind {
	case "content":
		return 0
	case "symbol":
		return 1
	case "filename":
		return 2
	default: // "path" and anything unrecognized
		return 3
	}
}

// collect gathers every match for query across names, paths, symbols,
// and — when enabled — contents. Files are visited in the index's sorted
// order, so early exit at maxCollected stays deterministic.
func (i *Index) collect(query string, opts Search) []Match {
	var matches []Match
	for _, f := range i.files {
		matchedName := false

		if containsOpt(f.Name, query, opts.CaseSensitive) {
			matches = append(matches, Match{Path: f.Path, Kind: "filename"})
			matchedName = true
		}
		// A base-name hit covers most name queries; record a separate
		// path match only when the query appears elsewhere in the path.
		if !matchedName && containsOpt(f.Path, query, opts.CaseSensitive) {
			matches = append(matches, Match{Path: f.Path, Kind: "path"})
		}

		if opts.Symbols {
			for _, s := range f.Symbols {
				if containsOpt(s.Name, query, opts.CaseSensitive) {
					matches = append(matches, Match{
						Path: f.Path,
						Line: s.Pos,
						Text: s.Kind + " " + s.Name,
						Kind: "symbol",
					})
				}
			}
		}

		if opts.Contents && !f.Binary {
			matches = append(matches, i.contentMatches(f, query, opts.CaseSensitive)...)
		}

		if len(matches) >= maxCollected {
			return matches[:maxCollected]
		}
	}
	return matches
}

// contentMatches returns one Match per matching line of the file body,
// capped at maxPerFile per file. When the indexed Body is truncated or
// was unreadable at index time, the rest of the file is streamed from
// disk under the same bounded-read rules, so content search finds
// needles anywhere in text files regardless of what indexing retained.
func (i *Index) contentMatches(f File, query string, caseSensitive bool) []Match {
	body := f.Body
	scanFrom := len(body)
	if f.Truncated && scanFrom > 0 {
		// Drop the final partial line; it is re-read from disk below.
		if idx := strings.LastIndexByte(body, '\n'); idx >= 0 {
			scanFrom = idx + 1
			body = body[:scanFrom]
		} else {
			scanFrom = 0
			body = ""
		}
	}

	var matches []Match
	add := func(line int, raw string) bool {
		col := indexOf(raw, query, caseSensitive)
		if col < 0 {
			return true
		}
		matches = append(matches, Match{
			Path:   f.Path,
			Line:   line,
			Column: col + 1,
			Text:   snippet(raw),
			Kind:   "content",
		})
		return len(matches) < maxPerFile
	}

	scanLines(body, 1, add)
	if len(matches) < maxPerFile && int64(scanFrom) < f.Size {
		i.scanDiskTail(f, scanFrom, 1+strings.Count(body[:scanFrom], "\n"), add)
	}
	return matches
}

// scanLines visits each line of body with 1-based line numbers until
// visit returns false.
func scanLines(body string, firstLine int, visit func(line int, raw string) bool) {
	line := firstLine
	for _, raw := range strings.Split(body, "\n") {
		if !visit(line, raw) {
			return
		}
		line++
	}
}

// scanDiskTail streams lines of the on-disk file starting after skip
// bytes (whose first line number is given), invoking visit per line
// until it returns false. It exists for files whose indexed Body was
// truncated or unreadable: the tail is read lazily, once per search,
// through a bounded buffered reader rather than loading the whole file.
func (i *Index) scanDiskTail(f File, skipBytes, firstLine int, visit func(line int, raw string) bool) {
	file, err := os.Open(filepath.Join(i.rootKey, filepath.FromSlash(f.Path)))
	if err != nil {
		return
	}
	defer file.Close()

	if skipBytes > 0 {
		if _, err := file.Seek(int64(skipBytes), io.SeekStart); err != nil {
			return
		}
	}

	reader := bufio.NewReaderSize(file, 64<<10)
	line := firstLine
	for {
		raw, err := reader.ReadString('\n')
		if raw == "" && err != nil {
			return
		}
		raw = strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		if !visit(line, raw) || err != nil { // err is io.EOF here
			return
		}
		line++
	}
}

// indexOf finds query in s, honoring the case-sensitivity flag. It
// returns the 0-based byte offset, or -1 when absent.
func indexOf(s, query string, caseSensitive bool) int {
	if caseSensitive {
		return strings.Index(s, query)
	}
	return strings.Index(strings.ToLower(s), strings.ToLower(query))
}

// containsOpt is indexOf's boolean wrapper.
func containsOpt(s, query string, caseSensitive bool) bool {
	return indexOf(s, query, caseSensitive) >= 0
}

// snippet trims whitespace and clips long lines to keep result output
// readable.
func snippet(line string) string {
	const maxSnippet = 160
	line = strings.TrimSpace(line)
	runes := []rune(line)
	if len(runes) > maxSnippet {
		return string(runes[:maxSnippet]) + "…"
	}
	return line
}

// Options carries settings for relevance retrieval.
type Options struct {
	MaxRootFiles int    // cap for the relevant-files list, 0 means default
	Query        string // optional natural-language question to score against
}

// defaultRelevantFiles is the list size used when Options.MaxRootFiles
// is unset.
const defaultRelevantFiles = 10

// Relevance returns up to n files likely useful for answering a
// repository question. Scoring combines structural signals present at
// index time (READMEs, manifests, shallow paths) with, when Options.Query
// is set, lexical overlap between the query's words and each file's
// name, path, Go symbols, and indexed content. It is deterministic:
// ties break by path.
func (i *Index) Relevance(opts Options) []File {
	if i == nil || !i.built {
		return nil
	}
	n := opts.MaxRootFiles
	if n <= 0 {
		n = defaultRelevantFiles
	}

	queryTerms := tokenize(opts.Query)

	type item struct {
		f      File
		key    string
		weight int
	}
	var items []item

	for _, f := range i.files {
		var score int
		if len(queryTerms) > 0 {
			// Query mode: relevance comes from the question's own words.
			// Structural bonuses shrink to a small tiebreaker so a deep
			// file about the subject outranks a root file that merely
			// exists.
			score = lexicalScore(f, queryTerms)
			if depth := strings.Count(f.Path, "/"); depth == 0 {
				score += 2
			}
			switch f.Name {
			case "README.md", "README":
				score += 3
			case "go.mod", "go.work", "package.json", "Cargo.toml", "pyproject.toml":
				score += 2
			}
		} else {
			// No-query mode: rank by structural importance alone.
			depth := strings.Count(f.Path, "/")
			switch {
			case depth == 0:
				score += 10
			case depth == 1:
				score += 4
			}
			switch f.Name {
			case "README.md", "README":
				score += 20
			case "LICENSE", "LICENSE.md", "COPYING":
				score += 8
			case "go.mod", "go.work", "package.json", "Cargo.toml", "pyproject.toml":
				score += 6
			case "Makefile", "Dockerfile", "docker-compose.yml", "compose.yaml":
				score += 5
			}
			if f.Lang != "" {
				score += 3
			}
		}
		if f.Binary {
			score = 0 // never recommend binaries
		}
		if score <= 0 {
			continue
		}
		items = append(items, item{f: f, key: f.Path, weight: score})
	}

	sort.SliceStable(items, func(a, b int) bool {
		if items[a].weight != items[b].weight {
			return items[a].weight > items[b].weight
		}
		return items[a].key < items[b].key
	})

	out := make([]File, 0, n)
	for j := 0; j < len(items) && j < n; j++ {
		out = append(out, items[j].f)
	}
	return out
}

// lexicalScore rewards files whose indexed metadata mentions the query's
// terms. Name and symbol hits are worth more than body hits because they
// identify purpose rather than incidental usage.
func lexicalScore(f File, terms []string) int {
	score := 0
	nameHit := false
	pathLower := strings.ToLower(f.Path)
	for _, t := range terms {
		if strings.Contains(strings.ToLower(f.Name), t) {
			nameHit = true
		}
		if strings.Contains(pathLower, t) {
			score++
		}
		for _, s := range f.Symbols {
			if strings.Contains(strings.ToLower(s.Name), t) {
				score += 2
				break
			}
		}
		body := strings.ToLower(f.Body)
		if body != "" && strings.Contains(body, t) {
			score++
		}
	}
	if nameHit {
		score += 4
	}
	return score
}

// tokenize lowercases text and splits it into words long enough to be
// meaningful for matching.
func tokenize(text string) []string {
	var terms []string
	for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '_')
	}) {
		if len(field) >= 3 {
			terms = append(terms, field)
		}
	}
	return terms
}

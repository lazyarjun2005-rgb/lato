package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo builds a small Go repo with known files for search tests.
func testRepo(t *testing.T) *Index {
	t.Helper()
	dir := writeTree(t, "repo",
		"go.mod",
		"main.go",
		"internal/server/server.go",
		"pkg/client/client.go",
		"README.md",
		"doc/guide.md",
	)
	writeFile(t, dir, "go.mod", "module example.com/demo\n")
	writeFile(t, dir, "main.go", `package main

import "example.com/demo/internal/server"

func main() { server.Run() }
`)
	writeFile(t, dir, "internal/server/server.go", `package server

type Server struct{ port int }

func Run() {}

func (s Server) Listen() {}
`)
	writeFile(t, dir, "pkg/client/client.go", `package client

type Client struct{}

func NewClient() *Client { return &Client{} }
`)
	writeFile(t, dir, "README.md", "# Demo\n\nHandles TLS connections here.\n")
	writeFile(t, dir, "doc/guide.md", "# Guide\n\nSee the server package for details.\n")

	return NewBuilder(dir).Build()
}

func TestSearchFilename(t *testing.T) {
	idx := testRepo(t)
	res := idx.Search(Search{Query: "server"})
	if res.Count == 0 {
		t.Fatal("no matches for filename query 'server'")
	}
	found := false
	for _, m := range res.Matches {
		if m.Kind == "filename" && filepath.Base(m.Path) == "server.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected filename match for server.go, got: %v", matchPaths(res.Matches))
	}
}

func TestSearchFilenameNotFound(t *testing.T) {
	idx := testRepo(t)
	res := idx.Search(Search{Query: "zzz_nothing_here"})
	if res.Count != 0 {
		t.Errorf("matches = %d, want 0", res.Count)
	}
}

func TestSearchPathSubstring(t *testing.T) {
	idx := testRepo(t)
	res := idx.Search(Search{Query: "pkg/client"})
	if res.Count == 0 {
		t.Fatal("no matches for path query 'pkg/client'")
	}
	for _, m := range res.Matches {
		if m.Kind == "filename" {
			continue
		}
		if !strings.Contains(m.Path, "pkg/client") {
			t.Errorf("path match %q does not contain pkg/client", m.Path)
		}
	}
}

func TestSearchContent(t *testing.T) {
	idx := testRepo(t)
	res := idx.Search(Search{Query: "TLS connections", Contents: true})
	if res.Count == 0 {
		t.Fatal("no content matches for 'TLS connections'")
	}
	var content *Match
	for _, m := range res.Matches {
		if m.Kind == "content" {
			m := m
			content = &m
			break
		}
	}
	if content == nil {
		t.Fatalf("no content-kind match: %v", matchPaths(res.Matches))
	}
	if content.Path != "README.md" {
		t.Errorf("content match path = %q, want README.md", content.Path)
	}
	if content.Line != 3 { // README: "# Demo", blank, then the sentence
		t.Errorf("content match line = %d, want 3", content.Line)
	}
	if !strings.Contains(content.Text, "TLS connections") {
		t.Errorf("content match text = %q, expected the matching line", content.Text)
	}
}

// TestSearchContentMultipleMatchesPerFile verifies one Match is produced
// per matching line, each with its own number and snippet.
func TestSearchContentMultipleMatchesPerFile(t *testing.T) {
	dir := writeTree(t, "multi", "multi.go")
	writeFile(t, dir, "multi.go", `package p

func a() { hit() }
func b() { other() }
func c() { hit() again() }
`)
	idx := NewBuilder(dir).Build()

	res := idx.Search(Search{Query: "hit", Contents: true})
	if res.Count != 2 {
		t.Fatalf("count = %d, want 2 (one per matching line)", res.Count)
	}
	lines := []int{res.Matches[0].Line, res.Matches[1].Line}
	if lines[0] != 3 || lines[1] != 5 {
		t.Errorf("match lines = %v, want [3 5]", lines)
	}
	for _, m := range res.Matches {
		if m.Column <= 0 {
			t.Errorf("match on line %d has column %d, want 1-based column", m.Line, m.Column)
		}
		if !strings.Contains(m.Text, "hit") {
			t.Errorf("match text %q missing the query", m.Text)
		}
	}
}

// TestSearchContentNestedSourceFile pins the Milestone 5 regression: a
// content needle inside a nested Go source file must be found with the
// right path and line even when the query looks nothing like the name.
func TestSearchContentNestedSourceFile(t *testing.T) {
	idx := testRepo(t)
	res := idx.Search(Search{Query: "server.Run()", Contents: true})
	var found bool
	for _, m := range res.Matches {
		if m.Kind != "content" {
			continue
		}
		found = true
		if m.Path != "main.go" || m.Line != 5 {
			t.Errorf("content match = %s:%d, want main.go:5", m.Path, m.Line)
		}
		if m.Column < 14 { // after "func main() { "
			t.Errorf("column = %d, expected within the statement", m.Column)
		}
	}
	if !found {
		t.Fatal("nested content not found")
	}
}

// TestSearchContentCaseSensitivity verifies the default insensitive
// matching and the CaseSensitive opt-out are both deterministic.
func TestSearchContentCaseSensitivity(t *testing.T) {
	dir := writeTree(t, "case", "doc.txt")
	writeFile(t, dir, "doc.txt", "MixedCase value\nlowercase value\n")
	idx := NewBuilder(dir).Build()

	insensitive := idx.Search(Search{Query: "mixedcase", Contents: true})
	if insensitive.Count != 1 {
		t.Errorf("insensitive count = %d, want 1", insensitive.Count)
	}

	sensitive := idx.Search(Search{Query: "MixedCase", Contents: true, CaseSensitive: true})
	if sensitive.Count != 1 || sensitive.Matches[0].Line != 1 {
		t.Errorf("sensitive result = %+v, want exactly line 1", sensitive.Matches)
	}

	sensitiveMiss := idx.Search(Search{Query: "mixedcase", Contents: true, CaseSensitive: true})
	if sensitiveMiss.Count != 0 {
		t.Errorf("sensitive miss count = %d, want 0", sensitiveMiss.Count)
	}
}

// TestSearchContentBinaryExcluded verifies binary files never yield
// content matches and are never opened for tail scanning.
func TestSearchContentBinaryExcluded(t *testing.T) {
	dir := writeTree(t, "binary", "logo.png", "note.txt")
	full := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(full, append([]byte{0x00, 0x01}, []byte("needle inside bytes")...), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "note.txt", "a real needle here\n")
	idx := NewBuilder(dir).Build()

	res := idx.Search(Search{Query: "needle", Contents: true})
	for _, m := range res.Matches {
		if strings.HasSuffix(m.Path, ".png") {
			t.Errorf("binary file returned a match: %+v", m)
		}
	}
	if res.Count != 1 {
		t.Errorf("count = %d, want exactly the text file match", res.Count)
	}
}

// TestSearchContentTruncatedFileTailScanned verifies that content
// present only in the part of an oversized file NOT kept at index time
// is still found by streaming the tail from disk.
func TestSearchContentTruncatedFileTailScanned(t *testing.T) {
	dir := writeTree(t, "tail", "huge.log")
	head := strings.Repeat("filler\n", maxTextBytes) // > maxTextBytes total
	needle := "\nthe_tail_needle\n"
	writeFile(t, dir, "huge.log", head+needle)

	idx := NewBuilder(dir).Build()
	f, ok := idx.Lookup("huge.log")
	if !ok || !f.Truncated {
		t.Fatalf("huge.log should be indexed as truncated, got %+v ok=%v", f, ok)
	}

	res := idx.Search(Search{Query: "the_tail_needle", Contents: true})
	if res.Count == 0 {
		t.Fatal("tail content beyond the indexed prefix was not found")
	}
	m := res.Matches[0]
	if m.Path != "huge.log" || m.Line < 2 {
		t.Errorf("match = %+v, want huge.log with a line number past the prefix", m)
	}
}

// TestSearchContentIgnoresGitignoredDirs verifies content search does
// not leak matches from ignored trees.
func TestSearchContentIgnoresGitignoredDirs(t *testing.T) {
	dir := writeTree(t, "leaky", "main.go", "vendor/x/x.go")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "vendor/x/x.go", "package x\n\nvar SecretNeedle = 1\n")

	idx := NewBuilder(dir).Build()
	res := idx.Search(Search{Query: "SecretNeedle", Contents: true})
	if res.Count != 0 {
		t.Errorf("matches = %v, want none from ignored vendor/", matchPaths(res.Matches))
	}
}

// TestSearchEmptyRepository verifies searching an empty workspace is a
// clean no-op rather than an error.
func TestSearchEmptyRepository(t *testing.T) {
	idx := NewBuilder(t.TempDir()).Build()

	for _, opts := range []Search{
		{Query: "main.go"},
		{Query: "anything", Contents: true},
		{Query: "Anything", Symbols: true},
	} {
		res := idx.Search(opts)
		if res.Count != 0 || len(res.Matches) != 0 || res.Truncated {
			t.Errorf("empty repo search %+v = %+v, want zero results", opts, res)
		}
	}
}

func TestSearchSymbols(t *testing.T) {
	idx := testRepo(t)
	res := idx.Search(Search{Query: "NewClient", Symbols: true})
	if res.Count == 0 {
		t.Fatal("no symbol matches for 'NewClient'")
	}
	var symbol *Match
	for _, m := range res.Matches {
		if m.Kind == "symbol" {
			m := m
			symbol = &m
			break
		}
	}
	if symbol == nil {
		t.Fatalf("no symbol-kind match: %v", matchPaths(res.Matches))
	}
	if symbol.Path != "pkg/client/client.go" {
		t.Errorf("symbol match path = %q, want pkg/client/client.go", symbol.Path)
	}
}

func TestSearchSymbolsRequiresFlag(t *testing.T) {
	idx := testRepo(t)
	// Without Symbols=true a symbol query still finds the file via the
	// filename, but must not report a symbol-kind match.
	res := idx.Search(Search{Query: "NewClient"})
	for _, m := range res.Matches {
		if m.Kind == "symbol" {
			t.Errorf("no-symbol search returned symbol match %v", m)
		}
	}
}

func TestSearchMaxAndTruncation(t *testing.T) {
	dir := writeTree(t, "many", "a.go", "b.go", "c.go", "d.go")
	for _, f := range []string{"a.go", "b.go", "c.go", "d.go"} {
		writeFile(t, dir, f, "package p\n")
	}
	idx := NewBuilder(dir).Build()

	res := idx.Search(Search{Query: ".go", Max: 2})
	if len(res.Matches) != 2 {
		t.Errorf("len(matches) = %d, want 2", len(res.Matches))
	}
	if !res.Truncated {
		t.Error("expected Truncated=true for capped result")
	}
	if res.Count < 4 {
		t.Errorf("res.Count = %d, want at least 4 total matches before cap", res.Count)
	}
}

// TestSearchDeterministicAssertsEqualResultsOnRebuild ensures the same
// query against rebuilt indexes yields identical results (sorted).
func TestSearchDeterministicOrdering(t *testing.T) {
	dir := writeTree(t, "det", "b.go", "a.go", "c.go")
	writeFile(t, dir, "a.go", "package p\n")
	writeFile(t, dir, "b.go", "package p\n")
	writeFile(t, dir, "c.go", "package p\n")

	first := NewBuilder(dir).Build().Search(Search{Query: ".go"})
	second := NewBuilder(dir).Build().Search(Search{Query: ".go"})

	if first.Count != second.Count {
		t.Fatalf("counts differ: %d vs %d", first.Count, second.Count)
	}
	for i := range first.Matches {
		if first.Matches[i].Path != second.Matches[i].Path {
			t.Errorf("position %d differs: %q vs %q", i, first.Matches[i].Path, second.Matches[i].Path)
		}
	}
}

// TestSearchSymbolIncludesLineAndKind verifies symbol matches report the
// declaration line and kind.
func TestSearchSymbolIncludesLineAndKind(t *testing.T) {
	idx := testRepo(t)
	res := idx.Search(Search{Query: "Listen", Symbols: true})
	var found bool
	for _, m := range res.Matches {
		if m.Kind != "symbol" {
			continue
		}
		found = true
		if m.Line == 0 {
			t.Errorf("symbol match %+v missing declaration line", m)
		}
		if m.Text != "method Listen" {
			t.Errorf("symbol text = %q, want \"method Listen\"", m.Text)
		}
	}
	if !found {
		t.Fatal("no symbol match for Listen")
	}
}

// TestSearchContentPerFileCap verifies one hot file cannot dominate the
// result list.
func TestSearchContentPerFileCap(t *testing.T) {
	dir := writeTree(t, "hot", "hot.txt", "cold.txt")
	var b strings.Builder
	b.WriteString("start\n")
	for i := 0; i < 50; i++ {
		b.WriteString("needle here\n")
	}
	writeFile(t, dir, "hot.txt", b.String())
	writeFile(t, dir, "cold.txt", "needle once\n")
	idx := NewBuilder(dir).Build()

	res := idx.Search(Search{Query: "needle", Contents: true})
	hits := 0
	for _, m := range res.Matches {
		if m.Path == "hot.txt" {
			hits++
		}
	}
	if hits > maxPerFile {
		t.Errorf("hot.txt produced %d content matches, cap is %d", hits, maxPerFile)
	}
}

// TestSearchCollectBound verifies a tiny Max yields capped, truncated,
// deterministic results even for a query matching nearly every line.
func TestSearchCollectBound(t *testing.T) {
	dir := writeTree(t, "flood", "f1.txt", "f2.txt")
	filler := strings.Repeat("x ", 40) + "\n"
	var body strings.Builder
	for i := 0; i < 300; i++ {
		body.WriteString(filler)
	}
	writeFile(t, dir, "f1.txt", body.String())
	writeFile(t, dir, "f2.txt", body.String())
	idx := NewBuilder(dir).Build()

	res := idx.Search(Search{Query: "x", Contents: true, Max: 5})
	if len(res.Matches) != 5 || !res.Truncated || res.Count <= len(res.Matches) {
		t.Errorf("result = %d matches, count %d, truncated %v; want capped+truncated",
			len(res.Matches), res.Count, res.Truncated)
	}
}

// TestRelevanceScoresAgainstQuery verifies query-aware retrieval ranks a
// question's subject files above generic structural picks.
func TestRelevanceScoresAgainstQuery(t *testing.T) {
	idx := testRepo(t)

	rel := idx.Relevance(Options{MaxRootFiles: 3, Query: "how does the client connect"})
	if len(rel) == 0 {
		t.Fatal("relevance returned no files")
	}
	found := false
	for _, f := range rel {
		if f.Path == "pkg/client/client.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("pkg/client/client.go missing from top-3 for a client query: %v", rel)
	}
}

// TestSearchArbitraryRootOutsideLatoSource pins Milestone 4.5 behavior:
// an index built over any absolute directory searches that directory,
// never Lato's own source tree.
func TestSearchArbitraryRootOutsideLatoSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "nested/prog/main.go", "package main\n\nfunc runProgram() { fmt.Println(\"unique_marker\") }\n")

	idx := NewBuilder(dir).Build()
	res := idx.Search(Search{Query: "unique_marker", Contents: true})
	if res.Count == 0 {
		t.Fatal("content not found under arbitrary root")
	}
	m := res.Matches[0]
	if m.Path != "nested/prog/main.go" {
		t.Errorf("match path = %q, want nested/prog/main.go with forward slashes", m.Path)
	}
	if _, leak := idx.Lookup("internal/runtime/runtime.go"); leak {
		t.Error("index leaked Lato source files into an external workspace")
	}
}

func TestRelevanceIncludesRootFiles(t *testing.T) {
	idx := testRepo(t)
	rel := idx.Relevance(Options{MaxRootFiles: 8})
	if len(rel) == 0 {
		t.Fatal("Relevance returned no files")
	}
	// README should be highly ranked for a repository question.
	hasREADME := false
	for _, f := range rel {
		if f.Name == "README.md" {
			hasREADME = true
		}
	}
	if !hasREADME {
		t.Errorf("README.md missing from relevance: %v", rel)
	}
}

func TestLookup(t *testing.T) {
	idx := testRepo(t)
	f, ok := idx.Lookup("main.go")
	if !ok || f.Name != "main.go" {
		t.Fatalf("Lookup(main.go) = %+v, %v", f, ok)
	}
	if _, ok := idx.Lookup("nope.go"); ok {
		t.Error("Lookup(nope.go) found a file")
	}
}

func matchPaths(matches []Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Path + ":" + m.Kind
	}
	return out
}

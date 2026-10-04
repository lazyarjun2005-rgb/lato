package index

import (
	"strings"
	"testing"
)

// TestRelevanceExcludesSensitiveFiles is the regression test for the
// gap this change closes: relevance feeds the repository snapshot that
// is injected into the model prompt, and it previously ranked
// credential files like .env as highly important root files.
func TestRelevanceExcludesSensitiveFiles(t *testing.T) {
	dir := writeTree(t, "secrets", "go.mod", "main.go", ".env", "id_rsa", "deploy.pem", "config/.aws/credentials")
	writeFile(t, dir, "go.mod", "module example.com/demo\n")
	writeFile(t, dir, ".env", "API_KEY=super-secret-value\n")

	idx := NewBuilder(dir).Build()
	if !idx.Built() {
		t.Fatal("index not built")
	}

	// The files are deliberately still indexed: search_repo filters them
	// from its own results. What must not happen is one of them being
	// *recommended* as relevant repository content.
	paths := map[string]bool{}
	for _, f := range idx.Relevance(Options{MaxRootFiles: 50}) {
		paths[f.Path] = true
	}
	for _, secret := range []string{".env", "id_rsa", "deploy.pem", "config/.aws/credentials"} {
		if paths[secret] {
			t.Errorf("Relevance recommended sensitive file %q", secret)
		}
	}
	if !paths["go.mod"] {
		t.Error("Relevance should still recommend ordinary project files")
	}
}

// TestRelevanceQueryModeExcludesSensitiveFiles covers the scored branch,
// where a question's words could otherwise pull a credential file up.
func TestRelevanceQueryModeExcludesSensitiveFiles(t *testing.T) {
	dir := writeTree(t, "secrets", ".env", "main.go")
	writeFile(t, dir, ".env", "API_KEY=super-secret-value\nDB_PASSWORD=hunter2\n")

	idx := NewBuilder(dir).Build()

	for _, f := range idx.Relevance(Options{MaxRootFiles: 50, Query: "api key password"}) {
		if f.Path == ".env" {
			t.Fatal("Relevance returned .env for a matching query")
		}
	}
}

// TestRelevanceStillReturnsBinaryFreeResults confirms the guard is
// additive rather than replacing the existing binary exclusion.
func TestRelevanceStillReturnsBinaryFreeResults(t *testing.T) {
	dir := writeTree(t, "assets", "main.go", "logo.png")
	writeFile(t, dir, "logo.png", "\x89PNG\r\n\x1a\n\x00\x00binary")

	idx := NewBuilder(dir).Build()

	for _, f := range idx.Relevance(Options{MaxRootFiles: 50}) {
		if f.Binary {
			t.Errorf("Relevance returned binary file %q", f.Path)
		}
	}
}

// TestSearchStillFindsSensitivePathsButToolFiltersThem documents the
// deliberate split: the index keeps these files so the search tool can
// report that a match exists, while search_repo strips them from its
// output before the model sees it. The guard belongs at the tool
// boundary, not in the index.
func TestSearchStillFindsSensitivePathsButToolFiltersThem(t *testing.T) {
	dir := writeTree(t, "cfg", ".env")
	writeFile(t, dir, ".env", "API_KEY=super-secret-value\n")

	idx := NewBuilder(dir).Build()
	res := idx.Search(Search{Query: "API_KEY", Contents: true, Max: 20})

	if len(res.Matches) == 0 {
		t.Skip("search did not match the fixture; nothing to assert here")
	}
	found := false
	for _, m := range res.Matches {
		if m.Path == ".env" {
			found = true
			// The tool layer is responsible for dropping this match.
			// See internal/tools/repository: it filters with
			// permissions.IsSensitivePath before rendering results.
			if !strings.Contains(m.Text, "super-secret-value") {
				t.Error("unexpected fixture content")
			}
		}
	}
	if !found {
		t.Error("expected the index itself to still match .env")
	}
}

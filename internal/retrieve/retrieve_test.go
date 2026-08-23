package retrieve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/index"
)

// buildIndex walks dir and returns its index, the way the runtime does.
func buildIndex(t *testing.T, dir string) *index.Index {
	t.Helper()
	idx := index.NewBuilder(dir).Build()
	if !idx.Built() {
		t.Fatal("index not built")
	}
	return idx
}

// writeRepo creates a small Go repository under a temp directory with
// the given files (slash-separated relative path → content).
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func sampleRepo() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/demo\n\ngo 1.26\n",
		"main.go": `package main

import "example.com/demo/greet"

func main() {
	fmt.Println("Hello")
	greet.Hello()
}
`,
		"greet/greet.go": `package greet

import "fmt"

// Hello prints a greeting to the world.
func Hello() {
	fmt.Println("hello from greet")
}

// Farewell says goodbye.
func Farewell() {
	fmt.Println("goodbye from greet")
}
`,
		"unrelated/notes.txt": "shopping list: apples, bananas\nnothing about code here\n",
	}
}

// TestQuestionRetrievesSourceEvidence covers the headline M8 behavior:
// asking about the main function surfaces actual main.go source lines.
func TestQuestionRetrievesSourceEvidence(t *testing.T) {
	dir := writeRepo(t, sampleRepo())
	ev := ForQuestion(buildIndex(t, dir), "demo", "How does the main function work?")

	if ev.Empty() {
		t.Fatal("retrieval found nothing for a main-function question")
	}

	// The primary file must be main.go, not the unrelated notes.
	if ev.Files[0].Path != "main.go" {
		t.Fatalf("first evidence file = %q, want main.go (got: %v)", ev.Files[0].Path, evidencePaths(ev))
	}

	text := ev.Text()
	for _, want := range []string{"main.go", "func main", "greet.Hello", "Declarations:"} {
		if !strings.Contains(text, want) {
			t.Errorf("evidence text missing %q:\n%s", want, text)
		}
	}
}

// TestUsageQuestionFindsContentMatches verifies "where is X used?"
// style questions retrieve the files containing the usage.
func TestUsageQuestionFindsContentMatches(t *testing.T) {
	dir := writeRepo(t, sampleRepo())
	ev := ForQuestion(buildIndex(t, dir), "demo", `Where is fmt.Println used?`)

	if ev.Empty() {
		t.Fatal("retrieval found nothing for a fmt.Println usage question")
	}
	foundMain, foundGreet := false, false
	for _, f := range ev.Files {
		switch f.Path {
		case "main.go":
			foundMain = true
		case "greet/greet.go":
			foundGreet = true
		}
	}
	if !foundMain || !foundGreet {
		t.Errorf("evidence missing usage sites: %v", evidencePaths(ev))
	}
}

// TestSymbolInformationIsUsed checks that symbol declarations appear in
// the evidence for the winning files.
func TestSymbolInformationIsUsed(t *testing.T) {
	dir := writeRepo(t, sampleRepo())
	ev := ForQuestion(buildIndex(t, dir), "demo", "explain the greet package")

	text := ev.Text()
	if !strings.Contains(text, "function Hello") || !strings.Contains(text, "function Farewell") {
		t.Errorf("evidence missing greet symbol declarations:\n%s", text)
	}
}

// TestImportRelatedFilesAreFollowed verifies the import-relationship
// layer: evidence for main.go should mention greet/greet.go as related.
func TestImportRelatedFilesAreFollowed(t *testing.T) {
	dir := writeRepo(t, sampleRepo())
	ev := ForQuestion(buildIndex(t, dir), "demo", "the main entry point")

	var foundRelated bool
	for _, f := range ev.Files {
		if f.Path != "main.go" {
			continue
		}
		for _, rel := range f.Related {
			if rel.Path == "greet/greet.go" {
				foundRelated = true
				if rel.Via == "" {
					t.Error("related file missing the import it came via")
				}
			}
		}
	}
	if !foundRelated {
		t.Error("main.go evidence does not follow its import to greet/greet.go")
	}
}

// TestUnrelatedFilesExcluded pins precision: a question about code must
// not drag in unrelated text files.
func TestUnrelatedFilesExcluded(t *testing.T) {
	dir := writeRepo(t, sampleRepo())
	ev := ForQuestion(buildIndex(t, dir), "demo", "How does the Farewell function work?")

	for _, f := range ev.Files {
		if f.Path == "unrelated/notes.txt" {
			t.Errorf("unrelated file included in evidence: %v", evidencePaths(ev))
		}
	}
	if !strings.Contains(ev.Text(), "Farewell") {
		t.Errorf("evidence missing the asked-about symbol:\n%s", ev.Text())
	}
}

// TestBoundedContext verifies the rendered block respects its bounds
// even for a file engineered to match everywhere.
func TestBoundedContext(t *testing.T) {
	var b strings.Builder
	b.WriteString("package big\n\n")
	for i := 0; i < 2000; i++ {
		b.WriteString("// widget processing line for the widget factory\n")
	}
	dir := writeRepo(t, map[string]string{
		"go.mod":    "module example.com/big\n",
		"widget.go": b.String(),
	})

	ev := ForQuestion(buildIndex(t, dir), "big", "how does the widget factory work")
	if ev.Empty() {
		t.Fatal("no evidence for widget question")
	}

	text := ev.Text()
	if len(text) > maxEvidenceBytes {
		t.Errorf("evidence text = %d bytes, bound is %d", len(text), maxEvidenceBytes)
	}
	// Excerpt lines per file are bounded even with thousands of matches.
	for _, f := range ev.Files {
		lines := 0
		for _, ex := range f.Excerpts {
			lines += len(ex.Lines)
		}
		if lines > maxExcerptLines {
			t.Errorf("file %s has %d excerpt lines, bound is %d", f.Path, lines, maxExcerptLines)
		}
	}
}

// TestArbitraryWorkspaceRootIsRespected verifies retrieval operates on
// the index of the given root only — a second, unrelated repository's
// files must never appear.
func TestArbitraryWorkspaceRootIsRespected(t *testing.T) {
	other := writeRepo(t, map[string]string{
		"go.mod":           "module example.com/other\n",
		"main.go":          "package main\n\nfunc main() {}\n",
		"secret/secret.go": "package secret\n\n// SecretSauce is a special function\nfunc SecretSauce() {}\n",
	})
	_ = other

	dir := writeRepo(t, sampleRepo())
	ev := ForQuestion(buildIndex(t, dir), "demo", "how does SecretSauce work")
	if !ev.Empty() {
		t.Errorf("evidence leaked from another workspace: %v", evidencePaths(ev))
	}
}

// TestEmptyAndDegenerateInputs covers the never-fail contract.
func TestEmptyAndDegenerateInputs(t *testing.T) {
	if ev := ForQuestion(nil, "demo", "anything"); !ev.Empty() {
		t.Error("nil index must yield empty evidence")
	}
	dir := writeRepo(t, sampleRepo())
	idx := buildIndex(t, dir)
	if ev := ForQuestion(idx, "demo", ""); !ev.Empty() {
		t.Error("empty question must yield empty evidence")
	}
	if ev := ForQuestion(idx, "demo", "the of and to"); !ev.Empty() && len(ev.Files) != 0 {
		// Stop-word-only questions may legitimately match files via
		// body content; they must at least not panic and stay bounded.
		if len(ev.Text()) > maxEvidenceBytes {
			t.Error("evidence exceeds bound")
		}
	}
}

// TestEvidenceBoundsAreCompact pins the reduced prompt-size budget:
// these caps exist so local CPU-only models are not stalled by huge
// evidence blocks. If a change raises them, it must justify the extra
// prompt tokens.
func TestEvidenceBoundsAreCompact(t *testing.T) {
	if maxEvidenceBytes != 2<<10 {
		t.Errorf("maxEvidenceBytes = %d, want %d", maxEvidenceBytes, 2<<10)
	}
	if maxFiles != 3 {
		t.Errorf("maxFiles = %d, want 3", maxFiles)
	}
	if maxExcerptLines != 12 {
		t.Errorf("maxExcerptLines = %d, want 12", maxExcerptLines)
	}
	if maxSymbolsShown != 5 {
		t.Errorf("maxSymbolsShown = %d, want 5", maxSymbolsShown)
	}
}

// TestManyMatchesStayWithinFileCap verifies the file cap behaviorally:
// a question matching many files still yields at most maxFiles.
func TestManyMatchesStayWithinFileCap(t *testing.T) {
	files := map[string]string{"go.mod": "module example.com/wide\n"}
	for i := 0; i < 10; i++ {
		files[fmt.Sprintf("pkg%d/pkg.go", i)] = "package pkg\n\n// WidgetProcessor assembles widgets.\nfunc WidgetProcessor() {}\n"
	}
	dir := writeRepo(t, files)

	ev := ForQuestion(buildIndex(t, dir), "wide", "where is WidgetProcessor implemented")
	if ev.Empty() {
		t.Fatal("no evidence for a question matching ten files")
	}
	if len(ev.Files) > maxFiles {
		t.Errorf("evidence has %d files, cap is %d", len(ev.Files), maxFiles)
	}
	if got := len(ev.Text()); got > maxEvidenceBytes {
		t.Errorf("evidence text = %d bytes, cap is %d", got, maxEvidenceBytes)
	}
}

func evidencePaths(ev *Evidence) []string {
	paths := make([]string, len(ev.Files))
	for i, f := range ev.Files {
		paths[i] = f.Path
	}
	return paths
}

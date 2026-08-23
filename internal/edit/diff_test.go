package edit

import (
	"strings"
	"testing"
)

func TestDiffIdenticalContentIsEmpty(t *testing.T) {
	if got := Diff("f.txt", "same\n", "same\n"); got != "" {
		t.Errorf("Diff of identical content = %q, want empty", got)
	}
}

func TestDiffSingleLineReplacement(t *testing.T) {
	got := Diff("main.go",
		"package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n",
		"package main\n\nfunc main() {\n\tfmt.Println(\"Hello from Lato\")\n}\n")

	for _, want := range []string{
		"--- main.go",
		"+++ main.go",
		"@@ -1,5 +1,5 @@", // context fills the small file: all five lines
		"-	fmt.Println(\"Hello\")",
		"+	fmt.Println(\"Hello from Lato\")",
		" func main() {",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q:\n%s", want, got)
		}
	}
}

func TestDiffMultilineReplacementIsPaired(t *testing.T) {
	before := "func a() {\n\told()\n\told()\n}\n\nfunc b() {}\n"
	after := "func a() {\n\tnew()\n}\n\nfunc b() {}\n"

	got := Diff("code.go", before, after)
	if !strings.Contains(got, "-\told()\n-\told()\n+\tnew()") {
		t.Errorf("expected removed lines paired with their replacement in one block:\n%s", got)
	}
	if strings.Count(got, "@@ -") != 1 { // one hunk header (its closing @@ is part of the same line)
		t.Errorf("expected a single hunk for one local change:\n%s", got)
	}
}

func TestDiffAdditionAndPureDeletion(t *testing.T) {
	added := Diff("f.txt", "one\ntwo\n", "one\ntwo\nthree\n")
	if !strings.Contains(added, "+three") {
		t.Errorf("addition missing the new line:\n%s", added)
	}
	if strings.Contains(added, "\n-") {
		t.Errorf("pure addition rendered deletions:\n%s", added)
	}

	deleted := Diff("f.txt", "one\ntwo\nthree\n", "one\n")
	if !strings.Contains(deleted, "-two") || !strings.Contains(deleted, "-three") {
		t.Errorf("deletion missing removed lines:\n%s", deleted)
	}
	if strings.Count(deleted, "@@ -") != 1 {
		t.Errorf("pure deletion expected exactly one hunk:\n%s", deleted)
	}
	if strings.Contains(deleted, "-one") { // kept line must not be rendered as removed
		t.Errorf("deletion rendered an unchanged line as removed:\n%s", deleted)
	}
}

func TestDiffDistantChangesProduceSeparateHunks(t *testing.T) {
	var b strings.Builder
	b.WriteString("start\n")
	for i := 0; i < 20; i++ { // more than 2*diffContext filler lines
		b.WriteString("filler\n")
	}
	b.WriteString("end\n")
	full := b.String()

	changed := strings.Replace(full, "filler", "FILLER", -1)
	// Change first and last filler lines only; they are far apart.
	lines := strings.SplitAfter(full, "\n")
	lines[1] = "CHANGED-FIRST\n"
	lines[20] = "CHANGED-LAST\n"
	sparse := strings.Join(lines, "")

	got := Diff("wide.txt", full, sparse)
	if strings.Count(got, "@@ -") != 2 {
		t.Errorf("expected two hunks for distant changes, got:\n%s", got)
	}
	_ = changed
}

func TestDiffContextLinesAreIncluded(t *testing.T) {
	before := "c1\nc2\nc3\nOLD\nc5\nc6\nc7\n"
	after := strings.Replace(before, "OLD", "NEW", 1)

	got := Diff("f.txt", before, after)
	for _, ctx := range []string{" c1", " c2", " c3", " c5", " c6", " c7"} {
		if !strings.Contains(got, ctx+"\n") {
			t.Errorf("hunk missing context %q:\n%s", ctx, got)
		}
	}
}

func TestDiffNormalizesLineEndings(t *testing.T) {
	crlfBefore := "a\r\nb\r\nc\r\n"
	lfAfter := "a\nB\nc\n"

	got := Diff("f.txt", crlfBefore, lfAfter)
	if !strings.Contains(got, "-b") || !strings.Contains(got, "+B") {
		t.Errorf("CRLF input was not normalized:\n%q", got)
	}
	if strings.ContainsRune(got, '\r') {
		t.Errorf("diff output contains carriage returns:\n%q", got)
	}
}

func TestDiffOnlyEndingStyleChangeIsEmpty(t *testing.T) {
	if got := Diff("f.txt", "a\r\nb\r\n", "a\nb\n"); got != "" {
		t.Errorf("line-ending-only change produced a diff:\n%q", got)
	}
}

func TestDiffEmptyToContentAndBack(t *testing.T) {
	created := Diff("new.txt", "", "package main\n")
	if !strings.Contains(created, "+package main") {
		t.Errorf("creation diff missing added lines:\n%s", created)
	}

	emptied := Diff("gone.txt", "data\n", "")
	if !strings.Contains(emptied, "-data") {
		t.Errorf("emptying diff missing removed lines:\n%s", emptied)
	}
}

// TestDiffMatchesEditResult pins the integration contract between the
// engine's Result fields and the renderer.
func TestDiffMatchesEditResult(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "main.go", "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n")

	ws := NewWorkspace(dir)
	res := mustReplace(t, ws, "main.go", "\"Hello\"", "\"Hello from Lato\"")

	d := Diff(res.Path, res.Before, res.After)
	for _, want := range []string{"--- main.go", "-\tfmt.Println(\"Hello\")", "+\tfmt.Println(\"Hello from Lato\")"} {
		if !strings.Contains(d, want) {
			t.Errorf("result diff missing %q:\n%s", want, d)
		}
	}
}

package retrieve

import (
	"strings"
	"testing"

	"lato/internal/permissions"
)

// TestForQuestionExcludesSensitiveFiles is the regression test for the
// gap this change closes. Retrieval excerpts are injected into the model
// prompt automatically, with no tool call and therefore no filter at the
// tool boundary, so a credential file matching the question's own words
// was previously quoted straight into the context.
func TestForQuestionExcludesSensitiveFiles(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"go.mod": "module example.com/demo\n\ngo 1.26\n",
		".env": "API_KEY=super-secret-value-123\nDB_PASSWORD=hunter2\n" +
			"api_key=another-secret\ndatabase_password=hunter3\n",
		"main.go": "package main\n\n// API_KEY placeholder in ordinary source.\nfunc main() {}\n",
	})

	idx := buildIndex(t, dir)

	for _, question := range []string{
		"what is the API_KEY",
		"database password",
		"show me the api key value",
	} {
		ev := ForQuestion(idx, "demo", question)
		text := ev.Text()
		if strings.Contains(text, "super-secret-value-123") || strings.Contains(text, "hunter2") {
			t.Errorf("question %q leaked credential content into the evidence:\n%s", question, text)
		}
		if strings.Contains(text, ".env") {
			t.Errorf("question %q named a credential file in the evidence:\n%s", question, text)
		}
	}
}

// TestForQuestionExcludesSensitiveRelatedFiles is the regression test for
// the one path the primary-file filter did not cover. relatedFiles walks
// the unfiltered index to follow Go imports, so a credential file that is
// itself Go source was admitted as a "Related" entry — contributing its
// path, package name, and declaration names to the prompt. Bodies are
// never quoted for related files, so the exposure is name-level, but the
// names still identify the credential.
func TestForQuestionExcludesSensitiveRelatedFiles(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"go.mod": "module example.com/demo\n\ngo 1.22\n",
		// The primary file imports a path whose last segment is "aws",
		// which is what makes the credential file a related candidate.
		"main.go": "package main\n\nimport \"example.com/demo/aws\"\n\n" +
			"// Configure selects the deployment region.\n" +
			"func Configure() string { return aws.Region }\n",
		// An ordinary package that legitimately matches the import.
		"aws/aws.go": "package aws\n\n// Region is the deployment region.\nconst Region = \"us-east-1\"\n",
		// The credential file: sensitive path AND a matching package name.
		".aws/credentials.go": "package aws\n\n" +
			"// AWSSecretKey is the real credential.\n" +
			"const AWSSecretKey = \"wJalrXUtnFEMI-K7MDENG-bPxRfiCYEXAMPLEKEY\"\n",
	})

	idx := buildIndex(t, dir)

	// Precondition: the fixture really is a reachable candidate, so this
	// test cannot pass vacuously if the fixture stops matching.
	if _, ok := idx.Lookup(".aws/credentials.go"); !ok {
		t.Fatal("fixture precondition failed: .aws/credentials.go is not indexed")
	}
	if !permissions.IsSensitivePath(".aws/credentials.go") {
		t.Fatal("fixture precondition failed: .aws/credentials.go is not classified sensitive")
	}

	ev := ForQuestion(idx, "demo", "where is Configure defined")
	out := ev.Text()
	t.Logf("evidence:\n%s", out)

	if strings.Contains(out, ".aws/credentials.go") {
		t.Errorf("evidence names a sensitive path:\n%s", out)
	}
	if strings.Contains(out, "AWSSecretKey") {
		t.Errorf("evidence names a sensitive declaration:\n%s", out)
	}
	// The sensitive VALUE must never appear either.
	if strings.Contains(out, "wJalrXUtnFEMI") {
		t.Errorf("evidence leaked credential content:\n%s", out)
	}
	// The ordinary related file must still be reported: the guard is
	// additive and must not disable import-following entirely.
	if !strings.Contains(out, "aws/aws.go") {
		t.Errorf("evidence should still list the ordinary related file:\n%s", out)
	}
}

// TestRelatedFilesSkipsSensitiveCandidates unit-tests the guard directly,
// so a future refactor cannot quietly drop it without this failing.
func TestRelatedFilesSkipsSensitiveCandidates(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"go.mod": "module example.com/demo\n\ngo 1.22\n",
		"main.go": "package main\n\nimport \"example.com/demo/aws\"\n\n" +
			"func Configure() string { return aws.Region }\n",
		"aws/aws.go":          "package aws\n\nconst Region = \"us-east-1\"\n",
		".aws/credentials.go": "package aws\n\nconst AWSSecretKey = \"wJalrXUtnFEMI\"\n",
	})

	idx := buildIndex(t, dir)
	main, ok := idx.Lookup("main.go")
	if !ok {
		t.Fatal("main.go not indexed")
	}

	related := relatedFiles(idx, &main, map[string]bool{}, map[string]bool{})

	for _, r := range related {
		if permissions.IsSensitivePath(r.Path) {
			t.Errorf("relatedFiles returned sensitive path %q", r.Path)
		}
	}
	for _, r := range related {
		if r.Path == "aws/aws.go" {
			return // correct behavior: ordinary related file still found
		}
	}
	t.Errorf("relatedFiles found no ordinary related file; got %+v", related)
}

// TestForQuestionStillReturnsOrdinaryEvidence confirms the guard drops
// only credential paths: a question matching ordinary source still
// produces excerpts.
func TestForQuestionStillReturnsOrdinaryEvidence(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"go.mod":  "module example.com/demo\n\ngo 1.26\n",
		"main.go": "package main\n\n// Greet says hello to the caller.\nfunc Greet() string { return \"hello\" }\n",
	})

	idx := buildIndex(t, dir)

	ev := ForQuestion(idx, "demo", "where is Greet defined")
	if ev.Empty() {
		t.Fatal("expected evidence for an ordinary question")
	}
	if !strings.Contains(ev.Text(), "Greet") {
		t.Errorf("evidence missing the expected symbol:\n%s", ev.Text())
	}
}

// TestForQuestionHandlesNilIndex keeps the existing guard behavior.
func TestForQuestionHandlesNilIndex(t *testing.T) {
	if ev := ForQuestion(nil, "demo", "what is this"); ev == nil {
		t.Fatal("ForQuestion(nil) returned nil")
	} else if !ev.Empty() {
		t.Error("ForQuestion(nil) should produce no evidence")
	}
}

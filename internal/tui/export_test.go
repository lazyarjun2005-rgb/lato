package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/command"
	"lato/internal/session"
)

// newExportTestModel builds a model over a real multi-turn session in
// an isolated CWD, ready for /export dispatch.
func newExportTestModel(t *testing.T) model {
	t.Helper()
	rt := newPickerTestRuntime(t)
	t.Chdir(t.TempDir())

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)
	m.session.Rename("Auth Debugging")
	m.session.AddMessage("user", "why does login fail")
	m.session.AddMessage("assistant", "the token expired mid-flight")
	if err := m.session.Save(); err != nil {
		t.Fatal(err)
	}
	return m
}

func transcriptOf(m model) string {
	var b strings.Builder
	for _, e := range m.entries {
		b.WriteString(e.Content + "\n")
	}
	return b.String()
}

// TestExportDispatchWritesMarkdown covers the core contract: explicit
// path → file exists with ordered conversation content; confirmation
// appears only after a successful write.
func TestExportDispatchWritesMarkdown(t *testing.T) {
	m := newExportTestModel(t)

	isCommand, err := command.Dispatch(&m, m.registry, "/export notes chat.md")
	if err != nil {
		t.Fatalf("/export dispatch: %v", err)
	}
	if !isCommand {
		t.Fatal("/export not recognized as a command")
	}

	raw, err := os.ReadFile("notes chat.md")
	if err != nil {
		t.Fatalf("exported file missing: %v", err)
	}
	doc := string(raw)

	for _, want := range []string{
		"# Lato Session",
		"**Title:** Auth Debugging",
		"## User",
		"why does login fail",
		"## Assistant",
		"the token expired mid-flight",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("export missing %q:\n%s", want, doc)
		}
	}
	u := strings.Index(doc, "## User")
	a := strings.Index(doc, "## Assistant")
	if !(u < a) {
		t.Errorf("conversation order wrong:\n%s", doc)
	}

	// Confirmation only after success, naming the real destination.
	out := transcriptOf(m)
	if !strings.Contains(out, "Conversation exported to notes chat.md") {
		t.Errorf("confirmation missing:\n%s", out)
	}

	// Session identity untouched by exporting.
	if m.session.Title != "Auth Debugging" || m.session.ID == "" || len(m.session.Messages) != 2 {
		t.Errorf("session mutated by export: %+v", m.session)
	}
}

// TestExportOverwriteRefused: second export to the same path fails, and
// the original file is byte-identical afterwards.
func TestExportOverwriteRefused(t *testing.T) {
	m := newExportTestModel(t)

	if _, err := command.Dispatch(&m, m.registry, "/export out.md"); err != nil {
		t.Fatalf("first export: %v", err)
	}
	before, err := os.ReadFile("out.md")
	if err != nil {
		t.Fatal(err)
	}

	m.session.AddMessage("user", "one more turn")
	_, err = command.Dispatch(&m, m.registry, "/export out.md")
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("second export error = %v, want overwrite refusal", err)
	}

	after, err := os.ReadFile("out.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("existing export was modified")
	}
	if strings.Contains(transcriptOf(m), "Conversation exported to out.md\n✓") &&
		strings.Count(transcriptOf(m), "Conversation exported") != 1 {
		t.Errorf("false success reported:\n%s", transcriptOf(m))
	}
}

// TestExportMissingDirectoryRefused: no surprise directory trees.
func TestExportMissingDirectoryRefused(t *testing.T) {
	m := newExportTestModel(t)

	_, err := command.Dispatch(&m, m.registry, "/export no-such-dir/out.md")
	if err == nil || !strings.Contains(err.Error(), "destination directory does not exist") {
		t.Fatalf("error = %v, want missing-directory refusal", err)
	}
	if _, statErr := os.Stat("no-such-dir"); !os.IsNotExist(statErr) {
		t.Error("directory was created anyway")
	}
}

// TestExportDefaultFilename: bare /export uses the sanitized-title
// default and lands in the current directory.
func TestExportDefaultFilename(t *testing.T) {
	m := newExportTestModel(t)

	if _, err := command.Dispatch(&m, m.registry, "/export"); err != nil {
		t.Fatalf("default /export: %v", err)
	}
	want := m.session.DefaultExportFilename()
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("default-named export missing (%s): %v", want, err)
	}
	if !strings.Contains(string(raw), "Auth Debugging") {
		t.Errorf("default export content wrong:\n%s", raw)
	}
}

// TestExportEmptyConversationRefused pins the chosen convention (mirrors
// /copy): an empty conversation has nothing to export.
func TestExportEmptyConversationRefused(t *testing.T) {
	rt := newPickerTestRuntime(t)
	t.Chdir(t.TempDir())

	m := model{runtime: rt, registry: newRegistry(), entries: []chatEntry{}, session: session.New()}
	m.palette = newSlashPalette(m.registry)

	_, err := command.Dispatch(&m, m.registry, "/export")
	if err == nil || !strings.Contains(err.Error(), "nothing to export") {
		t.Fatalf("error = %v, want empty-conversation refusal", err)
	}
	entries, _ := filepath.Glob("lato-session-*.md")
	if len(entries) != 0 {
		t.Errorf("files written despite refusal: %v", entries)
	}
}

// TestExportNoCredentialsInDocument: even though the test runtime
// carries configuration, only conversation text may reach the file.
func TestExportNoCredentialsInDocument(t *testing.T) {
	m := newExportTestModel(t)

	if _, err := command.Dispatch(&m, m.registry, "/export cred-check.md"); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := os.ReadFile("cred-check.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"API_KEY", "api_key", "Authorization", "sk-", "endpoint"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("export contains credential-like marker %q:\n%s", forbidden, raw)
		}
	}
}

// TestOtherSessionsUnaffectedByExport: exporting touches exactly one
// file — the sibling session's store is unchanged.
func TestOtherSessionsUnaffectedByExport(t *testing.T) {
	m := newExportTestModel(t)
	other := session.New()
	other.AddMessage("user", "other conversation")
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	otherBefore, err := os.ReadFile(filepath.Join(".lato", "sessions", other.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := command.Dispatch(&m, m.registry, "/export x.md"); err != nil {
		t.Fatalf("export: %v", err)
	}

	otherAfter, err := os.ReadFile(filepath.Join(".lato", "sessions", other.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(otherBefore) != string(otherAfter) {
		t.Error("another session's file changed during export")
	}
}

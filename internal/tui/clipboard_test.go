package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleTranscript() []chatEntry {
	return []chatEntry{
		{Role: roleUser, Content: "why does the parser fail?"},
		{Role: roleActivity, Content: "✓ read_repo_file completed"},
		{Role: roleAssistant, Content: "The tokenizer rejects the EOF case."},
		{Role: roleError, Content: "tool timed out"},
		{Role: roleAssistant, Content: "## Result\n\nIt returns 42."},
	}
}

func TestLatestResponsePicksTrailingBlock(t *testing.T) {
	m := &model{entries: sampleTranscript()}
	got := m.LatestResponse()

	if got != "## Result\n\nIt returns 42." {
		t.Errorf("LatestResponse() = %q", got)
	}
	if strings.Contains(got, "tokenizer") || strings.Contains(got, "parser") {
		t.Errorf("copied more than the latest response block:\n%s", got)
	}
}

// TestLatestResponseIncludesToolOutput pins requirement 6: activity
// lines that belong to the trailing turn are copied alongside it, in
// chronological order.
func TestLatestResponseIncludesToolOutput(t *testing.T) {
	entries := []chatEntry{
		{Role: roleUser, Content: "run the tests"},
		{Role: roleActivity, Content: "✓ read_repo_file completed"},
		{Role: roleActivity, Content: "✓ run_command completed"},
		{Role: roleAssistant, Content: "## Result\n\nAll tests pass."},
	}
	m := &model{entries: entries}
	got := m.LatestResponse()

	if !strings.Contains(got, "✓ run_command completed") {
		t.Errorf("tool output missing from copy:\n%s", got)
	}
	if !strings.Contains(got, "## Result") {
		t.Errorf("response missing from copy:\n%s", got)
	}
	if strings.Index(got, "run_command") > strings.Index(got, "Result") {
		t.Errorf("copy is out of order:\n%s", got)
	}
}

// TestLatestResponseSurvivesSystemConfirmation keeps repeat /copy
// working after a previous copy appended its confirmation entry.
func TestLatestResponseSurvivesSystemConfirmation(t *testing.T) {
	entries := append(sampleTranscript(),
		chatEntry{Role: roleSystem, Content: "✓ Copied the latest response (12 characters) to the clipboard."},
	)
	m := &model{entries: entries}
	if got := m.LatestResponse(); !strings.Contains(got, "It returns 42.") {
		t.Errorf("system tail broke extraction: %q", got)
	}
}

func TestLatestResponseEmptyWithoutResponses(t *testing.T) {
	m := &model{entries: []chatEntry{
		{Role: roleUser, Content: "hi"},
		{Role: roleSystem, Content: "welcome"},
	}}
	if got := m.LatestResponse(); got != "" {
		t.Errorf("LatestResponse() = %q, want empty", got)
	}
}

func TestTranscriptTextLabelsChronologically(t *testing.T) {
	m := &model{entries: []chatEntry{
		{Role: roleUser, Content: "question one"},
		{Role: roleAssistant, Content: "answer **one**"},
	}}
	got := m.TranscriptText()
	want := "You:\nquestion one\n\nLato:\nanswer **one**"
	if got != want {
		t.Errorf("TranscriptText() =\n%q\nwant\n%q", got, want)
	}
}

func TestStripANSIRemovesStylingAndOSC(t *testing.T) {
	in := "\x1b[31mred\x1b[0m plain \x1b]0;title\x07tail"
	if got := stripANSI(in); got != "red plain tail" {
		t.Errorf("stripANSI() = %q", got)
	}
}

// TestCopiedTextContainsNoANSI guarantees plain-text copies even if an
// entry ever contained escape sequences.
func TestCopiedTextContainsNoANSI(t *testing.T) {
	m := &model{entries: []chatEntry{
		{Role: roleUser, Content: "q"},
		{Role: roleAssistant, Content: "styled \x1b[1;32mbold-green\x1b[0m end"},
	}}
	if got := m.LatestResponse(); strings.ContainsRune(got, '\x1b') {
		t.Errorf("ANSI escape reached the copy path: %q", got)
	}
}

// --- keyboard shortcut ---------------------------------------------------

type recordingClipboard struct {
	text string
	err  error
}

func TestAltCCopiesLatestResponse(t *testing.T) {
	rec := &recordingClipboard{text: "", err: nil}
	old := writeClipboard
	writeClipboard = func(s string) error { rec.text = s; return rec.err }
	defer func() { writeClipboard = old }()

	m := model{entries: []chatEntry{
		{Role: roleUser, Content: "q"},
		{Role: roleAssistant, Content: "copy me please"},
	}}

	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}, Alt: true})
	gm := got.(model)

	if rec.text != "copy me please" {
		t.Errorf("clipboard received %q, want the latest response", rec.text)
	}
	found := false
	for _, e := range gm.entries {
		if e.Role == roleSystem && strings.Contains(e.Content, "✓ Copied") {
			found = true
		}
	}
	if !found {
		t.Error("no copy confirmation appended to the transcript")
	}
}

func TestShortcutFailureShowsErrorNotCrash(t *testing.T) {
	old := writeClipboard
	writeClipboard = func(string) error { return errTestNoClipboard }
	defer func() { writeClipboard = old }()

	m := model{entries: []chatEntry{{Role: roleAssistant, Content: "keep me"}}}
	got, _ := m.handleKey(keyMsgAltC())
	gm := got.(model)

	if len(gm.entries) != 2 || gm.entries[1].Role != roleError {
		t.Fatalf("entries after failure = %+v", gm.entries)
	}
	if !strings.Contains(gm.entries[0].Content, "keep me") {
		t.Error("the response must remain visible when copying fails")
	}
}

func TestShortcutEmptyTranscriptHints(t *testing.T) {
	old := writeClipboard
	called := false
	writeClipboard = func(string) error { called = true; return nil }
	defer func() { writeClipboard = old }()

	m := model{}
	got, _ := m.handleKey(keyMsgAltC())
	gm := got.(model)

	if called {
		t.Error("clipboard written although there was nothing to copy")
	}
	if len(gm.entries) != 1 || !strings.Contains(gm.entries[0].Content, "Nothing to copy") {
		t.Errorf("hint missing: %+v", gm.entries)
	}
}

func TestCtrlCStillQuits(t *testing.T) {
	m := model{}
	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !(got.(model)).quitting {
		t.Error("ctrl+c no longer quits; quit behavior regressed")
	}
}

func keyMsgAltC() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}, Alt: true}
}

var errTestNoClipboard = errors.New("no clipboard tool available")

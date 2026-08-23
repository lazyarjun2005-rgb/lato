package builtin

import (
	"errors"
	"strings"
	"testing"
)

// TestExportForwardsPathAndConfirms: explicit single and multi-word
// paths are joined per parser semantics and forwarded verbatim; the
// confirmation names the path actually written.
func TestExportForwardsPathAndConfirms(t *testing.T) {
	fc := &fakeContext{}
	if err := NewExport().Execute(fc, []string{"notes", "chat.md"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if fc.exportedPath != "notes chat.md" {
		t.Errorf("forwarded path = %q", fc.exportedPath)
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "notes chat.md") || !strings.Contains(out, "exported") {
		t.Errorf("confirmation missing:\n%s", out)
	}

	// Default invocation forwards an empty path.
	fc = &fakeContext{}
	if err := NewExport().Execute(fc, nil); err != nil {
		t.Fatalf("default Execute() error = %v", err)
	}
	if fc.exportedPath != "" {
		t.Errorf("default path = %q, want empty (Context chooses default)", fc.exportedPath)
	}
	out = strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "lato-session-exported.md") {
		t.Errorf("confirmation must name the written path:\n%s", out)
	}
}

// TestExportPropagatesErrors: any Context failure (overwrite refusal,
// bad directory, empty conversation) reaches the caller with no
// success output.
func TestExportPropagatesErrors(t *testing.T) {
	want := errors.New("refusing to overwrite existing destination x.md")
	fc := &fakeContext{exportErr: want}
	err := NewExport().Execute(fc, []string{"x.md"})
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if len(fc.lines) != 0 {
		t.Errorf("success lines printed despite failure: %v", fc.lines)
	}
}

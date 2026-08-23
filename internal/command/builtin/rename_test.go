package builtin

import (
	"errors"
	"strings"
	"testing"

	"lato/internal/session"
)

// TestRenameJoinsMultiWordTitle pins the core requirement: every
// argument word becomes part of one exact title, in order.
func TestRenameJoinsMultiWordTitle(t *testing.T) {
	fc := &fakeContext{}
	if err := NewRename().Execute(fc, []string{"My", "Authentication", "Debugging"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if fc.renamedTitle != "My Authentication Debugging" {
		t.Errorf("renamed to %q, want %q", fc.renamedTitle, "My Authentication Debugging")
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, `Session renamed to "My Authentication Debugging"`) {
		t.Errorf("confirmation missing:\n%s", out)
	}
}

// TestRenameRequiresATitle: bare /rename is a usage error and must not
// touch the session.
func TestRenameRequiresATitle(t *testing.T) {
	fc := &fakeContext{}
	err := NewRename().Execute(fc, nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("error = %v, want usage error", err)
	}
	if fc.renamedTitle != "" {
		t.Errorf("session was renamed anyway: %q", fc.renamedTitle)
	}
}

// TestRenameRejectsWhitespaceOnlyTitle covers the case a future caller
// (or Context) could expose even though the parser collapses it.
func TestRenameRejectsWhitespaceOnlyTitle(t *testing.T) {
	fc := &fakeContext{}
	err := NewRename().Execute(fc, []string{"   "})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("error = %v, want usage error", err)
	}
	if fc.renamedTitle != "" {
		t.Errorf("session was renamed anyway: %q", fc.renamedTitle)
	}
}

// TestRenamePropagatesErrors: persistence failures reach the command
// unchanged so dispatch can render them.
func TestRenamePropagatesErrors(t *testing.T) {
	fc := &fakeContext{renameErr: errors.New("disk full")}
	err := NewRename().Execute(fc, []string{"new", "name"})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v, want RenameSession failure", err)
	}
	if fc.renamedTitle != "" {
		t.Errorf("failed rename recorded a title: %q", fc.renamedTitle)
	}
}

// TestSessionTitlePrefersUserTitle covers picker display for both
// shapes: titled sessions show their title; legacy/untitled sessions
// keep the derived first-user-message preview.
func TestSessionTitlePrefersUserTitle(t *testing.T) {
	titled := session.Session{Title: "My Authentication Debugging"}
	if got := SessionTitle(titled); got != "My Authentication Debugging" {
		t.Errorf("titled display = %q", got)
	}

	legacy := session.Session{Messages: []session.Message{
		{Role: "user", Content: "why does the build fail on windows"},
	}}
	got := SessionTitle(legacy)
	if !strings.HasPrefix(got, `"`) || !strings.Contains(got, "build fail") {
		t.Errorf("legacy display = %q, want quoted first-message preview", got)
	}

	empty := session.Session{}
	if got := SessionTitle(empty); got != "(empty session)" {
		t.Errorf("empty display = %q", got)
	}
}

package builtin

import (
	"errors"
	"strings"
	"testing"

	"lato/internal/session"
)

// TestResumeBareOpensPicker: bare /resume hands the sorted session list
// to the existing picker instead of guessing.
func TestResumeBareOpensPicker(t *testing.T) {
	t.Chdir(t.TempDir()) // session listing is CWD-relative
	for _, title := range []string{"newer", "older"} {
		s := session.New()
		s.Title = title
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}

	fc := &fakeContext{}
	if err := NewResume().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(fc.pickedSessions) != 2 {
		t.Fatalf("picker sessions = %d, want 2", len(fc.pickedSessions))
	}
	for i := 1; i < len(fc.pickedSessions); i++ {
		if fc.pickedSessions[i-1].UpdatedAt.Before(fc.pickedSessions[i].UpdatedAt) {
			t.Error("sessions not sorted newest-first")
		}
	}
}

// TestResumeForwardsJoinedTarget: multi-word titles survive as one
// string; single IDs pass through unchanged.
func TestResumeForwardsJoinedTarget(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"8f31c2a1"}, "8f31c2a1"},
		{[]string{"Demo", "Authentication", "Project"}, "Demo Authentication Project"},
	}
	for _, tc := range cases {
		fc := &fakeContext{}
		if err := NewResume().Execute(fc, tc.args); err != nil {
			t.Fatalf("Execute(%v) error = %v", tc.args, err)
		}
		if fc.resumedSession != tc.want {
			t.Errorf("forwarded %q, want %q", fc.resumedSession, tc.want)
		}
	}
}

// TestResumePropagatesErrors: not-found / ambiguity / list failures
// surface unchanged, with no picker opened and no success output.
func TestResumePropagatesErrors(t *testing.T) {
	want := errors.New(`session not found: "nope" — run /sessions to pick one`)
	fc := &fakeContext{resumeSessionErr: want}

	err := NewResume().Execute(fc, []string{"nope"})
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if len(fc.pickedSessions) != 0 || len(fc.lines) != 0 {
		t.Errorf("side effects on failure: picked=%v lines=%v", fc.pickedSessions, fc.lines)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error shape: %v", err)
	}
}

package builtin

import (
	"errors"
	"strings"
	"testing"
)

// TestRewindMetadataAndRegistration covers registry-facing fields.
func TestRewindMetadata(t *testing.T) {
	r := NewRewind()
	if r.Name() != "rewind" || r.Usage() != "/rewind [N]" {
		t.Errorf("metadata = %q / %q", r.Name(), r.Usage())
	}
	if strings.TrimSpace(r.Description()) == "" {
		t.Error("empty description")
	}
}

// TestRewindDefaultAndExplicitCounts: bare /rewind means 1; a single
// positive integer is forwarded verbatim; the confirmation reflects the
// count with correct pluralization.
func TestRewindDefaultAndExplicitCounts(t *testing.T) {
	fc := &fakeContext{}
	if err := NewRewind().Execute(fc, nil); err != nil {
		t.Fatalf("default Execute() error = %v", err)
	}
	if fc.rewindCalls != 1 || fc.lastRewindTurns != 1 {
		t.Fatalf("default: calls=%d turns=%d, want 1/1", fc.rewindCalls, fc.lastRewindTurns)
	}
	if !strings.Contains(strings.Join(fc.lines, "\n"), "Rewound 1 conversation turn.") {
		t.Errorf("singular confirmation missing:\n%s", strings.Join(fc.lines, "\n"))
	}

	fc = &fakeContext{}
	if err := NewRewind().Execute(fc, []string{"10"}); err != nil {
		t.Fatalf("Execute(10) error = %v", err)
	}
	if fc.lastRewindTurns != 10 {
		t.Errorf("forwarded turns = %d, want 10", fc.lastRewindTurns)
	}
	if !strings.Contains(strings.Join(fc.lines, "\n"), "Rewound 10 conversation turns.") {
		t.Errorf("plural confirmation missing:\n%s", strings.Join(fc.lines, "\n"))
	}
}

// TestRewindInvalidArguments: every invalid shape is a usage error with
// zero Context calls.
func TestRewindInvalidArguments(t *testing.T) {
	cases := [][]string{
		{"0"}, {"-1"}, {"abc"}, {"1", "2"}, {}, // {} covered below separately
	}
	for _, args := range cases[:4] {
		fc := &fakeContext{}
		err := NewRewind().Execute(fc, args)
		if err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("args %v: error = %v, want usage error", args, err)
		}
		if fc.rewindCalls != 0 {
			t.Errorf("args %v: RewindConversation called %d times", args, fc.rewindCalls)
		}
	}
}

// TestRewindPropagatesErrors: busy/save-fail/over-count refusals from
// the Context surface unchanged with no success output and no AI
// submission of any kind.
func TestRewindPropagatesErrors(t *testing.T) {
	want := errors.New("cannot rewind conversation while Lato is busy")
	fc := &fakeContext{rewindErr: want}

	err := NewRewind().Execute(fc, []string{"1"})
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if len(fc.lines) != 0 || len(fc.submitted) != 0 {
		t.Errorf("side effects on failure: lines=%v submitted=%v", fc.lines, fc.submitted)
	}
}

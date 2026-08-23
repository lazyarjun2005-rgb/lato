package builtin

import (
	"errors"
	"strings"
	"testing"
)

// TestBranchMetadata covers registry-facing fields.
func TestBranchMetadata(t *testing.T) {
	b := NewBranch()
	if b.Name() != "branch" || b.Usage() != "/branch [title]" {
		t.Errorf("metadata = %q / %q", b.Name(), b.Usage())
	}
	if strings.TrimSpace(b.Description()) == "" {
		t.Error("empty description")
	}
}

// TestBranchForwardsTitle: bare /branch forwards an empty title (the
// Context derives the default); multi-word explicit titles survive the
// parser exactly as with /rename.
func TestBranchForwardsTitle(t *testing.T) {
	fc := &fakeContext{}
	if err := NewBranch().Execute(fc, nil); err != nil {
		t.Fatalf("bare Execute() error = %v", err)
	}
	if fc.branchTitle != "" {
		t.Errorf("bare title = %q, want empty", fc.branchTitle)
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "8f31c2a1") || !strings.Contains(out, "Branched") {
		t.Errorf("confirmation missing:\n%s", out)
	}

	fc = &fakeContext{}
	if err := NewBranch().Execute(fc, []string{"My", "OAuth", "Direction"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if fc.branchTitle != "My OAuth Direction" {
		t.Errorf("forwarded title = %q", fc.branchTitle)
	}
}

// TestBranchPropagatesErrors: busy/save failures surface unchanged with
// no success output and no model involvement of any kind.
func TestBranchPropagatesErrors(t *testing.T) {
	want := errors.New("cannot branch while Lato is busy")
	fc := &fakeContext{branchErr: want}

	err := NewBranch().Execute(fc, []string{"somewhere"})
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if len(fc.lines) != 0 || len(fc.submitted) != 0 {
		t.Errorf("side effects on failure: lines=%v submitted=%v", fc.lines, fc.submitted)
	}
}

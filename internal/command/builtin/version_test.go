package builtin

import (
	"strings"
	"testing"

	"lato/internal/version"
)

func TestVersionReportsBuildInfo(t *testing.T) {
	fc := &fakeContext{}
	if err := NewVersion().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, version.Version) {
		t.Errorf("version output missing %q:\n%s", version.Version, out)
	}
	if !strings.Contains(out, "Lato") {
		t.Errorf("version output missing product name:\n%s", out)
	}
}

func TestVersionRejectsArguments(t *testing.T) {
	fc := &fakeContext{}
	err := NewVersion().Execute(fc, []string{"extra"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
	if len(fc.lines) != 0 {
		t.Errorf("nothing should be printed on a usage error: %v", fc.lines)
	}
}

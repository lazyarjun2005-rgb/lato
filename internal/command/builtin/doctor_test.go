package builtin

import (
	"strings"
	"testing"

	"lato/internal/workspace"
)

func TestDoctorRendersEnvironmentReport(t *testing.T) {
	fc := &fakeContext{
		workspace: workspace.Info{Root: "/home/user/demo", CWD: "/home/user/demo"},
	}

	if err := NewDoctor().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")

	// The report must be the shared internal/doctor text, not a
	// hand-rolled variant: check its stable landmarks.
	for _, want := range []string{
		"Lato environment check",
		"Executable",
		"Config",
		"Providers",
		"Memory",
		"Tasks",
		"Workspace    /home/user/demo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	fc := &fakeContext{}
	err := NewDoctor().Execute(fc, []string{"fix"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

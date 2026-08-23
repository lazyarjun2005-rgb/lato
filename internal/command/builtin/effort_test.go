package builtin

import (
	"strings"
	"testing"

	"lato/internal/command"
)

func TestEffortNoArgShowsCurrentAndLadder(t *testing.T) {
	fc := &fakeContext{effort: "high"}
	if err := NewEffort().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")
	for _, want := range []string{"Current effort: high", "low", "medium", "ultra", "lato-X", "› high"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestEffortSwitchPersistsByDefault(t *testing.T) {
	fc := &fakeContext{}
	if err := NewEffort().Execute(fc, []string{"lato-x"}); err != nil {
		t.Fatalf("Execute(lato-x) error = %v", err)
	}
	if fc.effort != "lato-X" {
		t.Errorf("effort = %q, want lato-X", fc.effort)
	}
	if fc.sessionOnly {
		t.Error("/effort <level> must persist by default")
	}
	if !strings.Contains(strings.Join(fc.lines, " "), "✓ Effort: lato-X") {
		t.Errorf("confirmation missing: %v", fc.lines)
	}
}

func TestEffortInvalidNamesRefused(t *testing.T) {
	fc := &fakeContext{}
	err := NewEffort().Execute(fc, []string{"turbo"})
	if err == nil || !strings.Contains(err.Error(), "valid:") {
		t.Fatalf("Execute(turbo) = %v, want refusal naming valid values", err)
	}
	if fc.effort != "" {
		t.Errorf("failed switch changed effort to %q", fc.effort)
	}

	if err := NewEffort().Execute(fc, []string{"a", "b"}); err == nil {
		t.Error("multiple args accepted")
	}
}

// TestEffortRegisteredInPaletteSource pins that /effort is part of the
// registry every discovery surface (help + slash palette) derives from.
func TestEffortRegisteredInPaletteSource(t *testing.T) {
	reg := command.NewRegistry()
	reg.Register(NewEffort())
	cmd, ok := reg.Lookup("effort")
	if !ok || cmd.Name() != "effort" {
		t.Fatal("/effort not resolvable through the registry")
	}
}

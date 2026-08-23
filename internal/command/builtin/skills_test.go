package builtin

import (
	"strings"
	"testing"
)

func TestSkillsListsCatalog(t *testing.T) {
	fc := &fakeContext{
		skillsSummary: "architecture-review — Architecture Review: review the design\nrelease-checklist — Release Checklist",
	}

	if err := NewSkills().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")

	if !strings.Contains(out, "architecture-review") || !strings.Contains(out, "release-checklist") {
		t.Errorf("skill ids missing from output:\n%s", out)
	}
	if !strings.HasPrefix(out, "Skills:") {
		t.Errorf("expected a Skills header:\n%s", out)
	}
}

func TestSkillsEmptyCatalogMessage(t *testing.T) {
	fc := &fakeContext{}
	if err := NewSkills().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "No skills found") {
		t.Errorf("empty catalog message missing:\n%s", out)
	}
}

func TestSkillsRejectsArguments(t *testing.T) {
	fc := &fakeContext{}
	err := NewSkills().Execute(fc, []string{"load", "x"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

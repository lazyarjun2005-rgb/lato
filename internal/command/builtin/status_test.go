package builtin

import (
	"strings"
	"testing"

	"lato/internal/workspace"
)

func TestStatusRendersSessionAndProject(t *testing.T) {
	fc := &fakeContext{
		model:    "qwen3:8b",
		provider: "ollama",
		effort:   "high",
		workspace: workspace.Info{
			Repository: "demo",
			Root:       "/home/user/demo",
			IsGitRepo:  true,
			Branch:     "main",
			Language:   "Go",
			Framework:  "chi",
		},
	}

	if err := NewStatus().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")

	for _, want := range []string{
		"Model: qwen3:8b", "Provider: ollama", "Effort: high",
		"Name: demo", "Root: /home/user/demo", "Branch: main",
		"Stack: Go, chi",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
}

func TestStatusDetachedBranchAndNonRepo(t *testing.T) {
	fc := &fakeContext{
		model:    "m",
		provider: "p",
		workspace: workspace.Info{
			Repository: "plain",
			Root:       "/tmp/plain",
		},
	}
	if err := NewStatus().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "not a repository") {
		t.Errorf("non-git workspace not reported:\n%s", out)
	}
	if strings.Contains(out, "Branch:") || strings.Contains(out, "Stack:") {
		t.Errorf("empty branch/stack must be omitted:\n%s", out)
	}
}

func TestStatusRejectsArguments(t *testing.T) {
	fc := &fakeContext{}
	err := NewStatus().Execute(fc, []string{"verbose"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

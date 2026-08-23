package tui

import (
	"strings"
	"testing"

	"lato/internal/task"
)

// TestResumeAnnouncement pins the M15 resume UX block: it names the
// task, states saved progress, and shows last/next actions from the
// persisted record only.
func TestResumeAnnouncement(t *testing.T) {
	var tk task.Task
	tk.Goal = "Add authentication to this project."
	tk.SetPlanFromText("1. Inspect auth code\n2. Implement login handler\n3. Run tests")
	tk.MarkStepComplete("Inspect")
	tk.MarkStepComplete("Implement")
	tk.NoteAction("edit_file: internal/auth/login.go")
	tk.NextAction = "Run tests"

	got := resumeAnnouncement(tk)
	for _, want := range []string{
		"Resuming task:",
		"Add authentication to this project.",
		"Previous state:",
		"2/3 steps completed",
		"Last action:",
		"edit_file: internal/auth/login.go",
		"Next:",
		"Run tests",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("announcement missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Error("announcement contains ANSI escapes")
	}
}

// TestResumeAnnouncementMinimal verifies a task without a plan still
// renders a sensible announcement with no invented fields.
func TestResumeAnnouncementMinimal(t *testing.T) {
	tk := task.Task{Goal: "Tidy the makefile", Status: task.StatusPaused}
	tk.NoteAction("read_file: Makefile")

	got := resumeAnnouncement(tk)
	for _, want := range []string{"Resuming task:", "Tidy the makefile", "Last action:", "read_file: Makefile"} {
		if !strings.Contains(got, want) {
			t.Errorf("announcement missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Previous state:") || strings.Contains(got, "Next:") {
		t.Errorf("announcement invented state that was never recorded:\n%s", got)
	}
}

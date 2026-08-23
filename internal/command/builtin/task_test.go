package builtin

import (
	"errors"
	"strings"
	"testing"
)

func TestTaskListEmpty(t *testing.T) {
	ctx := &fakeContext{taskList: ""}
	if err := NewTask().Execute(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(ctx.lines) != 1 || !strings.Contains(ctx.lines[0], "No tasks recorded") {
		t.Errorf("output = %v", ctx.lines)
	}
}

func TestTaskListWithTasks(t *testing.T) {
	ctx := &fakeContext{taskList: "Tasks:\nab12c3 — Fix auth (paused) · 2/3"}
	if err := NewTask().Execute(ctx, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx.lines[0], "Fix auth") {
		t.Errorf("output = %v", ctx.lines)
	}
}

func TestTaskResumeWithID(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewTask().Execute(ctx, []string{"resume", "ab12"}); err != nil {
		t.Fatal(err)
	}
	if ctx.resumedTask != "ab12" {
		t.Errorf("resumed id = %q", ctx.resumedTask)
	}
}

func TestTaskResumeBare(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewTask().Execute(ctx, []string{"resume"}); err != nil {
		t.Fatal(err)
	}
	if ctx.resumedTask != "" {
		t.Errorf("bare resume should pass empty id, got %q", ctx.resumedTask)
	}
}

func TestTaskResumeErrorPropagates(t *testing.T) {
	ctx := &fakeContext{resumeErr: errors.New("no resumable task for this project")}
	err := NewTask().Execute(ctx, []string{"resume"})
	if err == nil || !strings.Contains(err.Error(), "no resumable task") {
		t.Fatalf("error = %v", err)
	}
}

func TestTaskAbandonRequiresID(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewTask().Execute(ctx, []string{"abandon"}); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestTaskAbandonOK(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewTask().Execute(ctx, []string{"abandon", "ab12"}); err != nil {
		t.Fatal(err)
	}
	if len(ctx.lines) == 0 || !strings.Contains(ctx.lines[0], "abandoned") {
		t.Errorf("confirmation missing: %v", ctx.lines)
	}
}

func TestTaskUnknownSubcommand(t *testing.T) {
	ctx := &fakeContext{}
	err := NewTask().Execute(ctx, []string{"teleport"})
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("error = %v", err)
	}
}

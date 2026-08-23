package builtin

import (
	"strings"
	"testing"
)

func TestPermissionsShowsPolicySummary(t *testing.T) {
	ctx := &fakeContext{permissionsSummary: "Permission policy\nWorkspace: /tmp/project\nPending approval: none"}
	if err := NewPermissions().Execute(ctx, nil); err != nil {
		t.Fatalf("Execute returned an error: %v", err)
	}
	if len(ctx.lines) != 1 || !strings.Contains(ctx.lines[0], "Workspace: /tmp/project") {
		t.Fatalf("summary not printed: %v", ctx.lines)
	}
}

func TestPermissionsResetReportsClearedCount(t *testing.T) {
	ctx := &fakeContext{resetCount: 2}
	if err := NewPermissions().Execute(ctx, []string{"reset"}); err != nil {
		t.Fatalf("Execute returned an error: %v", err)
	}
	if len(ctx.lines) != 1 || !strings.Contains(ctx.lines[0], "2") {
		t.Fatalf("reset report wrong: %v", ctx.lines)
	}
}

func TestPermissionsResetWithNoneActive(t *testing.T) {
	ctx := &fakeContext{resetCount: 0}
	if err := NewPermissions().Execute(ctx, []string{"reset"}); err != nil {
		t.Fatalf("Execute returned an error: %v", err)
	}
	if len(ctx.lines) != 1 || !strings.Contains(ctx.lines[0], "No temporary approvals") {
		t.Fatalf("no-approvals report wrong: %v", ctx.lines)
	}
}

func TestPermissionsRejectsUnknownSubcommand(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewPermissions().Execute(ctx, []string{"grant-all"}); err == nil {
		t.Fatal("expected an error for unknown subcommand")
	}
	if ctx.resetCalled() {
		t.Fatal("unknown subcommand must not reset anything")
	}
}

func TestPermissionsRejectsExtraArgs(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewPermissions().Execute(ctx, []string{"reset", "now"}); err == nil {
		t.Fatal("expected a usage error")
	}
}

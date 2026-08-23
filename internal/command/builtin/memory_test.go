package builtin

import (
	"errors"
	"strings"
	"testing"
)

func TestMemoryListEmpty(t *testing.T) {
	ctx := &fakeContext{memorySummary: ""}
	if err := NewMemory().Execute(ctx, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(ctx.lines) != 1 || !strings.Contains(ctx.lines[0], "No project memories") {
		t.Errorf("empty listing output = %v", ctx.lines)
	}
}

func TestMemoryListWithEntries(t *testing.T) {
	ctx := &fakeContext{memorySummary: "ab12 [user/command] Tests run with go test"}
	if err := NewMemory().Execute(ctx, []string{"list"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(ctx.lines) == 0 || !strings.Contains(ctx.lines[0], "go test") {
		t.Errorf("listing output = %v", ctx.lines)
	}
}

func TestMemoryAdd(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewMemory().Execute(ctx, []string{"add", "The", "project", "uses", "chi"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(ctx.lines) == 0 || !strings.Contains(ctx.lines[0], "Remembered") {
		t.Errorf("confirmation missing: %v", ctx.lines)
	}
}

func TestMemoryAddRequiresText(t *testing.T) {
	ctx := &fakeContext{}
	err := NewMemory().Execute(ctx, []string{"add"})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("error = %v, want usage guidance", err)
	}
}

func TestMemoryRemoveByID(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewMemory().Execute(ctx, []string{"remove", "ab12"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(ctx.lines) == 0 || !strings.Contains(ctx.lines[0], "ab12") {
		t.Errorf("confirmation missing: %v", ctx.lines)
	}
}

func TestMemoryRemoveRequiresID(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewMemory().Execute(ctx, []string{"remove"}); err == nil {
		t.Fatal("expected usage error when ID missing")
	}
}

func TestMemoryClear(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewMemory().Execute(ctx, []string{"clear"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(ctx.lines) == 0 || !strings.Contains(ctx.lines[0], "cleared") {
		t.Errorf("confirmation missing: %v", ctx.lines)
	}
}

func TestMemoryUnknownSubcommand(t *testing.T) {
	ctx := &fakeContext{}
	err := NewMemory().Execute(ctx, []string{"teleport"})
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("error = %v", err)
	}
}

func TestMemoryErrorsPropagateCleanly(t *testing.T) {
	ctx := &fakeContext{memoryErr: errors.New("store is full")}
	err := NewMemory().Execute(ctx, []string{"add", "fact"})
	if err == nil || !strings.Contains(err.Error(), "remember failed") {
		t.Fatalf("error = %v, want wrapped remember failure", err)
	}
	if strings.Contains(err.Error(), "\n") && len(ctx.lines) != 0 {
		t.Error("failure should not print success output")
	}
}

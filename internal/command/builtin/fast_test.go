package builtin

import (
	"errors"
	"strings"
	"testing"

	"lato/internal/command"
)

// TestFastSwitchesToLowSessionOnly pins M3.1's exact contract: /fast is
// a session-only jump to LOW through the existing effort mechanism —
// nothing persisted, no model or provider involvement.
func TestFastSwitchesToLowSessionOnly(t *testing.T) {
	fc := &fakeContext{effort: "lato-X"}

	if err := NewFast().Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if fc.setEffortCalls != 1 {
		t.Fatalf("SetEffort calls = %d, want 1", fc.setEffortCalls)
	}
	if fc.lastEffortLevel != "low" {
		t.Errorf("effort level = %q, want low", fc.lastEffortLevel)
	}
	if fc.lastEffortPersist {
		t.Error("/fast must not persist: persist must be false")
	}
	if !fc.sessionOnly {
		t.Error("fake did not record session-only scope")
	}

	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "low") || !strings.Contains(out, "session only") {
		t.Errorf("confirmation missing from output:\n%s", out)
	}
}

// TestFastRejectsArguments keeps the command family consistent.
func TestFastRejectsArguments(t *testing.T) {
	fc := &fakeContext{}
	err := NewFast().Execute(fc, []string{"ultra"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
	if fc.setEffortCalls != 0 {
		t.Errorf("refused invocation still called SetEffort (%d times)", fc.setEffortCalls)
	}
}

// TestFastReportsEffortErrors proves SetEffort failures (e.g. a
// provider rebuild problem) surface instead of being swallowed.
func TestFastReportsEffortErrors(t *testing.T) {
	fc := &fakeContext{setEffortErr: errors.New("provider rebuild failed")}
	err := NewFast().Execute(fc, nil)
	if err == nil || !strings.Contains(err.Error(), "provider rebuild failed") {
		t.Fatalf("error = %v, want wrapped SetEffort failure", err)
	}
}

// TestFastMetadata checks registry-facing fields.
func TestFastMetadata(t *testing.T) {
	f := NewFast()
	if f.Name() != "fast" {
		t.Errorf("name = %q", f.Name())
	}
	if f.Usage() != "/fast" {
		t.Errorf("usage = %q", f.Usage())
	}
	if strings.TrimSpace(f.Description()) == "" {
		t.Error("empty description")
	}
}

// TestHelpListsFast pins discoverability: /help derives its listing
// from the registry, and /fast must land in the Agent setup section.
func TestHelpListsFast(t *testing.T) {
	reg := command.NewRegistry()
	reg.Register(NewFast())
	reg.Register(NewEffort())
	reg.Register(NewHelp(reg))

	fc := &fakeContext{}
	if err := NewHelp(reg).Execute(fc, nil); err != nil {
		t.Fatalf("help Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "/fast") {
		t.Errorf("/help missing /fast:\n%s", out)
	}
	if !strings.Contains(out, "Agent setup") {
		t.Errorf("/help missing Agent setup section:\n%s", out)
	}
}

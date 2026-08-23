package builtin

import (
	"strings"
	"testing"
)

// wantDevNames pins the milestone's command set in display order, so an
// accidental removal or rename fails loudly here first.
var wantDevNames = []string{
	"search", "explain", "debug", "fix", "test",
	"build", "run", "review", "refactor", "code",
}

func devByName(t *testing.T, name string) devCommand {
	t.Helper()
	for _, d := range devCommands {
		if d.name == name {
			return d
		}
	}
	t.Fatalf("dev command %q not found", name)
	return devCommand{}
}

// TestDevCommandSetComplete verifies shape and metadata of every entry:
// unique names, self-consistent usage lines, non-empty descriptions,
// and directives that stand alone (optional-arg commands submit them
// without any request line attached).
func TestDevCommandSetComplete(t *testing.T) {
	cmds := NewDevCommands()
	if len(cmds) != len(wantDevNames) {
		t.Fatalf("dev command count = %d, want %d", len(cmds), len(wantDevNames))
	}
	for i, cmd := range cmds {
		if cmd.Name() != wantDevNames[i] {
			t.Errorf("command %d = %q, want %q", i, cmd.Name(), wantDevNames[i])
		}
		if cmd.Usage() != "/"+cmd.Name() && !strings.HasPrefix(cmd.Usage(), "/"+cmd.Name()+" ") {
			t.Errorf("%s: usage %q must start with /%s", cmd.Name(), cmd.Usage(), cmd.Name())
		}
		if strings.TrimSpace(cmd.Description()) == "" {
			t.Errorf("%s: empty description", cmd.Name())
		}
		d := devByName(t, cmd.Name())
		if strings.TrimSpace(d.directive) == "" {
			t.Errorf("%s: empty directive", cmd.Name())
		}
	}
}

// TestDevRequiredArgumentValidation: required-argument commands refuse
// bare invocation without submitting anything.
func TestDevRequiredArgumentValidation(t *testing.T) {
	for _, name := range []string{"search", "explain", "debug", "fix", "refactor", "code"} {
		d := devByName(t, name)
		fc := &fakeContext{}
		err := d.Execute(fc, nil)
		if err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("/%s bare call: got %v, want usage error", name, err)
		}
		if len(fc.submitted) != 0 {
			t.Errorf("/%s bare call submitted a prompt: %q", name, fc.submitted)
		}
	}
}

// TestDevOptionalArgumentsWorkBare: optional-argument commands submit
// their directive alone, with no dangling request line.
func TestDevOptionalArgumentsWorkBare(t *testing.T) {
	for _, name := range []string{"test", "build", "run", "review"} {
		d := devByName(t, name)
		fc := &fakeContext{}
		if err := d.Execute(fc, nil); err != nil {
			t.Fatalf("/%s bare call: %v", name, err)
		}
		if len(fc.submitted) != 1 {
			t.Fatalf("/%s: submissions = %d, want 1", name, len(fc.submitted))
		}
		if fc.submitted[0] != d.directive {
			t.Errorf("/%s bare prompt diverges from directive:\n%s", name, fc.submitted[0])
		}
	}
}

// TestDevPromptPreservesArguments: multi-word arguments survive verbatim
// into the submitted prompt, appended under the Request marker.
func TestDevPromptPreservesArguments(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"explain", "internal/runtime/runtime.go", "Request: internal/runtime/runtime.go"},
		{"debug", "login fails after provider change", "Request: login fails after provider change"},
		{"fix", "failing tests in internal/runtime", "Request: failing tests in internal/runtime"},
		{"search", "permission handling", "Request: permission handling"},
	}
	for _, tc := range cases {
		d := devByName(t, tc.name)
		fc := &fakeContext{}
		if err := d.Execute(fc, strings.Fields(tc.args)); err != nil {
			t.Fatalf("/%s: %v", tc.name, err)
		}
		if len(fc.submitted) != 1 {
			t.Fatalf("/%s: submissions = %d, want 1", tc.name, len(fc.submitted))
		}
		prompt := fc.submitted[0]
		if !strings.Contains(prompt, tc.want) {
			t.Errorf("/%s prompt missing %q:\n%s", tc.name, tc.want, prompt)
		}
		if !strings.Contains(prompt, d.directive) {
			t.Errorf("/%s prompt lost its directive:\n%s", tc.name, prompt)
		}
		if !strings.HasPrefix(prompt, d.directive) {
			t.Errorf("/%s directive must precede the request:\n%s", tc.name, prompt)
		}
	}
}

// TestDevSubmitErrorPropagates: a Context refusal (e.g. no session)
// surfaces as the command's error, never silently swallowed.
func TestDevSubmitErrorPropagates(t *testing.T) {
	fc := &fakeContext{submitErr: errNoSession}
	err := devByName(t, "build").Execute(fc, nil)
	if err != errNoSession {
		t.Fatalf("error = %v, want the SubmitPrompt error", err)
	}
	if len(fc.submitted) != 0 {
		t.Errorf("failed submit recorded a prompt: %v", fc.submitted)
	}
}

var errNoSession = &submitRefused{}

type submitRefused struct{}

func (*submitRefused) Error() string { return "no active session" }

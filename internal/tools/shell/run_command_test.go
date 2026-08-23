package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"lato/internal/process"
)

// newRunCommandFixture builds the portable helper binary once and
// returns a run_command tool plus the workspace root it is confined to.
func newRunCommandFixture(t *testing.T) (*RunCommand, *process.Runner, string) {
	t.Helper()
	helper := buildHelper(t)
	r, err := process.NewRunner(t.TempDir())
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return NewRunCommand(r), r, helper
}

// buildHelper compiles the portable test double into a per-test
// temporary directory. No shell is assumed anywhere: the suite talks to
// this binary directly on every platform.
func buildHelper(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lato-shell-helper")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "../../process/testdata/helper")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building test helper: %v\n%s", err, out)
	}
	return bin
}

func TestRunCommandSuccessAndFailureResults(t *testing.T) {
	tool, _, helper := newRunCommandFixture(t)

	res, err := tool.Execute(context.Background(), map[string]any{"command": helper + " -stdout ok"})
	if err != nil || res.IsError {
		t.Fatalf("success case: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Content, "SUCCESS") {
		t.Errorf("success result missing SUCCESS header:\n%s", res.Content)
	}

	res, err = tool.Execute(context.Background(), map[string]any{"command": helper + " -fail 2 -stderr broken"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Error("nonzero exit must surface as an error result for the model")
	}
	for _, want := range []string{"FAILURE", "exit code 2", "broken"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("failure result missing %q:\n%s", want, res.Content)
		}
	}
}

func TestRunCommandMissingProgramIsSoftError(t *testing.T) {
	tool, _, _ := newRunCommandFixture(t)

	res, err := tool.Execute(context.Background(), map[string]any{"command": "no-such-binary-xyz --flag"})
	if err != nil {
		t.Fatalf("Execute returned a hard error; start failures are soft: %v", err)
	}
	if !res.IsError {
		t.Fatal("start failure should be an error result")
	}
	if !strings.Contains(res.Content, "no-such-binary-xyz") {
		t.Errorf("result should name the missing program:\n%s", res.Content)
	}
}

func TestRunCommandBadArgumentsAreRejected(t *testing.T) {
	tool, _, _ := newRunCommandFixture(t)

	// Argument extraction failures are hard errors, matching the
	// editing tools; the runtime reports them through its error path.
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("missing command must be a hard error")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"command": 42}); err == nil {
		t.Error("non-string command must be a hard error")
	}

	// Semantic problems stay soft so the model can correct and retry.
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"empty command", map[string]any{"command": "   "}, "command is empty"},
		{"unterminated quote", map[string]any{"command": `echo "open`}, "unterminated quote"},
		{"bad timeout type", map[string]any{"command": "true", "timeout_seconds": "soon"}, "must be a number"},
		{"timeout too large", map[string]any{"command": "true", "timeout_seconds": 100000}, "exceeds the maximum"},
	}
	for _, tc := range cases {
		res, err := tool.Execute(context.Background(), tc.args)
		if err != nil {
			t.Errorf("%s: hard error: %v", tc.name, err)
			continue
		}
		if !res.IsError || !strings.Contains(res.Content, tc.want) {
			t.Errorf("%s: res=%q err=%v, want soft error containing %q", tc.name, res.Content, err, tc.want)
		}
	}
}

// TestRunCommandTimeoutOption verifies the model-facing timeout argument
// reaches the engine.
func TestRunCommandTimeoutOption(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps past a short timeout")
	}
	tool, _, helper := newRunCommandFixture(t)

	res, err := tool.Execute(context.Background(), map[string]any{
		"command":         helper + " -sleep 30s",
		"timeout_seconds": 1,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "timed out") {
		t.Fatalf("want timeout failure, got:\n%s", res.Content)
	}
}

// TestRunCommandDirOption pins that the dir argument runs the command in
// a workspace subdirectory and that escapes are refused.
func TestRunCommandDirOption(t *testing.T) {
	tool, runner, helper := newRunCommandFixture(t)

	if err := os.MkdirAll(filepath.Join(runner.Root(), "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res, err := tool.Execute(context.Background(), map[string]any{
		"command": helper + " -write marker.txt",
		"dir":     "sub",
	})
	if err != nil || res.IsError {
		t.Fatalf("dir=sub run failed: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Content, filepath.Join(runner.Root(), "sub")) {
		t.Errorf("result should report the resolved working directory:\n%s", res.Content)
	}

	res, err = tool.Execute(context.Background(), map[string]any{
		"command": helper,
		"dir":     "../outside",
	})
	if err != nil || !res.IsError || !strings.Contains(res.Content, "escapes the workspace root") {
		t.Errorf("dir escape not refused: res=%q err=%v", res.Content, err)
	}
}

// TestDescribeShape checks the structured result rendering: every field
// the milestone requires appears in the output.
func TestDescribeShape(t *testing.T) {
	tool, _, helper := newRunCommandFixture(t)

	res, _ := tool.Execute(context.Background(), map[string]any{"command": helper + " -stdout out-text -stderr err-text"})
	for _, want := range []string{
		"SUCCESS",
		"command: " + helper,
		"working directory:",
		"duration:",
		"stdout:",
		"out-text",
		"stderr:",
		"err-text",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("result missing %q:\n%s", want, res.Content)
		}
	}
}

package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// helperProgram is a tiny portable command built once per test run. It
// lets the suite exercise stdout, stderr, exit codes, and timeouts
// without assuming any shell (bash, PowerShell, ...) exists on the host.
var helperProgram string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lato-process-helper")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	helperProgram = filepath.Join(dir, "lato-test-helper")
	if runtime.GOOS == "windows" {
		helperProgram += ".exe"
	}
	build := exec.Command("go", "build", "-o", helperProgram, "./testdata/helper")
	if out, err := build.CombinedOutput(); err != nil {
		panic(string(out) + ": " + err.Error())
	}

	os.Exit(m.Run())
}

// TestSuccessfulRun covers a zero-exit command: output captured, success
// reported, exit code 0.
func TestSuccessfulRun(t *testing.T) {
	r := newTestRunner(t)
	res := r.Run(context.Background(), Spec{
		Command: helperProgram,
		Args:    []string{"-stdout", "hello from lato"},
	})

	if !res.Success || res.TimedOut || res.StartErr != "" {
		t.Fatalf("unexpected outcome: %+v", res)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "hello from lato") {
		t.Errorf("Stdout = %q, want it to contain the echoed text", res.Stdout)
	}
	if res.Stderr != "" {
		t.Errorf("Stderr = %q, want empty", res.Stderr)
	}
	if res.Dir != r.root {
		t.Errorf("Dir = %q, want workspace root %q", res.Dir, r.root)
	}
	if res.Duration <= 0 {
		t.Errorf("Duration = %v, want positive", res.Duration)
	}
}

// TestFailedRunCoversExitCodeAndStreams verifies that a failing command
// yields SUCCESS=false with its real exit code and both streams intact.
func TestFailedRunCoversExitCodeAndStreams(t *testing.T) {
	r := newTestRunner(t)
	res := r.Run(context.Background(), Spec{
		Command: helperProgram,
		Args:    []string{"-fail", "3", "-stderr", "something broke"},
	})

	if res.Success {
		t.Fatal("Success = true for an exiting command")
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
	if res.TimedOut || res.StartErr != "" {
		t.Errorf("TimedOut/StartErr misreported: %+v", res)
	}
	if !strings.Contains(res.Stderr, "something broke") {
		t.Errorf("Stderr = %q, want it to contain the message", res.Stderr)
	}
}

// TestWorkingDirectoryIsWorkspaceRoot pins the M7 requirement: commands
// execute relative to the target workspace, not wherever Lato runs from.
func TestWorkingDirectoryIsWorkspaceRoot(t *testing.T) {
	r := newTestRunner(t)

	// The helper writes a marker file with a relative path; where it
	// lands proves which directory was in effect.
	res := r.Run(context.Background(), Spec{
		Command: helperProgram,
		Args:    []string{"-write", "marker.txt"},
	})
	if !res.Success {
		t.Fatalf("run failed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(r.root, "marker.txt")); err != nil {
		t.Fatalf("marker not written into the workspace root: %v", err)
	}

	// A subdirectory request must resolve inside the workspace too.
	sub := filepath.Join(r.root, "sub")
	os.MkdirAll(sub, 0o755)
	res = r.Run(context.Background(), Spec{Command: helperProgram, Args: []string{"-write", "deep.txt"}, Dir: "sub"})
	if !res.Success {
		t.Fatalf("subdirectory run failed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(sub, "deep.txt")); err != nil {
		t.Fatalf("dir=sub did not run inside sub/: %v", err)
	}
}

// TestDirConfinementRejectsEscapes verifies no dir argument can point
// the runner outside the workspace.
func TestDirConfinementRejectsEscapes(t *testing.T) {
	r := newTestRunner(t)

	for _, dir := range []string{
		"..",
		"../..",
		"sub/../../outside",
		"/etc",
		"C:\\Windows",
	} {
		res := r.Run(context.Background(), Spec{Command: helperProgram, Dir: dir})
		if res.StartErr == "" {
			t.Errorf("dir %q accepted; want rejection", dir)
			continue
		}
		if strings.Contains(res.Dir, "etc") || strings.Contains(res.Dir, "Windows") {
			t.Errorf("dir %q resolved outside the workspace: %q", dir, res.Dir)
		}
	}
}

// TestStartFailureIsReportedNotPanicked covers commands that cannot
// start at all (missing program).
func TestStartFailureIsReportedNotPanicked(t *testing.T) {
	r := newTestRunner(t)
	res := r.Run(context.Background(), Spec{Command: "definitely-not-a-real-program-xyz"})

	if res.Success {
		t.Fatal("Success = true for a program that does not exist")
	}
	if res.StartErr == "" {
		t.Fatalf("StartErr empty for missing program: %+v", res)
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 when the process never ran", res.ExitCode)
	}
}

// TestTimeoutKillsHungCommand proves a hung process cannot stall Lato.
func TestTimeoutKillsHungCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps past a short timeout")
	}
	r := newTestRunner(t)

	started := time.Now()
	res := r.Run(context.Background(), Spec{
		Command: helperProgram,
		Args:    []string{"-sleep", "60s"},
		Timeout: 300 * time.Millisecond,
	})
	elapsed := time.Since(started)

	if !res.TimedOut || res.Success {
		t.Fatalf("want timeout, got: %+v", res)
	}
	if !strings.Contains(res.Stderr, "timeout") {
		t.Errorf("stderr should explain the timeout, got %q", res.Stderr)
	}
	if elapsed > 10*time.Second {
		t.Errorf("run took %v after timing out at 300ms; process kill leaked", elapsed)
	}
}

// TestLargeOutputIsBounded feeds far more than MaxCapture through both
// streams and checks the retained text stays within bounds while
// keeping head and tail content.
func TestLargeOutputIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("moves megabytes of pipe data")
	}
	r := newTestRunner(t)

	res := r.Run(context.Background(), Spec{
		Command: helperProgram,
		Args:    []string{"-spam", "HEADMARKER", "TAILMARKER"},
	})

	if len(res.Stdout) > MaxCapture {
		t.Errorf("captured stdout = %d bytes, bound is %d", len(res.Stdout), MaxCapture)
	}
	if !res.StdoutTruncated {
		t.Error("StdoutTruncated = false despite overflowing output")
	}
	if !strings.Contains(res.Stdout, "HEADMARKER") || !strings.Contains(res.Stdout, "TAILMARKER") {
		t.Error("truncation lost both head and tail markers")
	}
	if !strings.Contains(res.Stdout, "[output truncated") {
		t.Error("truncation marker missing from clipped stream")
	}
}

// TestParentContextCancellation verifies caller cancellation propagates:
// a canceled context ends the run instead of waiting for the child.
func TestParentContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps until canceled")
	}
	r := newTestRunner(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	res := r.Run(ctx, Spec{Command: helperProgram, Args: []string{"-sleep", "30s"}})
	if res.Success || res.TimedOut {
		t.Errorf("canceled run misreported as Success/TimedOut: %+v", res)
	}
	if res.Duration > 5*time.Second {
		t.Errorf("cancellation took %v to take effect", res.Duration)
	}
}

// TestSplitCommandPortable covers the cross-platform command-line
// splitting rules: quotes group whitespace, backslashes stay literal,
// no shell metacharacters are interpreted.
func TestSplitCommandPortable(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"go test ./...", []string{"go", "test", "./..."}},
		{"npm test", []string{"npm", "test"}},
		{`cargo test -- --nocapture`, []string{"cargo", "test", "--", "--nocapture"}},
		{`git commit -m "fix: the bug"`, []string{"git", "commit", "-m", "fix: the bug"}},
		{"echo 'two words'", []string{"echo", "two words"}},
		{`C:\tools\app.exe --path C:\Users\me`, []string{`C:\tools\app.exe`, `--path`, `C:\Users\me`}},
		{`./scripts/build.sh --target all`, []string{"./scripts/build.sh", "--target", "all"}},
		{"  spaced   out  ", []string{"spaced", "out"}},
	}
	for _, tc := range cases {
		got, err := SplitCommand(tc.in)
		if err != nil {
			t.Errorf("SplitCommand(%q) error: %v", tc.in, err)
			continue
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("SplitCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{"", "   ", `git commit -m "unterminated`} {
		if got, err := SplitCommand(bad); err == nil {
			t.Errorf("SplitCommand(%q) = %q, want an error", bad, got)
		}
	}
}

// TestOnExitHookFiresOnlyForStartedProcesses pins the hook contract the
// runtime uses for index invalidation: fired when a process ran, skipped
// when nothing started.
func TestOnExitHookFiresOnlyForStartedProcesses(t *testing.T) {
	r := newTestRunner(t)

	fired := 0
	r.OnExit = func(Result) { fired++ }

	r.Run(context.Background(), Spec{Command: helperProgram})
	if fired != 1 {
		t.Errorf("hook fired %d times after one completed run, want 1", fired)
	}

	r.Run(context.Background(), Spec{Command: "definitely-not-a-real-program-xyz"})
	if fired != 1 {
		t.Errorf("hook fired for a start failure, want no call")
	}
}

// TestResolveDirNormalizesSeparators exercises the style-independent
// directory resolution directly.
func TestResolveDirNormalizesSeparators(t *testing.T) {
	r := newTestRunner(t)

	if got, err := r.resolveDir(""); err != nil || got != r.root {
		t.Errorf(`resolveDir("") = %q, %v; want root`, got, err)
	}
	if got, err := r.resolveDir("."); err != nil || got != r.root {
		t.Errorf(`resolveDir(".") = %q, %v; want root`, got, err)
	}

	sub := filepath.Join(r.root, "a", "b")
	os.MkdirAll(sub, 0o755)
	got, err := r.resolveDir("a/b")
	if err != nil || got != sub {
		t.Errorf(`resolveDir("a/b") = %q, %v; want %q`, got, err, sub)
	}

	// Windows-style separators name the same directory on every OS.
	got, err = r.resolveDir(`a\b`)
	if err != nil || got != sub {
		t.Errorf(`resolveDir("a\\b") = %q, %v; want %q`, got, err, sub)
	}
}

func newTestRunner(t *testing.T) *Runner {
	t.Helper()
	r, err := NewRunner(t.TempDir())
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return r
}

// Package process runs external commands inside the target workspace
// and reports their outcome as structured data.
//
// It is the execution engine behind Lato's command tool: every command
// is launched with its working directory pinned to (a validated
// subdirectory of) the discovered workspace root, never to whatever
// directory the Lato binary itself was started from. Commands run
// directly through os/exec — no shell is involved — so nothing here
// assumes bash, PowerShell, or any other command interpreter, and the
// same code path serves Linux and Windows.
//
// All outcomes, including "the program could not start", are reported
// as a Result rather than an error, so callers (and ultimately the
// model) always receive actionable output. Output capture is bounded so
// a runaway build cannot exhaust memory or flood the model's context,
// and every run sits under a timeout so a hung process can never stall
// Lato forever.
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultTimeout applies when a Spec requests no timeout. Long
	// enough for real builds and test suites, short enough that a hung
	// command cannot freeze an interactive session indefinitely.
	DefaultTimeout = 2 * time.Minute

	// MaxTimeout caps caller-requested timeouts.
	MaxTimeout = 30 * time.Minute

	// MaxCapture bounds how much of each output stream is kept. When a
	// stream produces more, the beginning and the end are retained and
	// the middle is replaced by a marker, since both early output
	// (configuration, progress) and late output (errors, summaries) aid
	// diagnosis.
	MaxCapture = 128 << 10 // 128 KiB per stream

	// waitDelay bounds how long Wait lingers on pipes that stay open
	// after the process died (e.g. killed by the timeout while a
	// grandchild still holds a descriptor), so a stubborn descendant
	// cannot hang the run.
	waitDelay = 5 * time.Second
)

// Spec describes one command execution. It is pure input: Runner.Run
// never mutates it.
type Spec struct {
	Command string   // program name (resolved via PATH) or path
	Args    []string // arguments passed to the program, without the program name

	// Dir is the working directory for the run, relative to the
	// workspace root and slash-separated. Empty means the root itself.
	// Absolute paths, drive letters, and ".." segments are rejected;
	// commands can never steer the runner outside the workspace.
	Dir string

	// Timeout bounds a single run. Zero or negative selects
	// DefaultTimeout; values above MaxTimeout are clamped.
	Timeout time.Duration
}

// Result is the complete outcome of one attempted run. Every field is
// meaningful regardless of how the run ended; StartErr distinguishes
// "never started" from "ran and failed".
type Result struct {
	CommandLine string // human-readable rendering of the requested command
	Dir         string // absolute working directory the command ran in

	ExitCode int // process exit code, -1 when there is none (start failure, signal)

	Stdout string
	Stderr string

	StdoutTruncated bool
	StderrTruncated bool

	Success  bool // the process ran and exited 0
	TimedOut bool // the timeout elapsed and the process was killed

	StartErr string // non-empty when the process could not be started at all

	Duration time.Duration
}

// Runner executes commands confined to one workspace root.
type Runner struct {
	root string

	// OnExit, when set, is called after a process actually ran (whether
	// it succeeded, failed, or timed out) but not when the command could
	// not be started. Because a command may modify any file under the
	// workspace, owners of derived state use it to invalidate caches
	// lazily; it must be cheap and runs synchronously on the caller's
	// goroutine.
	OnExit func(Result)
}

// NewRunner returns a Runner that confines every command to root.
// Root must exist and be a directory; it is normally the workspace root
// discovered at startup.
func NewRunner(root string) (*Runner, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("process runner root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("process runner root %q is not a directory", root)
	}
	return &Runner{root: root}, nil
}

// Root returns the absolute path all commands are confined to.
func (r *Runner) Root() string { return r.root }

// Run executes spec and reports its outcome. It never returns an error:
// every possible outcome — success, nonzero exit, timeout, signal, or a
// program that could not be started — is carried by the Result, so
// callers can feed it straight to the model.
func (r *Runner) Run(ctx context.Context, spec Spec) Result {
	res := Result{
		CommandLine: renderCommandLine(spec),
		ExitCode:    -1,
	}

	dir, err := r.resolveDir(spec.Dir)
	if err != nil {
		res.StartErr = err.Error()
		return res
	}
	res.Dir = dir

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxTimeout {
		timeout = MaxTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout := newBoundedWriter(MaxCapture)
	stderr := newBoundedWriter(MaxCapture)

	started := time.Now()
	cmd := exec.CommandContext(runCtx, spec.Command, spec.Args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay

	startErr := cmd.Start()
	if startErr != nil {
		res.Duration = time.Since(started)
		res.StartErr = fmt.Sprintf("cannot start %q: %v", spec.Command, startErr)
		return res
	}

	waitErr := cmd.Wait()

	res.Duration = time.Since(started)
	res.Stdout, res.StdoutTruncated = stdout.Text()
	res.Stderr, res.StderrTruncated = stderr.Text()

	switch {
	case runCtx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
		res.Stderr = appendNote(res.Stderr, fmt.Sprintf("command killed after exceeding its %s timeout", timeout))
	case waitErr != nil && !errors.As(waitErr, new(*exec.ExitError)):
		// The process ran but Wait failed for another reason (e.g. it
		// was killed by a signal); report the raw reason where stderr
		// would have gone.
		res.Stderr = appendNote(res.Stderr, waitErr.Error())
	}
	if exit := cmd.ProcessState.ExitCode(); exit >= 0 {
		res.ExitCode = exit
	}
	res.Success = !res.TimedOut && res.ExitCode == 0

	if r.OnExit != nil {
		r.OnExit(res)
	}
	return res
}

// resolveDir turns a workspace-relative directory request into an
// absolute path, rejecting anything that would place the command
// outside the workspace. The checks mirror the editing engine's path
// rules: forward slashes and backslashes both work as separators, while
// absolute forms ("/x"), Windows drive letters ("C:x"), UNC shares
// ("\\host\share"), and ".." segments are refused on every platform.
func (r *Runner) resolveDir(rel string) (string, error) {
	p := strings.TrimSpace(rel)
	if p == "" {
		return r.root, nil
	}

	s := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p, "\\", "/")))
	switch {
	case strings.HasPrefix(s, "/"):
		return "", fmt.Errorf("%q is absolute; use a directory relative to the workspace root", rel)
	case isDriveColon(s):
		return "", fmt.Errorf("%q contains a drive letter; use a directory relative to the workspace root", rel)
	case hasDotDotSegment(s):
		return "", fmt.Errorf("%q escapes the workspace root", rel)
	}
	s = strings.TrimSuffix(s, "/")

	abs := filepath.Join(r.root, filepath.FromSlash(s))
	if back, err := filepath.Rel(r.root, abs); err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q escapes the workspace root", rel)
	}
	return abs, nil
}

// truncationMarker is spliced into a stream whose output exceeded its
// capture bound. Its size comes out of the retained bytes so a clipped
// stream never exceeds the bound.
var truncationMarker = []byte("\n... [output truncated: showing the beginning and the end] ...\n")

// boundedWriter is an io.Writer that retains at most cap bytes: the
// first part of the stream, and — once the cap is exceeded — the most
// recent tail, eliding whatever fell out behind a marker. Writes never
// block and memory use never grows past the cap, so a chatty build can
// neither exhaust memory nor flood the model's context.
type boundedWriter struct {
	head     []byte // first bytes of the stream
	headLen  int
	tail     []byte // ring buffer of the most recent bytes
	tailLen  int
	tailNext int // next write position in the ring
	dropped  int64
}

func newBoundedWriter(capacity int) *boundedWriter {
	markerLen := len(truncationMarker)
	budget := capacity - markerLen
	headCap := budget * 2 / 3
	tailCap := budget - headCap
	return &boundedWriter{
		head: make([]byte, headCap),
		tail: make([]byte, tailCap),
	}
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	n := len(p)

	// Fill the head first; only true overflow reaches the ring.
	free := len(w.head) - w.headLen
	if free > 0 {
		copied := copy(w.head[w.headLen:], p)
		w.headLen += copied
		p = p[copied:]
	}

	for _, b := range p {
		if w.tailLen == len(w.tail) {
			// Ring full: the oldest byte falls out.
			w.dropped++
			w.tail[w.tailNext] = b
			w.tailNext = (w.tailNext + 1) % len(w.tail)
		} else {
			w.tail[(w.tailNext+w.tailLen)%len(w.tail)] = b
			w.tailLen++
		}
	}
	return n, nil
}

// Text returns the retained portion of the stream and whether anything
// was dropped.
func (w *boundedWriter) Text() (string, bool) {
	var b strings.Builder
	b.Write(w.head[:w.headLen])
	b.Write(w.ringBytes())
	return b.String(), w.dropped > 0
}

func (w *boundedWriter) ringBytes() []byte {
	if w.dropped == 0 {
		return nil // the ring only participates once something fell out
	}
	out := make([]byte, 0, len(truncationMarker)+w.tailLen)
	out = append(out, truncationMarker...)
	first := (w.tailNext + len(w.tail) - w.tailLen) % len(w.tail)
	if first+w.tailLen <= len(w.tail) {
		out = append(out, w.tail[first:first+w.tailLen]...)
	} else {
		out = append(out, w.tail[first:]...)
		out = append(out, w.tail[:first+w.tailLen-len(w.tail)]...)
	}
	return out
}

// appendNote appends a diagnostic line to a captured stream, trimming
// the front if necessary to stay within the capture bound.
func appendNote(text, note string) string {
	line := "\n[note] " + note + "\n"
	if over := len(text) + len(line) - MaxCapture; over > 0 {
		text = text[min(over, len(text)):]
	}
	return text + line
}

// renderCommandLine renders the requested command for display,
// quoting arguments that contain whitespace.
func renderCommandLine(spec Spec) string {
	parts := make([]string, 0, len(spec.Args)+1)
	parts = append(parts, spec.Command)
	for _, a := range spec.Args {
		if strings.ContainsAny(a, " \t\"") {
			parts = append(parts, fmt.Sprintf("%q", a))
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// SplitCommand turns a single command line into program and arguments,
// honoring double and single quotes. Whitespace separates arguments and
// quotes group whitespace-containing text; there is no escape
// character, so backslashes always mean themselves — a deliberate
// choice that keeps Windows paths intact and the rule identical on both
// platforms. No shell features exist here: a command is exactly one
// program plus its arguments.
func SplitCommand(line string) ([]string, error) {
	var (
		fields []string
		cur    strings.Builder
		open   bool // the current field has begun
		quote  rune // active quote character, 0 when not inside quotes
	)
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			open = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if open {
				fields = append(fields, cur.String())
				cur.Reset()
				open = false
			}
		default:
			cur.WriteRune(r)
			open = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in command %q", line)
	}
	if open {
		fields = append(fields, cur.String())
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("command is empty")
	}
	return fields, nil
}

// isDriveColon reports whether p begins with a Windows drive prefix
// such as "C:" or "d:/", which must never be treated as workspace-
// relative.
func isDriveColon(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
}

// hasDotDotSegment reports whether any path segment would climb above
// the workspace root.
func hasDotDotSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

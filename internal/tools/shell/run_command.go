package shell

import (
	"context"
	"fmt"
	"strings"
	"time"

	"lato/internal/process"
	"lato/internal/tools"
)

// maxTimeoutSeconds caps model-requested timeouts; the engine clamps to
// its own MaxTimeout as well, but refusing nonsense early keeps the
// error message actionable.
const maxTimeoutSeconds = int(process.MaxTimeout / time.Second)

// RunCommand executes one program with arguments inside the target
// workspace and reports its outcome.
type RunCommand struct {
	runner *process.Runner
}

// NewRunCommand returns a ready-to-register run_command tool bound to r.
func NewRunCommand(r *process.Runner) *RunCommand {
	return &RunCommand{runner: r}
}

func (RunCommand) Name() string { return "run_command" }

func (RunCommand) Description() string {
	return "Run a command in the target workspace and report its result. The command is a single program " +
		"with arguments (e.g. \"go test ./...\" or \"npm test\") — it runs directly, without a shell, so " +
		"pipes and redirections are not available. Use dir for a subdirectory of the workspace root. " +
		"Returns the exit code, stdout, stderr, and whether the command succeeded; a nonzero exit code is a " +
		"normal failure result you should read and react to, not a tool error."
}

func (RunCommand) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command line to run: program name followed by arguments, e.g. \"go test ./...\".",
			},
			"dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root (default: the root itself).",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": fmt.Sprintf("Optional timeout in seconds (default %d, max %d).", int(process.DefaultTimeout/time.Second), maxTimeoutSeconds),
			},
		},
		"required": []string{"command"},
	}
}

func (t *RunCommand) Execute(ctx context.Context, args map[string]any) (tools.Result, error) {
	line, err := tools.StringArg(args, "command")
	if err != nil {
		return tools.Result{}, err
	}

	fields, err := process.SplitCommand(line)
	if err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}

	spec := process.Spec{Command: fields[0], Args: fields[1:]}
	if d := tools.OptionalStringArg(args, "dir", ""); d != "" {
		spec.Dir = d
	}
	if s, err := tools.OptionalIntArg(args, "timeout_seconds", 0); err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	} else if s > 0 {
		if s > maxTimeoutSeconds {
			return tools.Result{IsError: true, Content: fmt.Sprintf("timeout_seconds %d exceeds the maximum of %d", s, maxTimeoutSeconds)}, nil
		}
		spec.Timeout = time.Duration(s) * time.Second
	}

	res := t.runner.Run(ctx, spec)
	return tools.Result{Content: describe(res), IsError: !res.Success}, nil
}

// describe renders a completed run as text the model can act on,
// leading with SUCCESS or FAILURE so the outcome is unambiguous even
// when output is empty.
func describe(res process.Result) string {
	var b strings.Builder
	status := "SUCCESS"
	if res.TimedOut {
		status = "FAILURE (timed out)"
	} else if !res.Success {
		status = fmt.Sprintf("FAILURE (exit code %d)", res.ExitCode)
	}
	fmt.Fprintf(&b, "%s\n", status)
	fmt.Fprintf(&b, "command: %s\n", res.CommandLine)
	fmt.Fprintf(&b, "working directory: %s\n", res.Dir)
	fmt.Fprintf(&b, "duration: %s\n", res.Duration.Round(time.Millisecond))

	stdout, stderr := strings.TrimRight(res.Stdout, "\n"), strings.TrimRight(res.Stderr, "\n")
	switch {
	case res.StartErr != "":
		fmt.Fprintf(&b, "\nerror: %s\n", res.StartErr)
	default:
		if stdout != "" || stderr != "" || res.ExitCode != -1 {
			fmt.Fprintf(&b, "\nstdout:\n%s\n", orNone(stdout))
			fmt.Fprintf(&b, "\nstderr:\n%s\n", orNone(stderr))
		}
	}
	if res.StdoutTruncated || res.StderrTruncated {
		b.WriteString("\n(output was truncated)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func orNone(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}

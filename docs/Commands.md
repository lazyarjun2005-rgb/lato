# Commands

Package: `internal/process`, tools in `internal/tools/shell`

The command execution engine lets Lato run programs inside the target repository and understand their results. It is the verification half of the agent loop: edit, run, inspect, fix. Like editing, it is local and deterministic — no model calls, no network, no telemetry.

## Purpose

A coding agent must be able to run the project's own build, test, and tooling commands and read their output. Lato executes commands directly through Go's `os/exec`: no shell is involved, so nothing assumes bash, PowerShell, or any other interpreter exists, and the same code path serves Linux and Windows.

## Architecture

```
model turn
   │  tool call: run_command
   ▼
internal/tools/shell         tool layer: argument parsing, soft errors, result text
   │
   ▼
internal/process             engine: workspace-confined execution, timeout, bounded capture
   ├── NewRunner(root)       binds execution to the workspace root
   ├── Run(Spec)             launches the process, returns a Result
   ├── SplitCommand(line)    portable command-line splitting (quotes, no shell)
   └── boundedWriter         head+tail output capture with a truncation marker
```

The engine knows nothing about tools, providers, or the runtime. The runtime registers the tool against its discovered workspace root and invalidates the cached repository index after every started run — a command may create or change workspace files behind Lato's back (code generators, formatters, builds), so search must never serve stale content afterwards (see [Runtime](Runtime.md)).

## Tool: `run_command`

Runs one program with its arguments inside the workspace.

| Argument          | Required | Description                                                        |
| ----------------- | -------- | ------------------------------------------------------------------ |
| `command`         | yes      | Command line: program name plus arguments, e.g. `go test ./...`.   |
| `dir`             | no       | Working directory relative to the workspace root (default: root).  |
| `timeout_seconds` | no       | Per-run timeout (default 120, max 1800).                           |

### Command-line splitting

The command line is split on whitespace; double and single quotes group whitespace-containing arguments. There is no escape character, so backslashes always mean themselves — Windows paths stay intact and the rule is identical on both platforms. No shell features exist: pipes, redirection, variable expansion, and command chaining (`&&`, `;`) are not available. A command is exactly one program with its arguments. To run a shell pipeline, the model can create a script with `create_file` and run it with the platform's interpreter.

### Working directory behavior

Every command executes with its working directory pinned to the discovered **target workspace root** — the project Lato was launched from — never the directory containing the Lato binary. `dir` may name a subdirectory, resolved against the root. Path validation mirrors the editing engine's rules: forward slashes and backslashes both work as separators, while absolute paths (`/x`), Windows drive letters (`C:x`), UNC shares, and `..` segments are rejected on every platform, so a command can never be steered outside the workspace.

### Result format

Every run produces a structured result the model reads directly. `Run` never returns an error — all outcomes, including "the program could not be started", are carried by the result:

```
SUCCESS
command: go test ./...
working directory: /home/user/project
duration: 3.2s

stdout:
ok  	example.com/pkg	0.014s

stderr:
(empty)
```

- `SUCCESS` / `FAILURE (exit code N)` / `FAILURE (timed out)` leads the result, so the outcome is unambiguous even when output is empty.
- A nonzero exit code is a normal `FAILURE` result (soft error), which the model is expected to read and react to — not a tool error.
- A start failure (program not found) is reported as `error: cannot start ...` with exit code `-1`.
- Both streams are captured separately; empty streams print as `(empty)`.

### Timeout behavior

Each run is bounded by a timeout (default 2 minutes, capped at 30 minutes; `timeout_seconds` overrides per call). On expiry the process is killed and the result reports `FAILURE (timed out)` with whatever output was produced plus a note explaining the kill. Caller cancellation (e.g. the user interrupting a session) also kills the process. A `WaitDelay` keeps a killed process's lingering pipes from hanging the run.

### Output bounding

Each stream is captured up to 128 KiB. When more arrives, the beginning and the end are kept and the middle is elided behind a marker (`... [output truncated: showing the beginning and the end] ...`), since early output (configuration) and late output (error summaries) aid diagnosis. Memory use never grows past the bound, and the result notes that truncation occurred.

## Security Model

- Commands execute only inside the workspace; directory escapes are refused before any process starts.
- Every `run_command` call passes through the centralized permission gate (M13, see [Runtime](Runtime.md) and `internal/permissions`): routine development commands (`go test`, `go build`, read-only git, search/listing tools) run automatically; destructive commands (`rm -rf`, `git reset --hard`, `git clean -fdx`, force pushes), shell-featured compounds, unknown programs, and anything with a working directory outside the workspace require explicit user confirmation first. Refusals return a structured result to the model.
- No desktop automation, system control, or network services are provided; the tool runs exactly one program per call.

## Testing

The tests use a tiny portable helper program (`internal/process/testdata/helper`) built by the test suite itself, so no shell is assumed on any platform. Covered: success/failure exit codes, stdout/stderr capture, working-directory pinning, directory confinement, start failures, timeout kill, caller cancellation, output bounding with head/tail retention, command-line splitting, and the index-invalidation hook.

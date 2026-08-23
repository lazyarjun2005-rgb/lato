// Runtime-level access to Lato's command execution engine (see
// internal/process). The run_command tool must be registered after the
// Runtime exists because it needs the discovered workspace root, and it
// reports completed runs back to the runtime so the cached repository
// index can be invalidated — a command such as a code generator may have
// changed workspace files behind Lato's back.
package runtime

import (
	"fmt"

	"lato/internal/process"
	"lato/internal/tools/shell"
)

// RegisterShellTools wires command execution into r's tool manager,
// pinned to the discovered workspace root. Every run that actually
// started a process invalidates the runtime's cached index lazily:
// nothing is rebuilt until something next reads it.
func (r *Runtime) RegisterShellTools() error {
	runner, err := process.NewRunner(r.workspace.Root)
	if err != nil {
		return fmt.Errorf("create process runner: %w", err)
	}
	runner.OnExit = func(process.Result) { r.noteEdit() }
	return shell.Register(r.manager, runner)
}

package shell

import (
	"lato/internal/process"
	"lato/internal/tools"
)

// Register adds the command execution tool to m, confined to r.
func Register(m *tools.Manager, r *process.Runner) error {
	return m.Register(NewRunCommand(r))
}

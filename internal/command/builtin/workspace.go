package builtin

import (
	"lato/internal/command"
)

// Workspace is the /workspace command. It prints a clean summary of the
// repository Lato is running inside. All information comes from the
// Discovery performed at startup; this command adds no logic of its own.
type Workspace struct{}

// NewWorkspace returns a ready-to-register /workspace command.
func NewWorkspace() *Workspace { return &Workspace{} }

func (Workspace) Name() string        { return "workspace" }
func (Workspace) Aliases() []string   { return nil }
func (Workspace) Description() string { return "Describe the current repository." }
func (Workspace) Usage() string       { return "/workspace" }

func (Workspace) Execute(ctx command.Context, _ []string) error {
	ctx.Println("%s", ctx.Workspace().Summary())
	return nil
}

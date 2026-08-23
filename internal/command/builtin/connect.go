package builtin

import (
	"fmt"

	"lato/internal/command"
)

// Connect is the /connect command. With no arguments it opens the
// interactive provider-connection flow: pick a provider, enter its
// endpoint and/or API key, validate against the provider's model list,
// and save the connection to Lato's user-level configuration.
//
// /connect import opens the same flow seeded from detected OpenCode or
// Claude Code gateway configurations instead of the static registry.
type Connect struct{}

// NewConnect returns a ready-to-register /connect command.
func NewConnect() *Connect { return &Connect{} }

func (Connect) Name() string      { return "connect" }
func (Connect) Aliases() []string { return nil }
func (Connect) Description() string {
	return "Connect a model provider interactively (/connect import imports from OpenCode/Claude)."
}
func (Connect) Usage() string { return "/connect [import]" }

func (Connect) Execute(ctx command.Context, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("expected at most one argument, got %d", len(args))
	}
	if len(args) == 1 && args[0] == "import" {
		ctx.OpenImportFlow()
		return nil
	}
	if len(args) == 1 {
		return fmt.Errorf("unknown argument %q (usage: %s)", args[0], Connect{}.Usage())
	}
	ctx.OpenConnectFlow()
	return nil
}

// ImportCmd is the /import command: an explicit alias for
// "/connect import" that seeds the connection flow from detected
// OpenCode or Claude Code configurations.
type ImportCmd struct{}

// NewImportCmd returns a ready-to-register /import command.
func NewImportCmd() *ImportCmd { return &ImportCmd{} }

func (ImportCmd) Name() string      { return "import" }
func (ImportCmd) Aliases() []string { return nil }
func (ImportCmd) Description() string {
	return "Import provider connections from OpenCode or Claude Code configs."
}
func (ImportCmd) Usage() string { return "/import" }

func (ImportCmd) Execute(ctx command.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("expected no arguments, got %d", len(args))
	}
	ctx.OpenImportFlow()
	return nil
}

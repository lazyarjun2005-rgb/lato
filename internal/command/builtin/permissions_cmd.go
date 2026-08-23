package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Permissions is the /permissions command: show the active permission
// policy at a glance and clear temporary approval state.
//
//	/permissions          concise policy summary
//	/permissions reset    drop allow-once / allow-for-task grants
type Permissions struct{}

// NewPermissions returns a ready-to-register /permissions command.
func NewPermissions() *Permissions { return &Permissions{} }

func (Permissions) Name() string      { return "permissions" }
func (Permissions) Aliases() []string { return nil }
func (Permissions) Description() string {
	return "Show the permission policy; /permissions reset clears temporary approvals."
}
func (Permissions) Usage() string { return "/permissions [reset]" }

func (Permissions) Execute(ctx command.Context, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: %s", Permissions{}.Usage())
	}

	if len(args) == 1 {
		if !strings.EqualFold(args[0], "reset") {
			return fmt.Errorf("unknown subcommand %q (usage: %s)", args[0], Permissions{}.Usage())
		}
		n := ctx.ResetPermissions()
		if n == 0 {
			ctx.Println("✓ No temporary approvals were active.")
		} else if n == 1 {
			ctx.Println("✓ Cleared 1 temporary approval.")
		} else {
			ctx.Println("✓ Cleared %d temporary approvals.", n)
		}
		return nil
	}

	summary := ctx.PermissionsSummary()
	if strings.TrimSpace(summary) == "" {
		ctx.Println("Permission policy unavailable.")
		return nil
	}
	ctx.Println("%s", summary)
	return nil
}

package builtin

import (
	"fmt"

	"lato/internal/command"
)

// Fast is the /fast command: drop this session to LOW effort for
// quicker, lighter answers. It reuses the existing M16 effort ladder —
// no separate model, provider path, or persistence — exactly like a
// session-only "/effort low", and it never touches config.yaml.
type Fast struct{}

// NewFast returns a ready-to-register /fast command.
func NewFast() *Fast { return &Fast{} }

func (Fast) Name() string        { return "fast" }
func (Fast) Aliases() []string   { return nil }
func (Fast) Description() string { return "Switch this session to low effort (session only)." }
func (Fast) Usage() string       { return "/fast" }

func (Fast) Execute(ctx command.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: %s", Fast{}.Usage())
	}
	if err := ctx.SetEffort("low", false); err != nil {
		return fmt.Errorf("switch effort: %w", err)
	}
	ctx.Println("⚡ Effort: %s (session only) — /effort <level> changes it back.", ctx.CurrentEffort())
	return nil
}

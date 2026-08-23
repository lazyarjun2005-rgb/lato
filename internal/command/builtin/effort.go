package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
	"lato/internal/effort"
)

// Effort is the /effort command: view or change Lato's agent-effort
// ladder (low → medium → high → ultra → lato-X). With no argument it
// reports the current level; with one it switches to it and saves it as
// the default, mirroring how /model persists.
type Effort struct{}

func NewEffort() *Effort { return &Effort{} }

func (Effort) Name() string        { return "effort" }
func (Effort) Aliases() []string   { return nil }
func (Effort) Description() string { return "Change agent effort: low, medium, high, ultra, lato-X." }
func (Effort) Usage() string       { return "/effort [level]" }

func (e Effort) Execute(ctx command.Context, args []string) error {
	if len(args) == 0 {
		current := ctx.CurrentEffort()
		var b strings.Builder
		fmt.Fprintf(&b, "Current effort: %s\n", current)
		b.WriteString("Ladder:")
		for _, lvl := range effort.All {
			marker := "  "
			if lvl.String() == current {
				marker = "›"
			}
			fmt.Fprintf(&b, "\n  %s %s — %s", marker, lvl, effortHint(lvl))
		}
		ctx.Println("%s\n\nChange with /effort <level>, or ←/→ inside /model.", strings.TrimRight(b.String(), "\n"))
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /effort [low|medium|high|ultra|lato-x]")
	}

	level := args[0]
	if _, err := effort.Parse(level); err != nil {
		return err
	}
	if err := ctx.SetEffort(level, true); err != nil {
		return err
	}
	ctx.Println("✓ Effort: %s — %s", ctx.CurrentEffort(), effortHint(mustLevel(ctx.CurrentEffort())))
	return nil
}

// mustLevel re-parses a value already validated upstream.
func mustLevel(s string) effort.Level {
	lvl, err := effort.Parse(s)
	if err != nil {
		return effort.Default
	}
	return lvl
}

func effortHint(lvl effort.Level) string {
	switch lvl {
	case effort.Low:
		return "fast and direct, minimal tool use"
	case effort.Medium:
		return "balanced planning and verification"
	case effort.High:
		return "thorough inspection, strong verification"
	case effort.Ultra:
		return "deep bounded agentic mode"
	case effort.LatoX:
		return "maximum bounded orchestration"
	default:
		return ""
	}
}

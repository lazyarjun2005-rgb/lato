package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Memory is the /memory command: inspect and manage persistent
// project-specific memory.
//
//	/memory              list remembered facts for this project
//	/memory add TEXT     remember a fact (user-provided)
//	/memory remove ID    delete one entry by ID or unique prefix
//	/memory clear        delete all memory for this project
type Memory struct{}

// NewMemory returns a ready-to-register /memory command.
func NewMemory() *Memory { return &Memory{} }

func (Memory) Name() string      { return "memory" }
func (Memory) Aliases() []string { return nil }
func (Memory) Description() string {
	return "Inspect or manage project memory (/memory add TEXT, remove ID, clear)."
}
func (Memory) Usage() string { return "/memory [add TEXT | remove ID | clear]" }

func (Memory) Execute(ctx command.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		return listMemory(ctx)
	}

	switch args[0] {
	case "add":
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			return fmt.Errorf("usage: /memory add <text>")
		}
		if err := ctx.RememberFact(text); err != nil {
			return fmt.Errorf("remember failed: %w", err)
		}
		ctx.Println("✓ Remembered. Use /memory to view stored facts.")
		return nil

	case "remove", "forget":
		if len(args) < 2 {
			return fmt.Errorf("usage: /memory remove <id>")
		}
		if err := ctx.ForgetMemory(args[1]); err != nil {
			return err
		}
		ctx.Println("✓ Removed memory %s.", args[1])
		return nil

	case "clear":
		if err := ctx.ClearMemory(); err != nil {
			return err
		}
		ctx.Println("✓ Project memory cleared.")
		return nil

	default:
		return fmt.Errorf("unknown subcommand %q (usage: %s)", args[0], Memory{}.Usage())
	}
}

func listMemory(ctx command.Context) error {
	summary := ctx.MemorySummary()
	if summary == "" {
		ctx.Println("No project memories stored yet. Add one with /memory add <text>.")
		return nil
	}
	ctx.Println("%s", summary)
	return nil
}

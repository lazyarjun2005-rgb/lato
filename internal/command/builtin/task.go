package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Task is the /task command: inspect and continue persistent tasks.
//
//	/task                list known tasks (resumable ones first)
//	/task resume [id]    continue a task through the M10 loop
//	/task abandon <id>   retire a resumable task without reverting files
type Task struct{}

// NewTask returns a ready-to-register /task command.
func NewTask() *Task { return &Task{} }

func (Task) Name() string      { return "task" }
func (Task) Aliases() []string { return nil }
func (Task) Description() string {
	return "Show, resume, or abandon multi-step tasks (/task resume [id])."
}
func (Task) Usage() string { return "/task [resume [id] | abandon <id>]" }

func (Task) Execute(ctx command.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		listing := ctx.TaskList()
		if strings.TrimSpace(listing) == "" {
			ctx.Println("No tasks recorded for this project.")
			return nil
		}
		ctx.Println("%s", listing)
		return nil
	}

	if len(args) > 2 {
		return fmt.Errorf("usage: %s", Task{}.Usage())
	}

	switch args[0] {
	case "resume":
		if len(args) == 2 && strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: %s", Task{}.Usage())
		}
		id := ""
		if len(args) == 2 {
			id = args[1]
		}
		return ctx.ResumeTask(id)

	case "abandon":
		if len(args) != 2 {
			return fmt.Errorf("usage: /task abandon <id>")
		}
		if err := ctx.AbandonTask(args[1]); err != nil {
			return err
		}
		ctx.Println("✓ Task %s abandoned. Repository changes were not reverted.", args[1])
		return nil

	default:
		return fmt.Errorf("unknown subcommand %q (usage: %s)", args[0], Task{}.Usage())
	}
}

package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Status is the /status command: a one-screen dashboard of the session.
// It reads only through command.Context accessors that already exist,
// so it can never drift from the real runtime state, and it performs no
// work of its own: no indexing, no scanning, no network.
type Status struct{}

// NewStatus returns a ready-to-register /status command.
func NewStatus() *Status { return &Status{} }

func (Status) Name() string        { return "status" }
func (Status) Aliases() []string   { return nil }
func (Status) Description() string { return "Show a summary of the current project and agent setup." }
func (Status) Usage() string       { return "/status" }

func (Status) Execute(ctx command.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: %s", Status{}.Usage())
	}

	ws := ctx.Workspace()

	var b strings.Builder

	b.WriteString("Session\n")
	writeField(&b, "Model", ctx.Model())
	writeField(&b, "Provider", ctx.Provider())
	writeField(&b, "Effort", ctx.CurrentEffort())

	b.WriteString("\nProject\n")
	if ws.Repository != "" {
		writeField(&b, "Name", ws.Repository)
	}
	if ws.Root != "" {
		writeField(&b, "Root", ws.Root)
	}
	if ws.IsGitRepo {
		branch := ws.Branch
		if branch == "" {
			branch = "(detached)"
		}
		writeField(&b, "Branch", branch)
	} else if ws.Root != "" {
		writeField(&b, "Git", "not a repository")
	}

	var stack []string
	seenStack := map[string]bool{}
	for _, v := range []string{ws.Language, ws.Framework, ws.BuildSystem, ws.PackageManager} {
		if v != "" && !seenStack[v] {
			seenStack[v] = true
			stack = append(stack, v)
		}
	}
	if len(stack) > 0 {
		writeField(&b, "Stack", strings.Join(stack, ", "))
	}

	b.WriteString("\nMore: /workspace, /index, /memory, /task, /permissions")
	ctx.Println("%s", strings.TrimRight(b.String(), "\n"))
	return nil
}

// writeField appends "  Label: value" unless value is empty.
func writeField(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "  %s: %s\n", label, value)
}

package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Rename is the /rename command: give the current session a persistent
// human-readable title. The whole argument list is the title, so
// multi-word names survive exactly as typed (whitespace runs collapse
// to single spaces, matching the shared parser). The title is saved
// immediately and shown by /sessions; untitled sessions keep the
// existing derived-preview display.
type Rename struct{}

// NewRename returns a ready-to-register /rename command.
func NewRename() *Rename { return &Rename{} }

func (Rename) Name() string        { return "rename" }
func (Rename) Aliases() []string   { return nil }
func (Rename) Description() string { return "Rename the current session (/rename <title>)." }
func (Rename) Usage() string       { return "/rename <title>" }

func (Rename) Execute(ctx command.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: %s", Rename{}.Usage())
	}

	title := strings.Join(args, " ")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("usage: %s", Rename{}.Usage())
	}

	if err := ctx.RenameSession(title); err != nil {
		return err
	}
	ctx.Println("✓ Session renamed to %q.", title)
	return nil
}

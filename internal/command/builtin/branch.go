package builtin

import (
	"strings"

	"lato/internal/command"
)

// Branch is the /branch command: snapshot the current conversation into
// a NEW independent session and continue working there.
//
//	/branch                  derived title: "<current> (branch)"
//	/branch My Direction     explicit multi-word title
//
// The original session is left byte-for-byte unchanged; the branch gets
// its own ID, timestamps, and message copy. Purely session management:
// no model call, no filesystem undo. Refused while a stream is active.
type Branch struct{}

// NewBranch returns a ready-to-register /branch command.
func NewBranch() *Branch { return &Branch{} }

func (Branch) Name() string      { return "branch" }
func (Branch) Aliases() []string { return nil }
func (Branch) Description() string {
	return "Create an independent copy of this session and switch to it."
}
func (Branch) Usage() string { return "/branch [title]" }

func (Branch) Execute(ctx command.Context, args []string) error {
	newID, err := ctx.BranchSession(strings.Join(args, " "))
	if err != nil {
		return err
	}
	short := newID
	if len(short) > 8 {
		short = short[:8]
	}
	ctx.Println("✓ Branched into a new session (%s). The original is untouched.", short)
	return nil
}

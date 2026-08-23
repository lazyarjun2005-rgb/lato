package builtin

import (
	"fmt"
	"strconv"

	"lato/internal/command"
)

// Rewind is the /rewind command: remove recent conversation history
// from the CURRENT session.
//
//	/rewind      drop the most recent turn (same as /rewind 1)
//	/rewind <N>  drop the most recent N turns
//
// A turn is one persisted user request plus its assistant response when
// one exists, so an unanswered final request is rewound by itself.
// This is a conversation-history operation only: no filesystem undo, no
// git operations, no tool-call replay. Refused while a stream is
// active; persistence failures roll the session back unchanged.
type Rewind struct{}

// NewRewind returns a ready-to-register /rewind command.
func NewRewind() *Rewind { return &Rewind{} }

func (Rewind) Name() string        { return "rewind" }
func (Rewind) Aliases() []string   { return nil }
func (Rewind) Description() string { return "Remove recent conversation turns (/rewind [N])." }
func (Rewind) Usage() string       { return "/rewind [N]" }

func (Rewind) Execute(ctx command.Context, args []string) error {
	turns := 1
	switch len(args) {
	case 0:
		// default: one turn
	case 1:
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return fmt.Errorf("usage: %s — N must be a positive number", Rewind{}.Usage())
		}
		turns = n
	default:
		return fmt.Errorf("usage: %s — exactly one optional count", Rewind{}.Usage())
	}

	removed, err := ctx.RewindConversation(turns)
	if err != nil {
		return err
	}

	label := "turn"
	if removed > 1 {
		label = "turns"
	}
	ctx.Println("✓ Rewound %d conversation %s.", removed, label)
	return nil
}

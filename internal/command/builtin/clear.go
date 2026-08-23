package builtin

import "lato/internal/command"

// Clear is the /clear command. It resets the CURRENT conversation:
// the visible transcript and the session's persisted Messages are
// emptied, so the next request starts fresh — while the session itself
// (ID, CreatedAt, Title), other sessions, memory, tasks, and
// model/provider/effort state all survive. Refused while a stream is
// active.
type Clear struct{}

// NewClear returns a ready-to-register /clear command.
func NewClear() *Clear { return &Clear{} }

func (Clear) Name() string        { return "clear" }
func (Clear) Aliases() []string   { return nil }
func (Clear) Description() string { return "Clear the conversation history and transcript." }
func (Clear) Usage() string       { return "/clear" }

func (Clear) Execute(ctx command.Context, _ []string) error {
	if err := ctx.ClearConversation(); err != nil {
		return err
	}
	ctx.Println("✓ Conversation cleared. The next message starts fresh.")
	return nil
}

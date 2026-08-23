package builtin

import (
	"strings"

	"lato/internal/command"
)

// Export is the /export command: write the current conversation to a
// Markdown file.
//
//	/export            safe default name derived from the session
//	/export <path>     explicit destination (multi-word paths join with
//	                   spaces, matching the shared parser)
//
// The file is written through Context.ExportConversation; existing
// files are never overwritten and the confirmation names the path only
// after the write actually succeeded.
type Export struct{}

// NewExport returns a ready-to-register /export command.
func NewExport() *Export { return &Export{} }

func (Export) Name() string        { return "export" }
func (Export) Aliases() []string   { return nil }
func (Export) Description() string { return "Export the conversation to a Markdown file." }
func (Export) Usage() string       { return "/export [path]" }

func (Export) Execute(ctx command.Context, args []string) error {
	written, err := ctx.ExportConversation(strings.Join(args, " "))
	if err != nil {
		return err
	}
	ctx.Println("✓ Conversation exported to %s.", written)
	return nil
}

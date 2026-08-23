package builtin

import (
	"fmt"

	"lato/internal/command"
)

// Index is the /index command. It reports what the repository index
// knows about the current workspace: the root, file and directory
// counts, language breakdown, Go packages and symbols, and ignored
// paths. All information comes from the runtime's cached index, which
// is built on first use; this command adds no indexing logic of its own.
type Index struct{}

// NewIndex returns a ready-to-register /index command.
func NewIndex() *Index { return &Index{} }

func (Index) Name() string        { return "index" }
func (Index) Aliases() []string   { return nil }
func (Index) Description() string { return "Show the repository index summary." }
func (Index) Usage() string       { return "/index" }

// Execute prints the index summary. The summary is multi-line, so it
// skips the fmt helper and writes directly.
func (Index) Execute(ctx command.Context, _ []string) error {
	idx := ctx.Index()
	if idx == nil {
		return fmt.Errorf("index not available")
	}
	ctx.Println("%s", idx.Summary())
	return nil
}

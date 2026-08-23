package builtin

import (
	"bytes"
	"fmt"
	"strings"

	"lato/internal/command"
	"lato/internal/doctor"
)

// Doctor is the /doctor command: the same environment report as
// `lato doctor`, rendered into the chat transcript. The shared renderer
// lives in internal/doctor; this command supplies it the runtime's
// cached workspace through Context, so invoking it never rescans the
// repository.
type Doctor struct{}

// NewDoctor returns a ready-to-register /doctor command.
func NewDoctor() *Doctor { return &Doctor{} }

func (Doctor) Name() string        { return "doctor" }
func (Doctor) Aliases() []string   { return nil }
func (Doctor) Description() string { return "Check installation, configuration, and environment." }
func (Doctor) Usage() string       { return "/doctor" }

func (Doctor) Execute(ctx command.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: %s", Doctor{}.Usage())
	}

	var buf bytes.Buffer
	doctor.Report(&buf, ctx.Workspace())
	ctx.Println("%s", strings.TrimRight(buf.String(), "\n"))
	return nil
}

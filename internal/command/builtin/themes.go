package builtin

import (
	"fmt"

	"lato/internal/command"
)

type Themes struct{}

func NewThemes() *Themes { return &Themes{} }

func (Themes) Name() string        { return "themes" }
func (Themes) Aliases() []string   { return nil }
func (Themes) Description() string { return "Browse and apply terminal themes." }
func (Themes) Usage() string       { return "/themes" }

func (Themes) Execute(ctx command.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: %s", Themes{}.Usage())
	}
	ctx.OpenThemePicker()
	return nil
}

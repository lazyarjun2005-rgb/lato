package builtin

import (
	"fmt"

	"lato/internal/command"
)

// Model is the /model command. With no arguments it opens the model
// picker; with one argument it switches to that model. "add" starts the
// custom-model registration flow; "refresh" reloads provider model
// lists.
type Model struct{}

// NewModel returns a ready-to-register /model command.
func NewModel() *Model { return &Model{} }

func (Model) Name() string      { return "model" }
func (Model) Aliases() []string { return nil }
func (Model) Description() string {
	return "Show or switch the active model (/model add registers custom model IDs, /model refresh reloads lists)."
}
func (Model) Usage() string { return "/model [name|add|refresh]" }

func (Model) Execute(ctx command.Context, args []string) error {
	if len(args) == 1 && args[0] == "refresh" {
		if err := ctx.RefreshModels(); err != nil {
			return fmt.Errorf("refresh models: %w", err)
		}
		return nil
	}
	if len(args) == 1 && args[0] == "add" {
		ctx.OpenAddModelFlow()
		return nil
	}
	if len(args) == 0 {
		ctx.OpenModelPicker()
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("expected exactly one model name, got %d", len(args))
	}

	name := args[0]
	if err := ctx.SetModel(name); err != nil {
		return fmt.Errorf("switch model: %w", err)
	}
	ctx.Println("✓ Model: %s", name)
	return nil
}

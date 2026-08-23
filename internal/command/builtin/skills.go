package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Skills is the /skills command: list the skill catalog discovered at
// startup. It shows ids, names, and descriptions only — full skill
// bodies load on demand through the agent's load_skill tool, exactly as
// they do inside a conversation.
type Skills struct{}

// NewSkills returns a ready-to-register /skills command.
func NewSkills() *Skills { return &Skills{} }

func (Skills) Name() string        { return "skills" }
func (Skills) Aliases() []string   { return nil }
func (Skills) Description() string { return "List available skills the agent can load." }
func (Skills) Usage() string       { return "/skills" }

func (Skills) Execute(ctx command.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: %s", Skills{}.Usage())
	}

	summary := ctx.SkillsSummary()
	if strings.TrimSpace(summary) == "" {
		ctx.Println("No skills found. Add Markdown skill files to your Lato skills directory and restart.")
		return nil
	}
	ctx.Println("Skills:\n%s", summary)
	return nil
}

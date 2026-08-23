package builtin

import (
	"fmt"
	"sort"
	"strings"

	"lato/internal/command"
	"lato/internal/session"
)

// Resume is the /resume command: continue a previous conversation.
//
//	/resume                open the session picker
//	/resume <id>           exact ID or unique ID prefix (e.g. 8f31c2a1)
//	/resume <title>        exact title as set with /rename
//
// Resolution never guesses: ambiguous prefixes or duplicate titles are
// refused with the candidates named. Legacy untitled sessions stay
// resumable by ID and through the picker.
type Resume struct{}

// NewResume returns a ready-to-register /resume command.
func NewResume() *Resume { return &Resume{} }

func (Resume) Name() string      { return "resume" }
func (Resume) Aliases() []string { return nil }
func (Resume) Description() string {
	return "Resume a session by ID or exact title (/resume <target>)."
}
func (Resume) Usage() string { return "/resume [<id|title>]" }

func (Resume) Execute(ctx command.Context, args []string) error {
	if len(args) == 0 {
		sessions, err := session.List()
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}
		if len(sessions) == 0 {
			return fmt.Errorf("no saved sessions yet")
		}
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		})
		ctx.OpenSessionPicker(sessions)
		return nil
	}

	return ctx.ResumeSession(strings.Join(args, " "))
}

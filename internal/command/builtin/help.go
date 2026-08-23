package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Help is the /help command (aliased to /?). It lists every command
// registered in reg, so it never goes stale as commands are added or
// removed — there is nothing to update by hand.
type Help struct {
	reg *command.Registry
}

// NewHelp returns a ready-to-register /help command that lists whatever
// is registered in reg at the time /help runs. reg is typically the same
// Registry that Help itself is about to be registered into: construct
// the Registry first, then construct Help with it, then Register(help).
func NewHelp(reg *command.Registry) *Help {
	return &Help{reg: reg}
}

func (Help) Name() string        { return "help" }
func (Help) Aliases() []string   { return []string{"?", "commands"} }
func (Help) Description() string { return "List available commands." }
func (Help) Usage() string       { return "/help" }

// helpSections orders /help output into logical groups. Mapping is by
// command name and purely presentational: every registered command is
// still listed exactly once, and anything unmapped lands in "Other"
// automatically, so new commands can never be forgotten by this file.
var helpSections = []struct {
	title string
	names []string
}{
	{"Chat & output", []string{"clear", "copy", "exit", "export", "help", "rename", "resume", "sessions"}},
	{"Models & providers", []string{"connect", "import", "model", "provider"}},
	{"Agent setup", []string{"effort", "fast", "skills", "status"}},
	{"Project state", []string{"memory", "permissions", "task"}},
	{"Development", []string{"build", "code", "debug", "explain", "fix", "refactor", "review", "run", "search", "test"}},
	{"Diagnostics", []string{"doctor", "version"}},
	{"Workspace", []string{"index", "workspace"}},
}

func (h *Help) Execute(ctx command.Context, _ []string) error {
	// Bucket registered commands by section, preserving registration
	// order within each bucket.
	bucket := map[string][]command.Command{}
	var others []command.Command
	registered := map[string]bool{}

	for _, cmd := range h.reg.All() {
		name := cmd.Name()
		registered[name] = true
		placed := false
		for _, s := range helpSections {
			if containsName(s.names, name) {
				bucket[s.title] = append(bucket[s.title], cmd)
				placed = true
				break
			}
		}
		if !placed {
			others = append(others, cmd)
		}
	}

	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, s := range helpSections {
		cmds := bucket[s.title]
		if len(cmds) == 0 {
			continue
		}
		writeSection(&b, s.title, cmds)
	}
	if len(others) > 0 {
		writeSection(&b, "Other", others)
	}

	ctx.Println("%s", strings.TrimRight(b.String(), "\n"))
	return nil
}

func writeSection(b *strings.Builder, title string, cmds []command.Command) {
	fmt.Fprintf(b, "\n%s\n", title)
	for _, cmd := range cmds {
		fmt.Fprintf(b, "  %-16s %s\n", cmd.Usage(), cmd.Description())
		if aliases := cmd.Aliases(); len(aliases) > 0 {
			fmt.Fprintf(b, "  %-16s (alias: %s)\n", "", joinAliases(aliases))
		}
	}
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func joinAliases(aliases []string) string {
	prefixed := make([]string, len(aliases))
	for i, a := range aliases {
		prefixed[i] = "/" + a
	}
	return strings.Join(prefixed, ", ")
}

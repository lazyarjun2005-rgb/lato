package builtin

import (
	"strings"
	"testing"

	"lato/internal/command"
)

// TestHelpListsEveryCommandExactlyOnce guards the M15 grouping: every
// registered command must appear in /help output exactly once, and the
// output must carry section headers.
func TestHelpListsEveryCommandExactlyOnce(t *testing.T) {
	reg := command.NewRegistry()
	reg.Register(NewExit())
	reg.Register(NewClear())
	reg.Register(NewCopy())
	reg.Register(NewMemory())
	reg.Register(NewTask())
	reg.Register(NewWorkspace())
	reg.Register(NewHelp(reg))

	fc := &fakeContext{}
	help := NewHelp(reg)
	if err := help.Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")

	for _, cmd := range reg.All() {
		usage := cmd.Usage()
		if c := strings.Count(out, usage); c != 1 {
			t.Errorf("usage %q appears %d times in help, want exactly 1:\n%s", usage, c, out)
		}
	}

	for _, section := range []string{"Chat & output", "Project state", "Workspace"} {
		if !strings.Contains(out, section) {
			t.Errorf("help missing section %q:\n%s", section, out)
		}
	}
}

// minimalCommand satisfies command.Command with just a name; help only
// needs Name and Usage for listing.
type minimalCommand struct{ name string }

func (m *minimalCommand) Name() string        { return m.name }
func (m *minimalCommand) Aliases() []string   { return nil }
func (m *minimalCommand) Description() string { return "test stub" }
func (m *minimalCommand) Usage() string       { return "/" + m.name }
func (m *minimalCommand) Execute(ctx command.Context, args []string) error {
	return nil
}

// TestHelpUnknownCommandsLandInOther pins the fallback: commands not
// mapped to a section are still listed under "Other" rather than
// dropped from discoverability.
func TestHelpUnknownCommandsLandInOther(t *testing.T) {
	reg := command.NewRegistry()
	reg.Register(NewExit()) // mapped
	reg.Register(&minimalCommand{name: "totally_new"})

	fc := &fakeContext{}
	if err := NewHelp(reg).Execute(fc, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := strings.Join(fc.lines, "\n")
	if !strings.Contains(out, "Other") || !strings.Contains(out, "/totally_new") {
		t.Errorf("unmapped command not listed under Other:\n%s", out)
	}
}

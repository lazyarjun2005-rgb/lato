package tui

import (
	"strings"
	"testing"

	"lato/internal/command"
	"lato/internal/config"
	"lato/internal/session"
)

// TestRegistryContainsMilestone1Commands pins that every Milestone 1
// command is registered in the production registry that powers
// dispatch, /help, and the slash palette alike.
func TestRegistryContainsMilestone1Commands(t *testing.T) {
	reg := newRegistry()
	for _, name := range []string{"version", "status", "doctor", "skills", "help"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("/%s missing from the production registry", name)
		}
	}
}

// TestMilestone1CommandsDispatchThroughTUIContext runs each new command
// through the real dispatcher with the TUI model as the Context, so the
// SkillsSummary addition is proven against the production implementation.
func TestMilestone1CommandsDispatchThroughTUIContext(t *testing.T) {
	rt := newPickerTestRuntime(t)

	cfgLoaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(cfgLoaded, session.New(), newUIAsker(), rt)

	cases := []struct {
		line string
		want []string
	}{
		{"/version", []string{"Lato"}},
		{"/status", []string{"Session", "Project", "Model:", "Effort:"}},
		{"/skills", []string{"No skills found"}},
		{"/doctor", []string{"Lato environment check", "Executable"}},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			isCommand, err := command.Dispatch(&m, m.registry, tc.line)
			if err != nil {
				t.Fatalf("%s: %v", tc.line, err)
			}
			if !isCommand {
				t.Fatalf("%s not recognized as a command", tc.line)
			}
			var out strings.Builder
			for _, e := range m.entries {
				out.WriteString(e.Content + "\n")
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("%s output missing %q:\n%s", tc.line, want, out.String())
				}
			}
		})
	}
}

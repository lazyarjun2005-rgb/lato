package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/command"
	"lato/internal/config"
	"lato/internal/runtime"
	"lato/internal/session"
)

// newOfflineTestRuntime builds a real runtime against an isolated
// configuration whose endpoint is a guaranteed-closed loopback port, so
// prompt-submission tests exercise stream wiring with no chance of
// contacting any model — local or remote.
func newOfflineTestRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LATO_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))

	cfg := `model:
  provider: ollama
  endpoint: http://127.0.0.1:9
  name: test-model
agent:
  name: default
  system_prompt: test
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	rt, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New() error = %v", err)
	}
	return rt
}

func newOfflineTestModel(t *testing.T) model {
	t.Helper()
	rt := newOfflineTestRuntime(t)
	cfgLoaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return newModel(cfgLoaded, session.New(), newUIAsker(), rt)
}

// assertSubmittedTurn checks everything SubmitPrompt must do before the
// existing submitInput promotion block takes over.
func assertSubmittedTurn(t *testing.T, m model, prompt string) {
	t.Helper()

	last := m.entries[len(m.entries)-1]
	if last.Role != roleUser || last.Content != prompt {
		t.Errorf("last transcript entry = %+v, want user %q", last, prompt)
	}
	msgs := m.session.Messages
	if n := len(msgs); n == 0 || msgs[n-1].Role != "user" || msgs[n-1].Content != prompt {
		t.Errorf("session messages lack trailing user %q: %+v", prompt, msgs)
	}
	if !m.waiting {
		t.Error("waiting must be true after submission")
	}
	if m.pendingStream == nil {
		t.Fatal("pendingStream must hold the started agent stream")
	}
	// Wiring-only assertion: never consume events here; detach instead.
	m.pendingStream = nil
	m.stream = nil
}

// TestSubmitPromptWiring pins the Context seam development commands rely
// on: a submitted prompt becomes a real user turn (transcript + session)
// and starts the ONE existing agent-loop stream via pendingStream —
// exactly like normal chat, with no second path.
func TestSubmitPromptWiring(t *testing.T) {
	m := newOfflineTestModel(t)

	const prompt = "inspect this repository"
	if err := m.SubmitPrompt(prompt); err != nil {
		t.Fatalf("SubmitPrompt() error = %v", err)
	}
	assertSubmittedTurn(t, m, prompt)
}

// TestEmptyPromptRefused guards trivial misuse: nothing is recorded,
// nothing starts.
func TestEmptyPromptRefused(t *testing.T) {
	m := newOfflineTestModel(t)

	before := len(m.entries)
	if err := m.SubmitPrompt("   "); err == nil {
		t.Fatal("empty prompt must be refused")
	}
	if len(m.entries) != before || m.waiting || m.pendingStream != nil {
		t.Error("refused submission must not touch state")
	}
}

// TestDevelopmentCommandsDispatchIntoAgentLoop runs representative dev
// commands through the REAL dispatcher with the TUI model as Context:
// each must land in SubmitPrompt and start the same loop a hand-typed
// message would.
func TestDevelopmentCommandsDispatchIntoAgentLoop(t *testing.T) {
	cases := []struct{ line, wantInTranscript string }{
		{"/explain internal/runtime/runtime.go", "Request: internal/runtime/runtime.go"},
		// Bare optional-arg command: the submitted prompt IS its directive.
		{"/build", "Build this project with its own build system"},
	}
	for _, tc := range cases {
		m := newOfflineTestModel(t)
		isCommand, err := command.Dispatch(&m, m.registry, tc.line)
		if err != nil {
			t.Fatalf("%s: %v", tc.line, err)
		}
		if !isCommand {
			t.Fatalf("%s not recognized as a command", tc.line)
		}

		var transcript strings.Builder
		for _, e := range m.entries {
			transcript.WriteString(e.Content + "\n")
		}
		if !strings.Contains(transcript.String(), tc.wantInTranscript) {
			t.Errorf("%s: transcript missing %q:\n%s", tc.line, tc.wantInTranscript, transcript.String())
		}

		prompt := m.session.Messages[len(m.session.Messages)-1].Content
		if m.session.Messages[len(m.session.Messages)-1].Role != "user" {
			t.Fatalf("%s: last session message is not the submitted prompt", tc.line)
		}
		assertSubmittedTurn(t, m, prompt)
	}
}

// TestRegistryContainsDevelopmentCommands pins registration of all ten
// commands in the production registry that powers dispatch, /help, and
// the palette alike.
func TestRegistryContainsDevelopmentCommands(t *testing.T) {
	reg := newRegistry()
	for _, name := range []string{
		"search", "explain", "debug", "fix", "test",
		"build", "run", "review", "refactor", "code",
	} {
		cmd, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("/%s missing from the production registry", name)
			continue
		}
		if cmd.Name() != name {
			t.Errorf("registry lookup for /%s resolved to %q", name, cmd.Name())
		}
	}
}

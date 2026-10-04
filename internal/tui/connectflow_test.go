package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/providers"
)

func TestConnectInputHandlesBracketedPaste(t *testing.T) {
	f := &connectFlow{input: newInputModal(inputStep{title: "Provider", prompt: "API key:", masked: true})}
	pasted := "sk-test!@#$%^&*()\nsecond-line"

	active, cmd := f.handleKey(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	if !active || cmd == nil {
		t.Fatalf("paste handling = active %v, cmd %v; want active and an input command", active, cmd)
	}
	if got := f.input.Value(); got != strings.ReplaceAll(pasted, "\n", " ") {
		t.Errorf("pasted API key = %q, want single-line input %q", got, strings.ReplaceAll(pasted, "\n", " "))
	}
}

func TestConnectInputPreservesClipboardCommand(t *testing.T) {
	f := &connectFlow{input: newInputModal(inputStep{title: "Provider", prompt: "API key:", masked: true})}

	active, cmd := f.handleKey(nil, tea.KeyMsg{Type: tea.KeyCtrlV})
	if !active {
		t.Fatal("Ctrl+V unexpectedly cancelled the connect flow")
	}
	if cmd == nil {
		t.Fatal("Ctrl+V clipboard command was discarded by the connect flow")
	}
}

func TestMainInputPasteDoesNotTriggerShortcuts(t *testing.T) {
	in := textinput.New()
	in.Focus()
	m := model{input: in}
	pasted := "key-with-ctrl-c\nnext line"

	got, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	updated := got.(model)
	if updated.quitting {
		t.Fatal("pasted content triggered quit")
	}
	if cmd == nil {
		t.Fatal("bracketed paste did not update the input")
	}
	if updated.input.Value() != strings.ReplaceAll(pasted, "\n", " ") {
		t.Errorf("main input = %q, want single-line input %q", updated.input.Value(), strings.ReplaceAll(pasted, "\n", " "))
	}
}

// TestConnectFlowOpenRouterUsesRegisteredEndpoint is the regression
// test for the manual /connect failure: selecting OpenRouter and typing
// only an API key must validate against the registry endpoint
// https://openrouter.ai/api/v1, never a blank URL.
func TestConnectFlowOpenRouterUsesRegisteredEndpoint(t *testing.T) {
	f := newConnectFlow()
	if !f.beginProvider("openrouter") {
		t.Fatal("OpenRouter flow should prompt for its API key")
	}
	if len(f.steps) != 1 || !f.steps[0].masked {
		t.Fatalf("steps = %+v, want one masked API-key prompt (no base URL prompt)", f.steps)
	}
	if !strings.Contains(f.steps[0].prompt, "API key") {
		t.Errorf("prompt = %q, want an API key prompt", f.steps[0].prompt)
	}

	f.steps[0].apply("sk-test-key")
	conn := f.finalize()

	want := "https://openrouter.ai/api/v1"
	if conn.Endpoint != want {
		t.Fatalf("endpoint = %q, want registered default %q (blank URL caused: unsupported protocol scheme)", conn.Endpoint, want)
	}
	if conn.ID != "openrouter" || conn.APIKey != "sk-test-key" {
		t.Errorf("connection = %+v", conn)
	}
}

// TestConnectFlowRoutersStillPromptForBaseURL guards the flows that
// must remain configurable per installation.
func TestConnectFlowRoutersStillPromptForBaseURL(t *testing.T) {
	for _, id := range []string{"9router", "omniroute"} {
		info, _ := providers.ByID(id)
		f := newConnectFlow()
		if !f.beginProvider(id) {
			t.Fatalf("%s flow should prompt for input", id)
		}
		first := f.steps[0]
		if first.prompt != "Base URL:" || first.initial != info.Endpoint {
			t.Errorf("%s first step = %q/%q, want Base URL with default %q", id, first.prompt, first.initial, info.Endpoint)
		}

		// Typing a custom base URL must win over the default.
		f.steps[0].apply("http://192.168.1.10:20128/v1/")
		if got := f.pending.Endpoint; got != "http://192.168.1.10:20128/v1" {
			t.Errorf("%s pending endpoint = %q after user override", id, got)
		}
	}
}

// TestConnectFlowOllamaUnchanged pins the local flow: endpoint prompt
// seeded from the registry, no key step.
func TestConnectFlowOllamaUnchanged(t *testing.T) {
	f := newConnectFlow()
	if !f.beginProvider("ollama") {
		t.Fatal("Ollama flow should prompt for its base URL")
	}
	if len(f.steps) != 1 || f.steps[0].masked {
		t.Fatalf("steps = %+v, want exactly one unmasked Base URL prompt", f.steps)
	}
	if f.steps[0].initial != "http://localhost:11434" {
		t.Errorf("default = %q, want http://localhost:11434", f.steps[0].initial)
	}
}

// TestConnectFlowCustomKeepsTypedURL verifies custom providers still
// accept a user-entered base URL and are not backfilled away.
func TestConnectFlowCustomKeepsTypedURL(t *testing.T) {
	f := newConnectFlow()
	f.beginProvider("__custom__")
	if len(f.steps) != 3 { // name, base URL, optional key
		t.Fatalf("custom flow steps = %d, want 3", len(f.steps))
	}
	f.steps[0].apply("My Provider!")             // name
	f.steps[1].apply("http://localhost:1234/v1") // base URL override
	f.steps[2].apply("")                         // optional key left blank
	conn := f.finalize()
	if conn.Endpoint != "http://localhost:1234/v1" {
		t.Errorf("custom endpoint = %q, want typed URL preserved", conn.Endpoint)
	}
	if conn.ID != "my-provider" {
		t.Errorf("custom ID = %q, want slug my-provider", conn.ID)
	}
}

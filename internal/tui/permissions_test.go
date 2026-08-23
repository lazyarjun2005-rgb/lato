package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/permissions"
	"lato/internal/providers"
	"lato/internal/runtime"
)

func newTestPermRequest(summary, reason string) *runtime.PermissionRequest {
	return runtime.NewPermissionRequest(
		"run_command", summary, reason, permissions.ClassHighRisk,
	)
}

func TestPermissionPromptRendersDecisionOptions(t *testing.T) {
	p := newPermPrompt(newTestPermRequest(
		"Delete directory ./build", "This operation permanently removes files.",
	))
	view := p.view(100, 30)

	for _, want := range []string{"Permission required", "./build", "[1] Allow once", "[2] Allow for task", "[3] Deny"} {
		if !strings.Contains(view, want) {
			t.Errorf("prompt view missing %q:\n%s", want, view)
		}
	}
}

func TestPermissionPromptKeysRouteDecisions(t *testing.T) {
	cases := []struct {
		key  tea.KeyMsg
		want runtime.PermissionChoice
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")}, runtime.PermissionAllowOnce},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}, runtime.PermissionAllowOnce},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}, runtime.PermissionAllowTask},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}, runtime.PermissionAllowTask},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")}, runtime.PermissionDeny},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}, runtime.PermissionDeny},
		{tea.KeyMsg{Type: tea.KeyEsc}, runtime.PermissionDeny},
	}
	for _, tc := range cases {
		req := newTestPermRequest("Run command: rm -rf build", "deletes files")
		m := model{entries: []chatEntry{}, perm: newPermPrompt(req)}
		next, _ := m.handlePermKey(tc.key)
		nm := next.(model)
		if nm.perm != nil {
			t.Fatalf("key %v: prompt stayed open", tc.key)
		}
		select {
		case got := <-req.Answer():
			if got != tc.want {
				t.Fatalf("key %v: answer = %v, want %v", tc.key, got, tc.want)
			}
		default:
			t.Fatalf("key %v: no answer delivered", tc.key)
		}
	}
}

// TestPermissionDecideAnswersAndCloses covers the deny path end to end:
// the modal closes, the waiting runtime receives the choice, and the
// transcript records what was refused (so /copy captures it).
func TestPermissionDecideAnswersAndCloses(t *testing.T) {
	req := newTestPermRequest("Run command: rm -rf ./important", "deletes files")
	m := model{entries: []chatEntry{}, perm: newPermPrompt(req)}

	next, _ := m.decide(runtime.PermissionDeny)
	nm := next.(model)
	if nm.perm != nil {
		t.Fatal("prompt stayed open after deciding")
	}

	select {
	case got := <-req.Answer():
		if got != runtime.PermissionDeny {
			t.Fatalf("answer = %v, want deny", got)
		}
	default:
		t.Fatal("decision never delivered to the waiting runtime")
	}

	found := false
	for _, e := range nm.entries {
		if strings.Contains(e.Content, "Denied") && strings.Contains(e.Content, "rm -rf ./important") {
			found = true
		}
	}
	if !found {
		t.Error("denial not recorded in transcript entries")
	}
}

func TestUIAskerFailsSafeWithoutProgram(t *testing.T) {
	a := newUIAsker() // bind() never called
	if choice := a.AskPermission(context.Background(), runtime.PermissionRequest{}); choice != runtime.PermissionDeny {
		t.Fatalf("unbound asker returned %v, want deny", choice)
	}
}

func TestFormatToolStartRedactsCredentialShapedArgs(t *testing.T) {
	got := formatToolStart(&providers.ToolCall{
		Name:      "run_command",
		Arguments: map[string]any{"command": `OPENROUTER_API_KEY=sk-abcdef1234567890 npm whoami`},
	})
	if strings.Contains(got, "sk-abcdef1234567890") {
		t.Errorf("tool start leaked the key: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("expected redaction marker in %q", got)
	}
}

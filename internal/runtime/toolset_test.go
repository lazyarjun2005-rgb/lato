package runtime

import (
	"context"
	"strings"
	"testing"

	"lato/internal/providers"
)

// --- conversationalTurn classifier -------------------------------------

func TestConversationalTurn(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		// greetings, gratitude, acknowledgments: no tools needed
		{"hi", true},
		{"Hi!", true},
		{"HELLO", true},
		{"hey there", true},
		{"thanks", true},
		{"Thank you!", true},
		{"ok", true},
		{"cool, nice", true},
		{"how are you?", true},
		{"what can you do?", true},
		{"who are you", true},
		{"good morning", true},
		{"bye", true},
		{"hi there :)", true},
		{"", true},

		// anything actionable or ambiguous keeps the full tool set
		{"fix the bug in main.go", false},
		{"list files in internal/runtime", false},
		{"explain this repository", false},
		{"what is a pointer?", false},
		{"run the tests", false},
		{"use a tool", false},
		{"create_file with content hello", false},
		{"how does the runtime work", false},
		{"that is wrong, fix it", false},
		{"search for ForQuestion", false},
	}
	for _, c := range cases {
		if got := conversationalTurn(c.text); got != c.want {
			t.Errorf("conversationalTurn(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// TestConversationalTurnsOmitTools pins the performance contract: a
// small-talk request sends zero tool definitions to the provider.
func TestConversationalTurnsOmitTools(t *testing.T) {
	provider := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "Hello!"}, {Done: true}},
	}}
	rt := newTestRuntime(provider)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range stream {
	}

	if provider.calls != 1 || len(provider.tools) != 1 {
		t.Fatalf("provider calls = %d, recorded tool sets = %d, want 1 each", provider.calls, len(provider.tools))
	}
	if len(provider.tools[0]) != 0 {
		t.Errorf("greeting sent %d tool definitions, want 0 (first: %+v)", len(provider.tools[0]), provider.tools[0])
	}
}

// TestWorkRequestsKeepAllTools verifies selective exposure never strips
// tools from a task-like request, so existing behavior is unchanged.
func TestWorkRequestsKeepAllTools(t *testing.T) {
	provider := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "done"}, {Done: true}},
	}}
	rt := newTestRuntime(provider)
	want := len(rt.manager.List())

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "fix the bug in main.go"},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range stream {
	}

	if provider.calls != 1 || len(provider.tools) != 1 {
		t.Fatalf("provider calls = %d, recorded tool sets = %d, want 1 each", provider.calls, len(provider.tools))
	}
	if len(provider.tools[0]) != want {
		t.Errorf("task request sent %d tools, want all %d", len(provider.tools[0]), want)
	}
}

// TestToolLoopKeepsItsToolSet checks that continuation turns inside one
// request keep the same tool set they started with.
func TestToolLoopKeepsItsToolSet(t *testing.T) {
	provider := &scriptedProvider{turns: testTurns()}
	rt := newTestRuntime(provider)

	events, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "use a tool"},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range events {
	}

	if provider.calls != 2 || len(provider.tools) != 2 {
		t.Fatalf("provider calls = %d, recorded tool sets = %d, want 2 each", provider.calls, len(provider.tools))
	}
	if len(provider.tools[1]) == 0 {
		t.Error("tool-loop continuation turn lost its tool definitions")
	}
}

// --- normalization helper ----------------------------------------------

func TestNormalizeTurnText(t *testing.T) {
	cases := map[string]string{
		"  Hello,   World! ": "hello world",
		"what's up?":         "whats up",
		"HI :)":              "hi",
	}
	for in, want := range cases {
		if got := normalizeTurnText(in); got != want {
			t.Errorf("normalizeTurnText(%q) = %q, want %q", in, got, want)
		}
	}
	if strings.Contains(normalizeTurnText("test\tpunctuation\n"), "\n") {
		t.Error("newlines must be normalized to spaces")
	}
}

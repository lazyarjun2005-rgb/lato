package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/providers"
	"lato/internal/workspace"
)

// TestCodeQuestionInjectsSourceEvidence pins the M8 contract end to
// end: a code question's system prompt must contain actual source
// excerpts retrieved from the target workspace, not just metadata.
func TestCodeQuestionInjectsSourceEvidence(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/acme\n\ngo 1.26\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(
		"package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"AcmeLauncher\")\n}\n"), 0o644)

	provider := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "answered"}, {Done: true}},
	}}
	rt := newTestRuntime(provider)
	rt.workspace = workspace.DiscoverDir(dir)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "How does the main function work?"},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range stream {
	}

	if provider.calls < 1 || len(provider.messages) == 0 {
		t.Fatal("provider never received messages")
	}
	sys := provider.messages[0][0]
	if sys.Role != providers.SystemRole {
		t.Fatalf("first message role = %q, want system", sys.Role)
	}
	for _, want := range []string{
		"Source evidence", // evidence block header
		"main.go",         // the relevant file
		"func main",       // actual source excerpt
		"AcmeLauncher",    // content from inside the file
		"Declarations:",   // symbol summary
	} {
		if !strings.Contains(sys.Content, want) {
			t.Errorf("system prompt missing %q:\n%s", want, sys.Content)
		}
	}
}

// TestUnrelatedChatStaysClean verifies non-code conversation does not
// pay for retrieval or receive evidence blocks.
func TestUnrelatedChatStaysClean(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	provider := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "ok"}, {Done: true}},
	}}
	rt := newTestRuntime(provider)
	rt.workspace = workspace.DiscoverDir(dir)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Write a haiku about coffee"},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range stream {
	}

	if provider.calls < 1 || len(provider.messages) == 0 {
		t.Fatal("provider never received messages")
	}
	sys := provider.messages[0][0]
	for _, banned := range []string{"Source evidence", "Repository:", "Declarations:"} {
		if strings.Contains(sys.Content, banned) {
			t.Errorf("unrelated chat received %q in the system prompt:\n%s", banned, sys.Content)
		}
	}
}

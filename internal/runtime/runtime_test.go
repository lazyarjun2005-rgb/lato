package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/agent"
	"lato/internal/index"
	"lato/internal/providers"
	"lato/internal/tools"
	"lato/internal/workspace"
)

type scriptedProvider struct {
	turns    [][]providers.StreamEvent
	calls    int
	messages [][]providers.Message
	tools    [][]tools.Definition
}

func (p *scriptedProvider) StreamChat(_ context.Context, messages []providers.Message, defs []tools.Definition) (<-chan providers.StreamEvent, error) {
	p.messages = append(p.messages, append([]providers.Message(nil), messages...))
	p.tools = append(p.tools, append([]tools.Definition(nil), defs...))

	// Exhausted scripts yield a benign empty turn instead of panicking:
	// tests that intentionally overrun their script still terminate.
	if p.calls >= len(p.turns) {
		events := make(chan providers.StreamEvent, 1)
		events <- providers.StreamEvent{Done: true}
		close(events)
		return events, nil
	}

	turn := p.turns[p.calls]
	p.calls++

	events := make(chan providers.StreamEvent, len(turn))
	for _, event := range turn {
		events <- event
	}
	close(events)
	return events, nil
}

func (p *scriptedProvider) ListModels(context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "Returns the supplied value." }
func (echoTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (echoTool) Execute(_ context.Context, args map[string]any) (tools.Result, error) {
	return tools.Result{Content: args["value"].(string)}, nil
}

func newTestRuntime(p providers.ModelProvider) *Runtime {
	manager := tools.NewManager(tools.NewRegistry())
	if err := manager.Register(echoTool{}); err != nil {
		panic(err)
	}

	return &Runtime{
		provider: p,
		agent:    agent.New("test", "system", ""),
		manager:  manager,
	}
}

func testTurns() [][]providers.StreamEvent {
	return [][]providers.StreamEvent{
		{
			{Text: "Checking that now. "},
			{ToolCalls: []providers.ToolCall{{
				ID:        "call-1",
				Name:      "echo",
				Arguments: map[string]any{"value": "tool output"},
			}}},
			{Done: true},
		},
		{
			{Text: "The tool said: tool output"},
			{Done: true},
		},
	}
}

func TestStreamChatExecutesToolsAndContinuesGeneration(t *testing.T) {
	provider := &scriptedProvider{turns: testTurns()}
	runtime := newTestRuntime(provider)

	events, err := runtime.StreamChat(context.Background(), []providers.Message{{Role: providers.UserRole, Content: "use a tool"}})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}

	var types []EventType
	var final *providers.Response
	var toolResult *ToolResult
	for event := range events {
		types = append(types, event.Type)
		if event.Type == EventDone {
			final = event.Response
		}
		if event.Type == EventToolFinish {
			toolResult = event.ToolResult
		}
	}

	wantTypes := []EventType{
		EventThinking,
		EventText,
		EventToolStart,
		EventToolFinish,
		EventThinking,
		EventText,
		EventDone,
	}
	if len(types) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d (%v)", len(types), len(wantTypes), types)
	}
	for i, want := range wantTypes {
		if types[i] != want {
			t.Errorf("event %d type = %v, want %v", i, types[i], want)
		}
	}

	if final == nil || final.Content != "The tool said: tool output" {
		t.Fatalf("final response = %#v, want final tool-informed response", final)
	}
	if toolResult == nil || !toolResult.Success || toolResult.Arguments["value"] != "tool output" {
		t.Errorf("tool result = %#v, want successful result with tool arguments", toolResult)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}

	continuedMessages := provider.messages[1]
	if got := continuedMessages[len(continuedMessages)-2]; got.Role != providers.AssistantRole || len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "call-1" {
		t.Errorf("assistant tool-call message = %#v, want tool call with its ID", got)
	}
	if got := continuedMessages[len(continuedMessages)-1]; got.Role != providers.ToolRole || got.ToolCallID != "call-1" || got.Content != "tool output" {
		t.Errorf("tool result message = %#v, want matching tool result", got)
	}
}

func TestRunUsesStreamingAgentLoop(t *testing.T) {
	provider := &scriptedProvider{turns: testTurns()}
	runtime := newTestRuntime(provider)

	response, err := runtime.Run([]providers.Message{{Role: providers.UserRole, Content: "use a tool"}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Content != "The tool said: tool output" {
		t.Errorf("response.Content = %q, want final tool-informed response", response.Content)
	}
	if provider.calls != 2 {
		t.Errorf("provider calls = %d, want 2", provider.calls)
	}
}

// newContextTestRuntime builds a Runtime pointed at a real workspace
// directory containing a go.mod and a README, so context injection can
// be exercised end to end.
func newContextTestRuntime(t *testing.T, wsDir string) (*Runtime, *scriptedProvider) {
	t.Helper()

	provider := &scriptedProvider{turns: testTurns()}
	rt := newTestRuntime(provider)

	ws := workspace.DiscoverDir(wsDir)
	rt.workspace = ws
	return rt, provider
}

func TestStreamChatInjectsContextForRepositoryQuestion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/acme/demo\n\ngo 1.26\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Demo Repo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)

	rt, provider := newContextTestRuntime(t, dir)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Explain this repository"},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	// Drain the stream so the background goroutine completes and the
	// provider records which messages it saw.
	for range stream {
	}

	if provider.calls < 1 || len(provider.messages) == 0 {
		t.Fatal("no messages captured by provider")
	}
	sysMsg := provider.messages[0][0]
	if sysMsg.Role != providers.SystemRole {
		t.Fatalf("first message role = %q, want system", sysMsg.Role)
	}
	for _, want := range []string{"Repository:", "Demo", "README Summary:", "# Demo Repo", "Language:", "Go"} {
		if !strings.Contains(sysMsg.Content, want) {
			t.Errorf("system prompt missing %q:\n%s", want, sysMsg.Content)
		}
	}
}

func TestStreamChatDoesNotInjectForUnrelatedChat(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n"), 0o644)

	rt, provider := newContextTestRuntime(t, dir)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Write a haiku about coffee"},
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range stream {
	}

	if provider.calls < 1 || len(provider.messages) == 0 {
		t.Fatal("no messages captured by provider")
	}
	sysMsg := provider.messages[0][0]
	if strings.Contains(sysMsg.Content, "Repository:") {
		t.Errorf("system prompt should not contain repository context:\n%s", sysMsg.Content)
	}
}

func TestStreamChatDoesNotInjectForEmptyMessages(t *testing.T) {
	dir := t.TempDir()
	rt, provider := newContextTestRuntime(t, dir)

	stream, err := rt.StreamChat(context.Background(), nil)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range stream {
	}

	if provider.calls < 1 || len(provider.messages) == 0 {
		t.Fatal("no messages captured by provider")
	}
	sysMsg := provider.messages[0][0]
	if strings.Contains(sysMsg.Content, "Repository:") {
		t.Errorf("system prompt should not contain repository context for empty messages:\n%s", sysMsg.Content)
	}
}

// TestIndexUsesWorkspaceRootNotLatoSource verifies the M4.5 requirement:
// a runtime pointed at an arbitrary workspace indexes THAT workspace, not
// whichever directory the test binary happens to live in.
func TestIndexUsesWorkspaceRootNotLatoSource(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/target\n"), 0o644)

	rt := newTestRuntime(nil)
	rt.workspace = workspace.DiscoverDir(dir)

	idx := rt.Index()
	if idx.Info.Root != dir {
		t.Errorf("index root = %q, want %q (must be the target workspace, not Lato source)", idx.Info.Root, dir)
	}
	f, ok := idx.Lookup("main.go")
	if !ok {
		t.Fatal("main.go should be indexed from the target workspace")
	}
	if !strings.Contains(f.Body, "package main") {
		t.Errorf("indexed body = %q, expected target-workspace content", f.Body)
	}
	// The Lato source tree must not leak into the target index.
	if _, leak := idx.Lookup("internal/runtime/runtime.go"); leak {
		t.Error("index leaked Lato source files; must only see the target workspace")
	}
}

// TestIndexCachedAcrossCalls verifies the lazy build-once caching.
func TestIndexCachedAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644)

	rt := newTestRuntime(nil)
	rt.workspace = workspace.DiscoverDir(dir)

	first := rt.Index()
	second := rt.Index()
	if first != second {
		t.Fatal("Index() must return the cached instance")
	}
	if first.Info.Root != dir {
		t.Errorf("index root = %q, want %q", first.Info.Root, dir)
	}
}

// TestSearchFindsContentInTargetWorkspace verifies search delegates to
// the cached index of the target workspace.
func TestSearchFindsContentInTargetWorkspace(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package p\n\n// FindTheNeedle here\nfunc Work() {}\n"), 0o644)

	rt := newTestRuntime(nil)
	rt.workspace = workspace.DiscoverDir(dir)

	res, err := rt.Search(index.Search{Query: "FindTheNeedle", Max: 10, Contents: true, Symbols: true})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if res.Count == 0 {
		t.Fatal("expected a content match in the target workspace")
	}
	if res.Matches[0].Path != "helper.go" {
		t.Errorf("match path = %q, want helper.go", res.Matches[0].Path)
	}
	if res.Matches[0].Line != 3 { // "package p", blank, then the needle comment
		t.Errorf("match line = %d, want 3", res.Matches[0].Line)
	}
}

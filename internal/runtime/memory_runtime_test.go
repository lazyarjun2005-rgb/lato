package runtime

import (
	"context"
	"strings"
	"testing"

	"lato/internal/memory"
	"lato/internal/providers"
	"lato/internal/workspace"
)

// seedMemory pre-populates the store for a workspace root.
func seedMemory(t *testing.T, root string, content, category string, kind memory.Kind) memory.Entry {
	t.Helper()
	s, err := memory.Load(memory.ProjectID(root))
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Add(content, category, kind)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func runtimeWithWorkspace(t *testing.T) (*Runtime, *scriptedProvider, string) {
	t.Helper()
	isolateUserConfig(t)
	root := t.TempDir()
	p := &scriptedProvider{turns: [][]providers.StreamEvent{{{Text: "ok"}, {Done: true}}}}
	rt := newTestRuntime(p)
	rt.workspace = workspace.DiscoverDir(root)
	return rt, p, root
}

func TestRelevantMemoryInjectedIntoPrompt(t *testing.T) {
	rt, p, root := runtimeWithWorkspace(t)
	seedMemory(t, root, "Tests run with go test ./...", "command", memory.KindUser)
	seedMemory(t, root, "PostgreSQL is the production database.", "technology", memory.KindUser)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "How do we run the tests in this project?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	sys := p.messages[0][0].Content
	if !strings.Contains(sys, "## Project memory") {
		t.Fatalf("relevant memory missing from prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "go test ./...") {
		t.Errorf("prompt lacks the relevant fact:\n%s", sys)
	}
	if strings.Contains(sys, "PostgreSQL") {
		t.Errorf("irrelevant fact was injected too:\n%s", sys)
	}
}

func TestIrrelevantGoalGetsNoMemory(t *testing.T) {
	rt, p, root := runtimeWithWorkspace(t)
	seedMemory(t, root, "PostgreSQL is the production database.", "technology", memory.KindUser)

	stream, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Write a haiku about coffee"},
	})
	for range stream {
	}

	if strings.Contains(p.messages[0][0].Content, "Project memory") {
		t.Errorf("irrelevant request received memory:\n%s", p.messages[0][0].Content)
	}
}

func TestSimpleGreetingStaysLightweight(t *testing.T) {
	rt, p, root := runtimeWithWorkspace(t)
	seedMemory(t, root, "PostgreSQL is the production database.", "technology", memory.KindUser)

	stream, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "hi"},
	})
	sawMemoryEvent := false
	for e := range stream {
		if e.Type == EventMemory {
			sawMemoryEvent = true
		}
	}
	if sawMemoryEvent {
		t.Error("greeting produced a memory-injection indication")
	}
	if len(p.messages) == 0 || strings.Contains(p.messages[0][0].Content, "Project memory") {
		t.Error("greeting received memory injection")
	}
}

// TestMemoryAvailableToPlanning pins M10 integration: complex goals get
// both the task protocol and their relevant project memory.
func TestMemoryAvailableToPlanning(t *testing.T) {
	rt, p, root := runtimeWithWorkspace(t)
	seedMemory(t, root, "The API lives under internal/api and uses chi.", "structure", memory.KindUser)

	stream, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Add a health endpoint to the api, test it, and fix any errors."},
	})
	for range stream {
	}

	sys := p.messages[0][0].Content
	if !strings.Contains(sys, "Multi-step task protocol") {
		t.Error("task directive missing")
	}
	if !strings.Contains(sys, "uses chi") {
		t.Errorf("planner did not receive relevant memory:\n%s", sys)
	}
}

// TestMemoryToolPersistsDiscovery covers the agent flow end to end: the
// model stores a durable discovery with the memory tool; the fact
// survives a reload (restart).
func TestMemoryToolPersistsDiscovery(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()

	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		{ // the agent decides a discovery is durable
			{ToolCalls: []providers.ToolCall{{
				ID:   "c1",
				Name: "remember_project_fact",
				Arguments: map[string]any{
					"content":  "This project uses chi as its HTTP router.",
					"category": "architecture",
				},
			}}},
			{Done: true},
		},
		{
			{Text: "Task complete: noted the router choice."},
			{Done: true},
		},
	}}
	rt := newTestRuntime(p)
	rt.workspace = workspace.DiscoverDir(root)
	if err := memory.Register(rt.manager, rt); err != nil {
		t.Fatal(err)
	}

	response, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "add an endpoint"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Content, "Task complete") {
		t.Errorf("final = %q", response.Content)
	}

	// Reload from disk as a fresh launch would.
	reloaded, err := memory.Load(memory.ProjectID(root))
	if err != nil {
		t.Fatal(err)
	}
	list := reloaded.List()
	if len(list) != 1 || !strings.Contains(list[0].Content, "chi") || list[0].Kind != memory.KindDiscovered {
		t.Fatalf("discovered fact not persisted: %+v", list)
	}
}

func TestMemoryToolsRejectSecrets(t *testing.T) {
	rt, _, _ := runtimeWithWorkspace(t)
	if err := memory.Register(rt.manager, rt); err != nil {
		t.Fatal(err)
	}

	res, err := rt.manager.Execute(context.Background(), "remember_project_fact", map[string]any{
		"content": "the openrouter key is sk-or-v1-abcdefabcdefabcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("secret stored as memory: %+v", res)
	}
	if strings.Contains(res.Content, "sk-or-v1") {
		t.Errorf("tool result echoed the secret: %q", res.Content)
	}
	if len(rt.Memory().List()) != 0 {
		t.Error("secret persisted despite rejection")
	}
}

// TestMemoryIsolatedAcrossWorkspaces proves two runtimes pointed at two
// different roots never share project memory.
func TestMemoryIsolatedAcrossWorkspaces(t *testing.T) {
	isolateUserConfig(t)
	rootA := t.TempDir()
	rootB := t.TempDir()

	seedMemory(t, rootA, "alpha uses module pattern A", "structure", memory.KindUser)

	pB := &scriptedProvider{turns: [][]providers.StreamEvent{{{Text: "ok"}, {Done: true}}}}
	rtB := newTestRuntime(pB)
	rtB.workspace = workspace.DiscoverDir(rootB)

	stream, _ := rtB.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "tell me about the module pattern here"},
	})
	for range stream {
	}
	if len(pB.messages) == 0 || strings.Contains(pB.messages[0][0].Content, "module pattern A") {
		t.Error("project B's prompt leaked project A memory")
	}
}

// TestEventMemoryEmittedWithCount verifies the TUI indication signal.
func TestEventMemoryEmittedWithCount(t *testing.T) {
	rt, _, root := runtimeWithWorkspace(t)
	seedMemory(t, root, "Tests run with go test ./...", "command", memory.KindUser)

	stream, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "how do we run the tests here?"},
	})
	count := -1
	for e := range stream {
		if e.Type == EventMemory {
			count = e.Count
		}
	}
	if count < 1 {
		t.Errorf("EventMemory count = %d, want >= 1", count)
	}
}

package runtime

import (
	"context"
	"strings"
	"testing"

	"lato/internal/memory"
	"lato/internal/providers"
	"lato/internal/task"
	"lato/internal/tools"
	"lato/internal/workspace"
)

func taskStoreForRoot(root string) (*task.Store, error) {
	return task.Load(memory.ProjectID(root))
}

func taskRuntime(t *testing.T) (*Runtime, *scriptedProvider, string) {
	t.Helper()
	isolateUserConfig(t)
	root := t.TempDir()
	p := &scriptedProvider{turns: [][]providers.StreamEvent{{{Text: "ok"}, {Done: true}}}}
	rt := newTestRuntime(p)
	rt.workspace = workspace.DiscoverDir(root)
	return rt, p, root
}

func TestSimpleRequestCreatesNoTask(t *testing.T) {
	rt, _, _ := taskRuntime(t)

	stream, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "What is 2+2?"},
	})
	for range stream {
	}
	if len(rt.TaskStore().All()) != 0 {
		t.Fatalf("simple message created tasks: %+v", rt.TaskStore().All())
	}
}

// stubEditTool accepts any args and succeeds; it exists so the
// checkpoint test exercises the real edit_file observation path.
type stubEditTool struct{}

func (stubEditTool) Name() string        { return "edit_file" }
func (stubEditTool) Description() string { return "stub editor" }
func (stubEditTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (stubEditTool) Execute(_ context.Context, _ map[string]any) (tools.Result, error) {
	return tools.Result{Content: "edited"}, nil
}

func TestComplexTaskCreatesAndCheckpoints(t *testing.T) {
	rt, p, root := taskRuntime(t)
	if err := rt.manager.Register(stubEditTool{}); err != nil {
		t.Fatal(err)
	}
	p.turns = [][]providers.StreamEvent{
		{ // plan + first tool call
			{Text: "1. Inspect auth code\n2. Implement login handler\n3. Run tests"},
			{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "edit_file",
				Arguments: map[string]any{"path": "internal/auth/login.go", "old_text": "a", "new_text": "b"}}}},
			{Done: true},
		},
		{
			{Text: "Task complete: authentication added."},
			{Done: true},
		},
	}

	response, err := rt.Run([]providers.Message{
		{Role: providers.UserRole, Content: "Add authentication to this project, test it, and fix any errors."},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Structured checkpoint persisted with parsed plan and file change.
	reloaded, err := taskStoreForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	all := reloaded.All()
	if len(all) != 1 {
		t.Fatalf("tasks persisted = %d, want 1", len(all))
	}
	tk := all[0]
	if tk.Status != task.StatusCompleted {
		t.Errorf("status = %q, want completed", tk.Status)
	}
	if len(tk.Steps) != 3 || tk.Steps[0].Title != "Inspect auth code" {
		t.Errorf("plan steps = %+v", tk.Steps)
	}
	if len(tk.FilesChanged) != 1 || tk.FilesChanged[0] != "internal/auth/login.go" {
		t.Errorf("files changed = %v", tk.FilesChanged)
	}

	// Compact status preview appended to the final answer, stating the
	// outcome in the unified schema.
	if !strings.Contains(response.Content, "authentication added.") ||
		!strings.Contains(response.Content, "Status: completed") {
		t.Errorf("preview missing from final content:\n%s", response.Content)
	}
}

func TestBudgetStopKeepsTaskResumable(t *testing.T) {
	rt, _, root := taskRuntime(t)
	rt.provider = &varyingProvider{}

	response, err := rt.Run([]providers.Message{
		{Role: providers.UserRole, Content: "Refactor everything repeatedly and make sure it never ends."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Content, "Execution budget reached") ||
		!strings.Contains(response.Content, "Status: paused") {
		t.Errorf("budget stop content:\n%s", response.Content)
	}
	reloaded, _ := taskStoreForRoot(root)
	rs := reloaded.Resumable()
	if len(rs) != 1 || rs[0].Status != task.StatusPaused {
		t.Fatalf("resumable after budget = %+v", rs)
	}
}

// failingMidwayProvider errors on the second turn, simulating a
// provider/process interruption mid-task.
type interruptingProvider struct {
	calls int
	err   error
}

func (p *interruptingProvider) ListModels(context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

func (p *interruptingProvider) StreamChat(_ context.Context, msgs []providers.Message, _ []tools.Definition) (<-chan providers.StreamEvent, error) {
	p.calls++
	events := make(chan providers.StreamEvent, 2)
	switch p.calls {
	case 1:
		events <- providers.StreamEvent{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "echo",
			Arguments: map[string]any{"value": "step one"}}}}
	default:
		events <- providers.StreamEvent{Err: context.Canceled}
	}
	close(events)
	return events, nil
}

func TestInterruptionLeavesTaskResumable(t *testing.T) {
	rt, _, root := taskRuntime(t)
	p := &interruptingProvider{}
	rt.provider = p

	stream, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Implement feature X, test it, and fix any errors."},
	})
	for e := range stream {
		if e.Type == EventError && e.Err == nil {
			t.Fatal("expected an error event")
		}
	}

	reloaded, _ := taskStoreForRoot(root)
	rs := reloaded.Resumable()
	if len(rs) != 1 || !strings.Contains(rs[0].LastAction, "echo") {
		t.Fatalf("interrupted task lost or uncheckpointed: %+v", rs)
	}
}

// --- resume ----------------------------------------------------------------

func TestResumeContinuesThroughM10WithoutReplay(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()

	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		{ // original run: plan + one edit, then the process "dies"
			{Text: "1. Inspect handler\n2. Fix validation\n3. Run tests"},
			{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "edit_file",
				Arguments: map[string]any{"path": "internal/api/h.go", "old_text": "x", "new_text": "y"}}}},
			{Done: true},
		},
		{ // second turn of original run dies
			{Err: context.Canceled},
		},
	}}
	rt := newTestRuntime(p)
	rt.workspace = workspace.DiscoverDir(root)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Fix the validation bug in the api, test it, and verify."},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	resumed := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "Re-inspecting the current state first."},
			{ToolCalls: []providers.ToolCall{{ID: "r1", Name: "echo",
				Arguments: map[string]any{"value": "internal/api/h.go re-checked"}}}},
			{Done: true}},
		{{Text: "[x] 3. Run tests\nTask complete: validation verified against current repository state."}, {Done: true}},
	}}
	rt2 := newTestRuntime(resumed)
	rt2.workspace = workspace.DiscoverDir(root)

	response, err := rt2.Run([]providers.Message{{Role: providers.UserRole, Content: "continue where we left off"}})
	if err != nil {
		t.Fatal(err)
	}

	// The resume request carries saved state in the user message...
	var sys strings.Builder
	for _, m := range resumed.messages[0] {
		sys.WriteString(m.Content + "\n")
	}
	prompt := sys.String()
	for _, want := range []string{"Fix the validation bug", "[pending]", "Last action:", "re-inspect"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("resume prompt missing %q:\n%s", want, prompt)
		}
	}
	// ...but did NOT replay the old tool call arguments.
	if strings.Contains(prompt, `"old_text"`) || strings.Contains(prompt, `new_text`) {
		t.Errorf("resume prompt replays old tool calls:\n%s", prompt)
	}
	// Same task record reused and completed; the unified preview states
	// the outcome explicitly, and the model-reported "[x] 3." marker is
	// reflected in persisted progress.
	reloaded, _ := taskStoreForRoot(root)
	all := reloaded.All()
	if len(all) != 1 || all[0].Status != task.StatusCompleted {
		t.Fatalf("resumed task state = %+v", all)
	}
	if done, total := all[0].Progress(); done != 1 || total != 3 {
		t.Errorf("persisted progress = %d/%d, want 1/3", done, total)
	}
	if !strings.Contains(response.Content, "Status: completed") {
		t.Errorf("final = %q", response.Content)
	}
}

// TestResumeAmbiguityNeverGuesses verifies multiple resumable tasks are
// listed rather than silently picking one.
func TestResumeAmbiguityNeverGuesses(t *testing.T) {
	rt, p, _ := taskRuntime(t)
	store := rt.TaskStore()
	t1, _ := store.Start("task alpha")
	t2, _ := store.Start("task beta")
	_ = t1
	_ = t2

	var sawListing string
	events, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "continue where we left off"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for e := range events {
		if e.Type == EventText {
			sawListing += e.Text
		}
	}
	if !strings.Contains(sawListing, "resumable tasks") || !strings.Contains(sawListing, "task resume") {
		t.Errorf("ambiguity handling output:\n%s", sawListing)
	}
	if p.calls != 0 {
		t.Errorf("model was invoked during ambiguity resolution (%d turns)", p.calls)
	}
}

// TestNoResumableTaskMessage covers the empty case for natural language.
func TestNoResumableTaskMessage(t *testing.T) {
	rt, p, _ := taskRuntime(t)
	var saw string
	events, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "continue where we left off"},
	})
	for e := range events {
		if e.Type == EventText {
			saw += e.Text
		}
	}
	if !strings.Contains(saw, "No resumable task") {
		t.Errorf("output = %q", saw)
	}
	if p.calls != 0 {
		t.Error("model invoked without a resumable task")
	}
}

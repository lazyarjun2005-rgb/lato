package runtime

import (
	"context"
	"strings"
	"testing"

	"lato/internal/providers"
	"lato/internal/task"
	"lato/internal/tools"
)

// failingRunTool stands in for run_command and always fails, so the
// task tracker records a failed verification.
type failingRunTool struct{}

func (failingRunTool) Name() string        { return "run_command" }
func (failingRunTool) Description() string { return "stub verifier that always fails" }
func (failingRunTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (failingRunTool) Execute(_ context.Context, _ map[string]any) (tools.Result, error) {
	return tools.Result{Content: "FAIL — 1 test failed", IsError: true}, nil
}

// finishScenario drives a two-turn complex task (one edit, then a final
// answer) and returns the final response content plus the persisted
// task status.
func finishScenario(t *testing.T, finalText string) (string, task.Status) {
	t.Helper()
	rt, p, root := taskRuntime(t)
	if err := rt.manager.Register(stubEditTool{}); err != nil {
		t.Fatal(err)
	}
	p.turns = [][]providers.StreamEvent{
		{
			{Text: "1. Inspect code\n2. Implement change\n3. Run tests"},
			{ToolCalls: []providers.ToolCall{{
				ID: "c1", Name: "edit_file",
				Arguments: map[string]any{"path": "main.go", "old_text": "a", "new_text": "b"},
			}}},
			{Done: true},
		},
		{{Text: finalText}, {Done: true}},
	}

	response, err := rt.Run([]providers.Message{
		{Role: providers.UserRole, Content: "Add a validation helper to this project, write tests, run the tests, and fix any failures."},
	})
	if err != nil {
		t.Fatal(err)
	}

	store, err := taskStoreForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	all := store.All()
	if len(all) != 1 {
		t.Fatalf("persisted tasks = %d, want 1", len(all))
	}
	return response.Content, all[0].Status
}

// TestBlockedMarkerNeverCompletes pins the M15 honesty rule: when the
// model ends with its visible "Task blocked:" marker, the persisted task
// is blocked — not completed — and stays resumable.
func TestBlockedMarkerNeverCompletes(t *testing.T) {
	content, status := finishScenario(t,
		"Task blocked: the config format is ambiguous without user input.")
	if status != task.StatusBlocked {
		t.Errorf("status = %q, want blocked; content:\n%s", status, content)
	}
	if !strings.Contains(content, "Status: blocked") {
		t.Errorf("preview missing blocked status:\n%s", content)
	}
}

// TestUnexecutedComplexTaskStaysResumable pins the third honesty rule:
// a complex request whose model never actually ran a tool, verified
// nothing, and left plan steps open is paused — not completed.
func TestUnexecutedComplexTaskStaysResumable(t *testing.T) {
	rt, p, root := taskRuntime(t)
	p.turns = [][]providers.StreamEvent{
		{{
			Text: "1. Inspect code\n2. Implement change\n3. Run tests\nI have a clear plan and will start now.",
			Done: true,
		}},
	}

	response, err := rt.Run([]providers.Message{
		{Role: providers.UserRole, Content: "Add a validation helper to this project, write tests, run the tests, and fix any failures."},
	})
	if err != nil {
		t.Fatal(err)
	}

	store, err := taskStoreForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	all := store.All()
	if len(all) != 1 {
		t.Fatalf("persisted tasks = %d, want 1", len(all))
	}
	if all[0].Status != task.StatusPaused {
		t.Errorf("status = %q, want paused; content:\n%s", all[0].Status, response.Content)
	}
	if strings.Contains(response.Content, "Status: completed") {
		t.Errorf("unexecuted work reported as completed:\n%s", response.Content)
	}
}

// TestFailedVerificationNeverCompletes pins the other half of the rule:
// a failing verification recorded before the final answer downgrades
// completion to blocked, so Lato never claims success after failed
// tests.
func TestFailedVerificationNeverCompletes(t *testing.T) {
	rt, p, root := taskRuntime(t)
	if err := rt.manager.Register(stubEditTool{}); err != nil {
		t.Fatal(err)
	}
	if err := rt.manager.Register(failingRunTool{}); err != nil {
		t.Fatal(err)
	}
	p.turns = [][]providers.StreamEvent{
		{
			{Text: "1. Inspect code\n2. Implement change\n3. Run tests"},
			{ToolCalls: []providers.ToolCall{{
				ID: "c1", Name: "edit_file",
				Arguments: map[string]any{"path": "main.go", "old_text": "a", "new_text": "b"},
			}}},
			{Done: true},
		},
		{
			{ToolCalls: []providers.ToolCall{{ID: "c2", Name: "run_command", Arguments: map[string]any{"command": "go test ./..."}}}},
			{Done: true},
		},
		{{Text: "Task complete: everything works."}, {Done: true}},
	}

	response, err := rt.Run([]providers.Message{
		{Role: providers.UserRole, Content: "Add a validation helper to this project, write tests, run the tests, and fix any failures."},
	})
	if err != nil {
		t.Fatal(err)
	}

	all, _ := taskStoreForRoot(root)
	got := all.All()
	if len(got) != 1 {
		t.Fatalf("persisted tasks = %d, want 1", len(got))
	}
	if got[0].Status != task.StatusBlocked {
		t.Errorf("status = %q, want blocked after failed verification:\n%s", got[0].Status, response.Content)
	}
	if strings.Contains(response.Content, "Status: completed") {
		t.Errorf("failed verification reported as completed:\n%s", response.Content)
	}
	if !strings.Contains(response.Content, "Status: blocked") {
		t.Errorf("preview missing blocked status:\n%s", response.Content)
	}
}

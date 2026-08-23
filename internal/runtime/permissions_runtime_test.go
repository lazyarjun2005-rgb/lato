// Permission-gate integration tests (M13). These exercise the single
// enforcement point inside the M10 loop: allowed actions continue the
// loop, denied actions return a structured result to the model, task
// approvals stay scoped, and a fresh process (or /permissions reset)
// never inherits old grants.
package runtime

import (
	"context"
	"strings"
	"testing"

	"lato/internal/permissions"
	"lato/internal/providers"
	"lato/internal/task"
	"lato/internal/tools"
	"lato/internal/workspace"
)

// recordingTool is a controllable stand-in registered under REAL tool
// names so classification exercises production tables.
type recordingTool struct {
	name    string
	calls   int
	content string
}

func (t *recordingTool) Name() string        { return t.name }
func (t *recordingTool) Description() string { return "recording stub" }
func (t *recordingTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (t *recordingTool) Execute(_ context.Context, _ map[string]any) (tools.Result, error) {
	t.calls++
	return tools.Result{Content: "executed"}, nil
}

// scriptedAsker answers confirmation prompts from a fixed sequence;
// prompts are recorded for assertions. Running out of answers denies —
// mirroring fail-safe behavior.
type scriptedAsker struct {
	choices []PermissionChoice
	asked   []PermissionRequest
}

func (a *scriptedAsker) AskPermission(_ context.Context, req PermissionRequest) PermissionChoice {
	a.asked = append(a.asked, req)
	if len(a.choices) == 0 {
		return PermissionDeny
	}
	c := a.choices[0]
	a.choices = a.choices[1:]
	return c
}

func permRuntime(t *testing.T, p *scriptedProvider) (*Runtime, string) {
	t.Helper()
	isolateUserConfig(t)
	root := t.TempDir()
	rt := newTestRuntime(p)
	rt.workspace = workspace.DiscoverDir(root)
	rt.perms = permissions.NewPolicy(root)
	return rt, root
}

func callTurn(id, tool string, args map[string]any) []providers.StreamEvent {
	return []providers.StreamEvent{
		{ToolCalls: []providers.ToolCall{{ID: id, Name: tool, Arguments: args}}},
		{Done: true},
	}
}

func drain(t *testing.T, stream <-chan Event) (toolResults []ToolResult, final string) {
	t.Helper()
	for e := range stream {
		switch e.Type {
		case EventToolFinish:
			toolResults = append(toolResults, *e.ToolResult)
		case EventDone:
			if e.Response != nil {
				final = e.Response.Content
			}
		}
	}
	return toolResults, final
}

func TestReadOnlyAndWorkspaceWritesRunAutomatically(t *testing.T) {
	readTool := &recordingTool{name: "read_repo_file"}
	writeTool := &recordingTool{name: "create_file"}

	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "read_repo_file", map[string]any{"path": "main.go"}),
		callTurn("c2", "create_file", map[string]any{"path": "src/new.go", "content": "package src"}),
		{{Text: "done"}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(readTool); err != nil {
		t.Fatal(err)
	}
	if err := rt.manager.Register(writeTool); err != nil {
		t.Fatal(err)
	}

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "inspect and add a helper"},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, final := drain(t, stream)

	if readTool.calls != 1 || writeTool.calls != 1 {
		t.Fatalf("read calls=%d write calls=%d, want 1/1", readTool.calls, writeTool.calls)
	}
	if len(results) != 2 || results[0].IsError || results[1].IsError {
		t.Fatalf("unexpected tool results: %+v", results)
	}
	if final == "" {
		t.Fatal("loop did not continue to a final answer")
	}
}

func TestDeniedActionNeverExecutesAndLoopContinues(t *testing.T) {
	danger := &recordingTool{name: "run_command"}

	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "run_command", map[string]any{"command": "rm -rf ./important"}),
		{{Text: "Understood; I will not delete it."}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(danger); err != nil {
		t.Fatal(err)
	}

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "delete the important directory"},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, final := drain(t, stream)

	if danger.calls != 0 {
		t.Fatalf("denied action executed %d times", danger.calls)
	}
	if len(results) != 1 || !results[0].IsError {
		t.Fatalf("expected one error result, got %+v", results)
	}
	if !strings.Contains(results[0].Content, "Permission denied") {
		t.Errorf("refusal content:\n%s", results[0].Content)
	}
	// The model observes the denial as a normal tool message...
	lastTurn := p.messages[len(p.messages)-1]
	last := lastTurn[len(lastTurn)-1]
	if last.Role != providers.ToolRole || !strings.Contains(last.Content, "NOT executed") {
		t.Errorf("model did not receive the refusal:\n%+v", last)
	}
	// ...and the SAME loop continues to its next turn.
	if !strings.Contains(final, "will not delete") {
		t.Errorf("loop did not continue after denial; final=%q", final)
	}
}

func TestUnknownToolFailsClosedWithoutAsker(t *testing.T) {
	mystery := &recordingTool{name: "format_entire_disk"}
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "format_entire_disk", nil),
		{{Text: "ok"}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(mystery); err != nil {
		t.Fatal(err)
	}

	results, _ := drain(t, mustStream(t, rt, "do the thing"))
	if mystery.calls != 0 {
		t.Fatalf("unknown tool executed %d times", mystery.calls)
	}
	if len(results) != 1 || !results[0].IsError {
		t.Fatalf("expected refusal result, got %+v", results)
	}
}

func TestAllowOnceExecutesThenExpires(t *testing.T) {
	danger := &recordingTool{name: "run_command"}
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "run_command", map[string]any{"command": "git clean -fdx build-a"}),
		callTurn("c2", "run_command", map[string]any{"command": "git clean -fdx build-b"}),
		{{Text: "Task complete: cleaned."}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(danger); err != nil {
		t.Fatal(err)
	}
	asker := &scriptedAsker{choices: []PermissionChoice{PermissionAllowOnce}}
	rt.SetAsker(asker)

	results, _ := drain(t, mustStream(t, rt, "clean the build artifacts"))

	if asker.len() != 2 {
		t.Fatalf("asker consulted %d times, want 2 (grant must not persist)", asker.len())
	}
	if danger.calls != 1 {
		t.Fatalf("dangerous command ran %d times, want exactly 1", danger.calls)
	}
	var okCount, denied int
	for _, r := range results {
		switch r.IsError {
		case false:
			okCount++
		default:
			denied++
		}
	}
	if okCount != 1 || denied != 1 {
		t.Fatalf("results = %+v", results)
	}
}

func TestDenyRefusesAndUserSeesRefusal(t *testing.T) {
	danger := &recordingTool{name: "run_command"}
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "run_command", map[string]any{"command": "git push --force"}),
		{{Text: "Skipping the force push."}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(danger); err != nil {
		t.Fatal(err)
	}
	asker := &scriptedAsker{choices: []PermissionChoice{PermissionDeny}}
	rt.SetAsker(asker)

	results, final := drain(t, mustStream(t, rt, "force push"))
	if danger.calls != 0 {
		t.Fatal("denied force push executed")
	}
	if len(asker.asked) != 1 {
		t.Fatalf("asker consulted %d times", asker.len())
	}
	if !strings.Contains(final, "Skipping") {
		t.Errorf("loop stopped after denial: %q", final)
	}
	if len(results) != 1 || !strings.Contains(results[0].Content, "Permission denied") {
		t.Fatalf("refusal missing: %+v", results)
	}
}

func TestAllowForTaskScopesToCurrentTaskOnly(t *testing.T) {
	danger := &recordingTool{name: "run_command"}
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "run_command", map[string]any{"command": "terraform apply -auto-approve"}),
		callTurn("c2", "run_command", map[string]any{"command": "terraform apply -auto-approve"}),
		{{Text: "Task complete: applied."}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(danger); err != nil {
		t.Fatal(err)
	}
	asker := &scriptedAsker{choices: []PermissionChoice{PermissionAllowTask}}
	rt.SetAsker(asker)

	_, err := rt.Run([]providers.Message{
		{Role: providers.UserRole, Content: "Apply the terraform config, then apply it again, and verify the outputs."},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Same action inside the SAME task is covered by the grant.
	if asker.len() != 1 {
		t.Fatalf("task grant not honored within its task (%d prompts)", asker.len())
	}
	if danger.calls != 2 {
		t.Fatalf("calls = %d, want 2", danger.calls)
	}

	// A different request creates a new task; the grant must NOT leak.
	p.turns = [][]providers.StreamEvent{
		callTurn("c3", "run_command", map[string]any{"command": "terraform apply -auto-approve"}),
		{{Text: "Task complete: applied again."}, {Done: true}},
	}
	p.calls = 0
	_, err = rt.Run([]providers.Message{
		{Role: providers.UserRole, Content: "Apply the terraform config once more, then verify it."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asker.len() != 2 {
		t.Fatalf("unrelated task inherited approval (prompts=%d)", asker.len())
	}
	if danger.calls != 2 { // second task's call was refused
		t.Fatalf("calls after leak check = %d, want 2", danger.calls)
	}
}

func TestResetPermissionsClearsGrants(t *testing.T) {
	danger := &recordingTool{name: "run_command"}
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "run_command", map[string]any{"command": "docker system prune -af"}),
		{{Text: "done"}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(danger); err != nil {
		t.Fatal(err)
	}
	rt.SetAsker(&scriptedAsker{choices: []PermissionChoice{PermissionAllowTask}})

	drain(t, mustStream(t, rt, "prune docker"))

	if n := rt.ResetPermissions(); n != 1 {
		t.Fatalf("reset returned %d, want 1", n)
	}

	// After reset, even the exact same action asks again.
	asker2 := &scriptedAsker{}
	rt.SetAsker(asker2)
	p.turns = [][]providers.StreamEvent{
		callTurn("c2", "run_command", map[string]any{"command": "docker system prune -af"}),
		{{Text: "done"}, {Done: true}},
	}
	p.calls = 0
	drain(t, mustStream(t, rt, "prune docker again"))
	if asker2.len() != 1 || danger.calls != 1 {
		t.Fatalf("post-reset behavior wrong: asked=%d calls=%d", asker2.len(), danger.calls)
	}
}

func TestFreshRuntimeReasksAfterRestart(t *testing.T) {
	// M12 + M13: grants live in process memory only. A resumed task in
	// a NEW runtime (restart) must be asked again for dangerous work.
	dangerA := &recordingTool{name: "run_command"}
	p1 := &scriptedProvider{turns: [][]providers.StreamEvent{
		{ // plan + approved destructive step
			{Text: "1. Reset the cache\n2. Rebuild\n3. Verify"},
			{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "run_command",
				Arguments: map[string]any{"command": "git reset --hard"}}}},
			{Done: true},
		},
		{{Err: context.Canceled}}, // interrupted mid-task → stays resumable
	}}
	rt1, root := permRuntime(t, p1)
	if err := rt1.manager.Register(dangerA); err != nil {
		t.Fatal(err)
	}
	asker1 := &scriptedAsker{choices: []PermissionChoice{PermissionAllowOnce}}
	rt1.SetAsker(asker1)

	stream, err := rt1.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Reset the cache, rebuild everything, and verify all tests pass."},
	})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, stream)
	if dangerA.calls != 1 || asker1.len() != 1 {
		t.Fatalf("first run: calls=%d asked=%d", dangerA.calls, asker1.len())
	}
	reloaded, _ := taskStoreForRoot(root)
	rs := reloaded.Resumable()
	// An interrupted run leaves the record active-or-paused; both stay
	// resumable per M12.
	if len(rs) != 1 || (rs[0].Status != task.StatusPaused && rs[0].Status != task.StatusActive) {
		t.Fatalf("task not resumable after interruption: %+v", rs)
	}
	if !strings.Contains(rs[0].LastAction, "run_command") {
		t.Errorf("checkpoint lost the last action: %+v", rs[0])
	}

	// Restart: brand-new runtime, empty approval memory, same workspace.
	dangerB := &recordingTool{name: "run_command"}
	p2 := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c9", "run_command", map[string]any{"command": "git reset --hard"}),
		{{Text: "Task complete: rebuilt and verified."}, {Done: true}},
	}}
	rt2 := newTestRuntime(p2)
	rt2.workspace = workspace.DiscoverDir(root)
	rt2.perms = permissions.NewPolicy(root)
	if err := rt2.manager.Register(dangerB); err != nil {
		t.Fatal(err)
	}
	asker2 := &scriptedAsker{choices: []PermissionChoice{PermissionAllowOnce}}
	rt2.SetAsker(asker2)

	response, err := rt2.ResumeStream(context.Background(), "") // single resumable
	if err != nil {
		t.Fatal(err)
	}
	_, final := drain(t, response)

	if asker2.len() != 1 {
		t.Fatalf("resumed task did NOT re-ask for the dangerous action (prompts=%d)", asker2.len())
	}
	if dangerB.calls != 1 {
		t.Fatalf("resumed run executions = %d", dangerB.calls)
	}
	if !strings.Contains(final, "Task complete") {
		t.Errorf("resume did not finish cleanly: %q", final)
	}
}

func TestPermissionPromptsAreRedacted(t *testing.T) {
	danger := &recordingTool{name: "run_command"}
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		callTurn("c1", "run_command", map[string]any{
			"command": `OPENROUTER_API_KEY=sk-secretvalue1234 npm whoami`,
		}),
		{{Text: "ok"}, {Done: true}},
	}}
	rt, _ := permRuntime(t, p)
	if err := rt.manager.Register(danger); err != nil {
		t.Fatal(err)
	}
	asker := &scriptedAsker{}
	rt.SetAsker(asker)

	results, final := drain(t, mustStream(t, rt, "check npm auth"))
	for _, req := range asker.asked {
		if strings.Contains(req.Summary, "sk-secretvalue1234") {
			t.Errorf("permission prompt leaked the secret: %s", req.Summary)
		}
	}
	for _, r := range results {
		if strings.Contains(r.Content, "sk-secretvalue1234") {
			t.Errorf("refusal leaked the secret: %s", r.Content)
		}
	}
	if strings.Contains(final, "sk-secretvalue1234") {
		t.Errorf("final answer leaked the secret")
	}
	if danger.calls != 0 {
		t.Error("credential-bearing command should not auto-run")
	}
}

func TestGateIsProviderIndependent(t *testing.T) {
	// The same runtime, policy, and refusal for different provider
	// backends: swapping the provider must not change privileges.
	danger := &recordingTool{name: "run_command"}
	turns := [][]providers.StreamEvent{
		callTurn("c1", "run_command", map[string]any{"command": "rm -rf ."}),
		{{Text: "ack"}, {Done: true}},
	}

	for _, name := range []string{"ollama-style", "openai-compatible-style"} {
		p := &scriptedProvider{turns: turns}
		rt, _ := permRuntime(t, p)
		if err := rt.manager.Register(danger); err != nil {
			t.Fatal(err)
		}
		rt.provider = p // identical scripted turns, "different" backend

		results, _ := drain(t, mustStream(t, rt, name))
		if danger.calls != 0 {
			t.Fatalf("%s: dangerous call executed", name)
		}
		if len(results) != 1 || !results[0].IsError {
			t.Fatalf("%s: no refusal result: %+v", name, results)
		}
	}
	if danger.calls != 0 {
		t.Fatalf("total unexpected executions: %d", danger.calls)
	}
}

func mustStream(t *testing.T, rt *Runtime, msg string) <-chan Event {
	t.Helper()
	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: msg},
	})
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func (a *scriptedAsker) len() int { return len(a.asked) }

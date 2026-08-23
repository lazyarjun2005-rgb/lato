// Agent-loop continuity regression tests (M16 fix).
//
// The reported bug: recoverable tool failures ended the whole agent
// request, forcing the user to type "continue" after every mistake.
// These tests pin the corrected contract inside the ONE shared M10
// loop: every tool execution — success, structured error result, or Go
// execution error — becomes a tool message in the conversation and the
// loop continues until a legitimate terminal condition.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/effort"
	"lato/internal/providers"
	"lato/internal/task"
	"lato/internal/tools"
	"lato/internal/tools/filesystem"
	"lato/internal/workspace"
)

// errProbe mirrors real tools' Go-error path (missing required
// arguments via tools.StringArg, unknown skill ids from the store,
// failed I/O): Execute returns a REAL error instead of a structured
// IsError result. Each instance fails with its own distinctive message.
type errProbe struct{ name, msg string }

func (e errProbe) Name() string        { return e.name }
func (e errProbe) Description() string { return "always returns a Go execution error" }
func (e errProbe) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (e errProbe) Execute(_ context.Context, _ map[string]any) (tools.Result, error) {
	return tools.Result{}, errors.New(e.msg)
}

// cancelProbe cancels the request context via its captured CancelFunc
// and then errors, simulating a tool interrupted by shutdown: there is
// no later model turn to inform.
type cancelProbe struct {
	name   string
	cancel context.CancelFunc
}

func (c cancelProbe) Name() string        { return c.name }
func (c cancelProbe) Description() string { return "cancels the request context" }
func (c cancelProbe) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (c cancelProbe) Execute(_ context.Context, _ map[string]any) (tools.Result, error) {
	c.cancel()
	return tools.Result{}, errors.New("interrupted")
}

// conversationText joins everything the provider was shown across all
// turns, so tests can assert which information reached the model.
func conversationText(p *scriptedProvider) string {
	var b strings.Builder
	for _, msgs := range p.messages {
		for _, m := range msgs {
			fmt.Fprintf(&b, "[%s] %s\n", m.Role, m.Content)
		}
	}
	return b.String()
}

// drainEvents consumes a stream and returns ordered event types plus
// the final response content, asserting exactly one EventDone that is
// also the last event (Phase 19-I: no premature completion).
func drainEvents(t *testing.T, stream <-chan Event) ([]EventType, string) {
	t.Helper()
	var types []EventType
	final := ""
	for e := range stream {
		types = append(types, e.Type)
		if e.Type == EventError {
			t.Fatalf("unexpected EventError: %v", e.Err)
		}
		if e.Type == EventDone {
			if e.Response != nil {
				final = e.Response.Content
			}
		}
	}
	done := 0
	for _, typ := range types {
		if typ == EventDone {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("EventDone count = %d, want exactly 1 (events: %v)", done, types)
	}
	if types[len(types)-1] != EventDone {
		t.Fatalf("EventDone must be the final event, got %v", types)
	}
	return types, final
}

// TestGoErrorToolFeedsBackAndContinues covers Phase 19-B/D: one tool
// failing with a real Go error → next model turn → successful tool →
// final answer, all inside ONE request with no user continuation.
func TestGoErrorToolFeedsBackAndContinues(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, root := taskRuntimeWithProvider(t, p)
	if err := rt.manager.Register(errProbe{name: "probe_err", msg: `argument "path" is required`}); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "probe_err"), // turn 1: failing call
		toolTurn("c2", "echo"),      // turn 2: model adapts, succeeds
		finalTurn("Task complete: recovered from the tool error."),
	}

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: complexGoal},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, final := drainEvents(t, stream)

	if p.calls != 3 {
		t.Errorf("model turns = %d, want 3 (failure must not end the request)", p.calls)
	}
	if !strings.Contains(final, "Task complete: recovered") {
		t.Errorf("final answer missing:\n%s", final)
	}

	// The failure reached the model as the tool's result. The FINAL
	// turn's conversation must contain it exactly once — appended as a
	// normal tool message, never replayed or retried in secret.
	lastTurn := p.messages[len(p.messages)-1]
	seen := 0
	for _, m := range lastTurn {
		if strings.Contains(m.Content, `argument "path" is required`) {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("tool error appeared %d times in the live conversation, want 1", seen)
	}

	// Task state stayed correct through the failure and completed only
	// at the legitimate end.
	store, err := taskStoreForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	all := store.All()
	if len(all) != 1 || all[0].Status != task.StatusCompleted {
		t.Errorf("task state = %+v, want completed", all)
	}
}

// TestConsecutiveGoErrorsFeedBackInOrder is Phase 5's exact scenario:
// two consecutive recoverable failures (the report's
// list_files("main.go") → list_files("go.mod") shape), each observed by
// the model, then recovery and conclusion — zero user continuations.
func TestConsecutiveGoErrorsFeedBackInOrder(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, _ := taskRuntimeWithProvider(t, p)
	if err := rt.manager.Register(errProbe{name: "probe_a", msg: "main.go is not a directory"}); err != nil {
		t.Fatal(err)
	}
	if err := rt.manager.Register(errProbe{name: "probe_b", msg: "go.mod is not a directory"}); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "probe_a"), // error 1 observed
		toolTurn("c2", "probe_b"), // error 2 observed
		toolTurn("c3", "echo"),    // read main.go (succeeds)
		toolTurn("c4", "echo"),    // read go.mod (succeeds)
		finalTurn("Task complete: inspected after correcting both mistakes."),
	}

	finishes := 0
	var types []EventType
	events, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: complexGoal},
	})
	if err != nil {
		t.Fatal(err)
	}
	for e := range events {
		types = append(types, e.Type)
		if e.Type == EventToolFinish {
			finishes++
			if !e.ToolResult.IsError && e.ToolResult.Name != "echo" {
				t.Errorf("unexpected non-error result for %s", e.ToolResult.Name)
			}
		}
	}
	if finishes != 4 {
		t.Errorf("tool executions = %d, want 4", finishes)
	}
	doneCount := 0
	for i, typ := range types {
		if typ == EventDone {
			doneCount++
			if i != len(types)-1 {
				t.Error("EventDone emitted before the run legitimately finished")
			}
		}
	}
	if doneCount != 1 {
		t.Errorf("EventDone count = %d, want 1", doneCount)
	}
	if p.calls != 5 {
		t.Errorf("model turns = %d, want 5 continuous turns", p.calls)
	}

	conv := conversationText(p)
	for _, want := range []string{"main.go is not a directory", "go.mod is not a directory"} {
		if !strings.Contains(conv, want) {
			t.Errorf("error %q never reached the model", want)
		}
	}
}

// TestUnknownToolNameInformsModelAndContinues pins the hallucinated-tool
// case ("list_directory" instead of list_files): the unknown name is a
// structured result the model can correct, not a fatal stop.
func TestUnknownToolNameInformsModelAndContinues(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, _ := taskRuntimeWithProvider(t, p)

	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "list_directory"), // not registered anywhere
		toolTurn("c2", "echo"),
		finalTurn("Task complete: used the right tool instead."),
	}

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: complexGoal},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, final := drainEvents(t, stream)

	if p.calls != 3 {
		t.Errorf("model turns = %d, want 3", p.calls)
	}
	if !strings.Contains(final, "Task complete") {
		t.Errorf("premature stop:\n%s", final)
	}
	if conv := conversationText(p); !strings.Contains(conv, "tool not found") {
		t.Errorf("unknown-tool feedback missing from conversation:\n%s", conv)
	}
}

// TestRealFilesystemToolsRecoverFromReportedMistake replays the exact
// production mistake with the REAL filesystem tools: listing a file,
// twice, then reading both files, then concluding.
func TestRealFilesystemToolsRecoverFromReportedMistake(t *testing.T) {
	dir := t.TempDir()
	mainGo := filepath.Join(dir, "main.go")
	goMod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(mainGo, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goMod, []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &scriptedProvider{}
	rt, _, _ := taskRuntimeWithProvider(t, p)
	rt.workspace = workspace.DiscoverDir(dir)
	if err := rt.manager.Register(filesystem.NewListFiles()); err != nil {
		t.Fatal(err)
	}
	if err := rt.manager.Register(filesystem.NewReadFile()); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		{
			{Text: "1. Inspect project files"},
			{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "list_files",
				Arguments: map[string]any{"path": mainGo}}}}, // file, not dir → error
			{Done: true},
		},
		{
			{ToolCalls: []providers.ToolCall{{ID: "c2", Name: "list_files",
				Arguments: map[string]any{"path": goMod}}}}, // same mistake again → error
			{Done: true},
		},
		{
			{ToolCalls: []providers.ToolCall{{ID: "c3", Name: "read_file",
				Arguments: map[string]any{"path": mainGo}}}},
			{Done: true},
		},
		{
			{ToolCalls: []providers.ToolCall{{ID: "c4", Name: "read_file",
				Arguments: map[string]any{"path": goMod}}}},
			{Done: true},
		},
		{{Text: "Task complete: project inspected."}, {Done: true}},
	}

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: complexGoal},
	})
	if err != nil {
		t.Fatal(err)
	}
	var results []ToolResult
	for e := range stream {
		if e.Type == EventToolFinish {
			results = append(results, *e.ToolResult)
		}
		if e.Type == EventError {
			t.Fatalf("unexpected EventError: %v", e.Err)
		}
	}
	if len(results) != 4 {
		t.Fatalf("tool executions = %d, want 4 in one continuous run", len(results))
	}
	if !results[0].IsError || !results[1].IsError {
		t.Errorf("listing files should fail structurally: %+v %+v", results[0], results[1])
	}
	if results[2].IsError || results[3].IsError {
		t.Errorf("reads should succeed: %+v %+v", results[2], results[3])
	}
	if !strings.Contains(results[0].Content, "cannot list") ||
		!strings.Contains(results[1].Content, "cannot list") {
		t.Errorf("structured list errors missing:\n%q\n%q", results[0].Content, results[1].Content)
	}

	// Both errors were appended to the conversation the model saw next.
	conv := conversationText(p)
	if strings.Count(conv, "cannot list") < 2 {
		t.Errorf("both tool errors must reach the model:\n%s", conv)
	}
	if !strings.Contains(conv, "package main") || !strings.Contains(conv, "module example.com/demo") {
		t.Errorf("successful reads must reach the model too:\n%s", conv)
	}
	if p.calls != 5 {
		t.Errorf("model turns = %d, want 5", p.calls)
	}
}

// TestFailureRecoveryAcrossAllEffortLevels runs the identical Go-error
// script at every ladder level: the shared loop recovers everywhere;
// only the bounded profile differs.
func TestFailureRecoveryAcrossAllEffortLevels(t *testing.T) {
	for _, lvl := range effort.All {
		t.Run(lvl.String(), func(t *testing.T) {
			p := &scriptedProvider{}
			rt, _, _ := taskRuntimeWithProvider(t, p)
			rt.effort = lvl
			if err := rt.manager.Register(errProbe{name: "probe_err", msg: "boom"}); err != nil {
				t.Fatal(err)
			}

			p.turns = [][]providers.StreamEvent{
				planTurn("c1", "probe_err"),
				toolTurn("c2", "echo"),
				finalTurn("Task complete: recovered at " + lvl.String() + "."),
			}

			resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: complexGoal}})
			if err != nil {
				t.Fatalf("%s: %v", lvl, err)
			}
			if !strings.Contains(resp.Content, "Task complete: recovered") {
				t.Errorf("%s: premature stop, final = %q", lvl, resp.Content)
			}
			if p.calls != 3 {
				t.Errorf("%s: model turns = %d, want 3", lvl, p.calls)
			}
			// Profile propagation: the level survived into this very run.
			if got := profileFor(rt.effort); got.MaxTurns != profileFor(lvl).MaxTurns {
				t.Errorf("%s: wrong profile applied (%+v)", lvl, got)
			}
		})
	}
}

// failingTurn scripts one turn whose probe call carries IDENTICAL
// arguments to every other failing turn, so the repetition signature
// guard (not argument variety) is what's under test.
func failingTurn(id string) []providers.StreamEvent {
	return []providers.StreamEvent{
		{Text: "trying again"},
		{ToolCalls: []providers.ToolCall{{ID: id, Name: "probe_err",
			Arguments: map[string]any{"value": "same"}}}},
		{Done: true},
	}
}

// TestRepeatedFailingCallsStillGuarded proves safety survives the fix:
// retrying the SAME failing call with identical arguments is steered
// once and then stopped by the effort-scaled repetition guard, inside
// one clean pause.
func TestRepeatedFailingCallsStillGuarded(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, root := taskRuntimeWithProvider(t, p)
	rt.effort = effort.Medium // RepeatNudgeAfter=3, RepeatStopAfter=4
	if err := rt.manager.Register(errProbe{name: "probe_err", msg: "still broken"}); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		failingTurn("c1"),
		failingTurn("c2"),
		failingTurn("c3"), // nudge injected after this one
		failingTurn("c4"), // guard fires here
	}

	resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: complexGoal}})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 4 {
		t.Errorf("model turns = %d, want exactly 4 before the guard", p.calls)
	}
	if !strings.Contains(resp.Content, "repeated with identical arguments 4 times") {
		t.Errorf("repetition summary missing:\n%s", resp.Content)
	}
	store, _ := taskStoreForRoot(root)
	rs := store.Resumable()
	if len(rs) != 1 || rs[0].Status != task.StatusPaused {
		t.Errorf("guarded run must pause resumably, got %+v", rs)
	}
}

// TestCancelledRequestDuringToolReportsError pins the one fatal case:
// when the REQUEST context dies mid-execution there is no later model
// turn to inform, so the run ends by cancellation — never with a fake
// completion, and no further model turn is attempted.
func TestCancelledRequestDuringToolReportsError(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, _ := taskRuntimeWithProvider(t, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rt.manager.Register(cancelProbe{name: "probe_cancel", cancel: cancel}); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "probe_cancel"),
	}

	_, err := rt.RunContext(ctx, []providers.Message{{Role: providers.UserRole, Content: complexGoal}})
	if err == nil {
		t.Fatal("expected an error after cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if p.calls != 1 {
		t.Errorf("model turns = %d, want 1 (no turn after cancellation)", p.calls)
	}
}

// TestFailedToolCheckpointsWhileContinuing verifies M12 behavior during
// continuation: the failed attempt is checkpointed into the task record
// while the SAME run keeps going (state stays active-correct, never
// silently dropped).
func TestFailedToolCheckpointsWhileContinuing(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, root := taskRuntimeWithProvider(t, p)
	if err := rt.manager.Register(errProbe{name: "probe_err", msg: "first try failed"}); err != nil {
		t.Fatal(err)
	}
	if err := rt.manager.Register(stubEditTool{}); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "probe_err"),
		toolTurn("c2", "edit_file"),
		finalTurn("Task complete: done despite the failed first step."),
	}

	resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: complexGoal}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Content, "Status: completed") {
		t.Errorf("task must complete after genuine work:\n%s", resp.Content)
	}
	store, _ := taskStoreForRoot(root)
	all := store.All()
	if len(all) != 1 {
		t.Fatalf("tasks = %d, want 1", len(all))
	}
	if !strings.Contains(all[0].LastAction, "edit_file") {
		t.Errorf("checkpoint lost the executed actions: %q", all[0].LastAction)
	}
	if all[0].Status != task.StatusCompleted {
		t.Errorf("status = %q, want completed", all[0].Status)
	}
}

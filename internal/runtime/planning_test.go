package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"lato/internal/providers"
	"lato/internal/tools"
)

// --- complexity gate ----------------------------------------------------

func TestIsComplexTask(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		// simple stays simple
		{"hi", false},
		{"hello there", false},
		{"thanks", false},
		{"What is this function?", false},
		{"Where is fmt.Println used?", false},
		{"Read main.go", false},
		{"Explain this repository", false},

		// complex multi-step goals
		{"Add a login validation function, test it, and fix any errors.", true},
		{"Refactor this package and make sure all tests pass.", true},
		{"Find why the application crashes, fix it, and verify the fix.", true},
		{"Inspect the auth code, then update the handler, and run the tests.", true},
		{"1. inspect files\n2. implement feature\n3. run tests", true},
	}
	for _, c := range cases {
		if got := isComplexTask(c.text); got != c.want {
			t.Errorf("isComplexTask(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// --- directive injection -------------------------------------------------

func TestComplexTaskInjectsDirective(t *testing.T) {
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "ok"}, {Done: true}},
	}}
	rt := newTestRuntime(p)

	stream, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "Add a login validation function, test it, and fix any errors."},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	if len(p.messages) == 0 || !strings.Contains(p.messages[0][0].Content, "Multi-step task protocol") {
		t.Fatal("complex request did not receive the task directive")
	}
	if !strings.Contains(p.messages[0][0].Content, "Task complete:") {
		t.Error("directive lost its summary marker")
	}
}

func TestSimpleRequestSkipsDirective(t *testing.T) {
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "hi there"}, {Done: true}},
	}}
	rt := newTestRuntime(p)

	stream, _ := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "What is this function?"},
	})
	for range stream {
	}

	if len(p.messages) == 0 {
		t.Fatal("provider saw no messages")
	}
	if strings.Contains(p.messages[0][0].Content, "Multi-step task protocol") {
		t.Errorf("simple request received the planning directive:\n%s", p.messages[0][0].Content)
	}
}

// --- bounded execution ---------------------------------------------------

// varyingProvider answers every turn with an echo tool call whose
// argument changes each time, so the repetition guard stays quiet and
// only the turn budget can stop it.
type varyingProvider struct{ n int }

func (p *varyingProvider) ListModels(context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

func (p *varyingProvider) StreamChat(_ context.Context, _ []providers.Message, _ []tools.Definition) (<-chan providers.StreamEvent, error) {
	p.n++
	events := make(chan providers.StreamEvent, 2)
	events <- providers.StreamEvent{ToolCalls: []providers.ToolCall{{
		ID:        fmt.Sprintf("call-%d", p.n),
		Name:      "echo",
		Arguments: map[string]any{"value": fmt.Sprintf("attempt-%d", p.n)},
	}}}
	events <- providers.StreamEvent{Done: true}
	close(events)
	return events, nil
}

func TestBudgetStopsRunCleanly(t *testing.T) {
	rt := newTestRuntime(&varyingProvider{})

	response, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "keep going"}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(response.Content, "Execution budget reached") {
		t.Errorf("final content = %q, want a budget summary", response.Content)
	}
	if !strings.Contains(response.Content, strconv.Itoa(maxAgentTurns)) {
		t.Errorf("budget summary should mention the turn count: %q", response.Content)
	}
	if !strings.Contains(response.Content, "echo") {
		t.Errorf("budget summary should list executed tools: %q", response.Content)
	}
}

// --- repetition guard ----------------------------------------------------

// repeatProvider returns the identical echo tool call every turn.
type repeatProvider struct {
	n       *int
	steered bool
}

func (p *repeatProvider) ListModels(context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

func (p *repeatProvider) StreamChat(_ context.Context, msgs []providers.Message, _ []tools.Definition) (<-chan providers.StreamEvent, error) {
	*p.n++
	for _, m := range msgs {
		if m.Role == providers.SystemRole && strings.Contains(m.Content, "same tool call") {
			p.steered = true
		}
	}
	events := make(chan providers.StreamEvent, 2)
	events <- providers.StreamEvent{ToolCalls: []providers.ToolCall{{
		ID:        "call-" + strconv.Itoa(*p.n),
		Name:      "echo",
		Arguments: map[string]any{"value": "same"},
	}}}
	events <- providers.StreamEvent{Done: true}
	close(events)
	return events, nil
}

func TestRepeatLoopSteersThenStops(t *testing.T) {
	calls := 0
	p := &repeatProvider{n: &calls}
	rt := newTestRuntime(p)

	response, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "do the thing"}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if calls < repeatStopAfter {
		t.Fatalf("loop stopped too early: %d calls", calls)
	}
	if calls > repeatStopAfter+2 {
		t.Fatalf("loop ran too long: %d calls", calls)
	}
	if !p.steered {
		t.Error("steering message was never injected")
	}
	if !strings.Contains(response.Content, "identical arguments") {
		t.Errorf("final content = %q, want repeat-loop summary", response.Content)
	}
}

// --- multi-step recovery --------------------------------------------------

// TestRecoverableFailureContinues pins the core M10 behavior: a failing
// first action does not end the run — the model observes the failure and
// its second turn succeeds. This is exactly how compilation/test failures
// get fixed mid-task.
func TestRecoverableFailureContinues(t *testing.T) {
	flaky := &flakyTool{}
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		{ // first attempt fails
			{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "flaky", Arguments: map[string]any{}}}},
			{Done: true},
		},
		{ // after observing the failure, the agent retries
			{ToolCalls: []providers.ToolCall{{ID: "c2", Name: "flaky", Arguments: map[string]any{}}}},
			{Done: true},
		},
		{
			{Text: "Task complete: recovered after the initial failure."},
			{Done: true},
		},
	}}
	rt := newTestRuntime(p)
	if err := rt.manager.Register(flaky); err != nil {
		t.Fatal(err)
	}

	response, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "fix it"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 3 {
		t.Fatalf("provider turns = %d, want 3 (fail → retry → summary)", p.calls)
	}
	if flaky.runs != 2 {
		t.Fatalf("flaky executed %d times, want 2", flaky.runs)
	}
	if !strings.Contains(response.Content, "Task complete:") {
		t.Errorf("final = %q", response.Content)
	}
}

// flakyTool fails on its first execution and succeeds afterwards.
type flakyTool struct{ runs int }

func (f *flakyTool) Name() string        { return "flaky" }
func (f *flakyTool) Description() string { return "fails once" }
func (f *flakyTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (f *flakyTool) Execute(_ context.Context, _ map[string]any) (tools.Result, error) {
	f.runs++
	if f.runs == 1 {
		return tools.Result{IsError: true, Content: "boom"}, nil
	}
	return tools.Result{Content: "ok"}, nil
}

// --- toolSignature --------------------------------------------------------

func TestToolSignatureDistinguishesCalls(t *testing.T) {
	a := toolSignature("run_command", map[string]any{"command": "go test ./..."})
	b := toolSignature("run_command", map[string]any{"command": "go test ./..."})
	c := toolSignature("run_command", map[string]any{"command": "go vet ./..."})

	if a != b {
		t.Error("identical calls must produce identical signatures")
	}
	if a == c {
		t.Error("different arguments must produce different signatures")
	}
}

// --- existing behavior stays intact ---------------------------------------

func TestSingleTurnStillWorks(t *testing.T) {
	p := &scriptedProvider{turns: [][]providers.StreamEvent{
		{{Text: "hello!"}, {Done: true}},
	}}
	rt := newTestRuntime(p)

	response, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "hello!" || p.calls != 1 {
		t.Fatalf("simple path changed: %q after %d turns", response.Content, p.calls)
	}
}

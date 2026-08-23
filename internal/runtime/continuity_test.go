package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"lato/internal/effort"
	"lato/internal/providers"
	"lato/internal/task"
	"lato/internal/tools"
)

// failingProbe always returns an error result, mirroring the real-world
// "list_files main.go → not a directory" mistake: the tool runs, the
// model gets a clear error, nothing terminates.
type failingProbe struct{ name string }

func (f failingProbe) Name() string        { return f.name }
func (f failingProbe) Description() string { return "always fails" }
func (f failingProbe) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (f failingProbe) Execute(_ context.Context, _ map[string]any) (tools.Result, error) {
	return tools.Result{IsError: true, Content: "not a directory"}, nil
}

// planTurn is the scripted first turn of a complex task: visible plan
// plus one tool call.
func planTurn(callID, tool string) []providers.StreamEvent {
	return []providers.StreamEvent{
		{Text: "1. Inspect\n2. Implement\n3. Verify"},
		{ToolCalls: []providers.ToolCall{{ID: callID, Name: tool,
			Arguments: map[string]any{"value": callID}}}},
		{Done: true},
	}
}

func toolTurn(callID, tool string) []providers.StreamEvent {
	return []providers.StreamEvent{
		{ToolCalls: []providers.ToolCall{{ID: callID, Name: tool,
			Arguments: map[string]any{"value": callID}}}},
		{Done: true},
	}
}

func finalTurn(text string) []providers.StreamEvent {
	return []providers.StreamEvent{{Text: text}, {Done: true}}
}

const complexGoal = "Inspect this project, implement the fix, add tests, and run verification."

// TestRecoverableToolFailuresContinueAutomatically pins the reported
// bug: two consecutive recoverable tool failures must NOT end the
// request. The model observes each error and keeps working inside ONE
// agent run; the user never types "continue".
func TestRecoverableToolFailuresContinueAutomatically(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, _ := taskRuntimeWithProvider(t, p)
	if err := rt.manager.Register(failingProbe{name: "probe_fail"}); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "probe_fail"),                    // bad call #1 → error observed
		toolTurn("c2", "probe_fail"),                    // bad call #2 → error observed
		toolTurn("c3", "echo"),                          // recovery: success
		finalTurn("Task complete: fixed and verified."), // conclusion
	}

	var finishes, errs int
	events, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: complexGoal},
	})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range events {
		switch ev.Type {
		case EventToolFinish:
			finishes++
		case EventError:
			errs++
			t.Errorf("unexpected error event: %v", ev.Err)
		}
	}

	if finishes != 3 {
		t.Errorf("tool executions = %d, want 3 in one continuous run", finishes)
	}
	if errs != 0 {
		t.Error("error events emitted")
	}

	store, err := taskStoreForRoot(rootForRuntime(rt))
	if err != nil {
		t.Fatal(err)
	}
	all := store.All()
	if len(all) != 1 || all[0].Status != task.StatusCompleted {
		t.Errorf("task state = %+v, want completed", all)
	}
}

// taskRuntimeWithProvider adapts the shared taskRuntime harness so the
// scripted provider is installed before any run starts.
func taskRuntimeWithProvider(t *testing.T, p *scriptedProvider) (*Runtime, *scriptedProvider, string) {
	t.Helper()
	rt, got, root := taskRuntime(t)
	if got != p {
		// taskRuntime installs its own default script provider; swap in
		// ours while preserving the discovered workspace.
		rt.provider = p
	}
	return rt, got, root
}

func rootForRuntime(rt *Runtime) string { return rt.workspace.Root }

// TestRecoveryAcrossAllEffortLevels runs the identical failure-recovery
// script under every ladder level and asserts the shared loop behaves
// identically — only the profile bounds differ.
func TestRecoveryAcrossAllEffortLevels(t *testing.T) {
	for _, lvl := range effort.All {
		t.Run(lvl.String(), func(t *testing.T) {
			p := &scriptedProvider{}
			rt, _, _ := taskRuntimeWithProvider(t, p)
			rt.effort = lvl
			if err := rt.manager.Register(failingProbe{name: "probe_fail"}); err != nil {
				t.Fatal(err)
			}

			p.turns = [][]providers.StreamEvent{
				planTurn("c1", "probe_fail"),
				toolTurn("c2", "probe_fail"),
				toolTurn("c3", "echo"),
				finalTurn("Task complete: recovered at " + lvl.String() + "."),
			}

			resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: complexGoal}})
			if err != nil {
				t.Fatalf("%s: %v", lvl, err)
			}
			if !strings.Contains(resp.Content, "Task complete: recovered") {
				t.Errorf("%s: premature stop, final = %q", lvl, resp.Content)
			}

			// Profile propagation: the level survived into the run.
			if got := profileFor(rt.effort); got.MaxTurns != profileFor(lvl).MaxTurns {
				t.Errorf("%s: wrong profile applied", lvl)
			}
		})
	}
}

// TestNarrationStallGetsBoundedContinuations proves mid-task narration
// turns are steered back into the same run exactly twice, then paused
// honestly — never silently dropped, never marked completed.
func TestNarrationStallGetsBoundedContinuations(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, _ := taskRuntimeWithProvider(t, p)
	if err := rt.manager.Register(stubEditTool{}); err != nil {
		t.Fatal(err)
	}

	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "edit_file"),                     // progress
		finalTurn("Let me think about the approach..."), // stall 1 → nudged
		toolTurn("c2", "edit_file"),                     // real progress
		finalTurn("Hmm, where was I..."),                // stall 2 → nudged
		finalTurn("Still thinking..."),                  // allowance exhausted → honest pause
	}

	resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: complexGoal}})
	if err != nil {
		t.Fatal(err)
	}

	// Count NEWLY injected continuation nudges: the nudge stays in the
	// conversation, so later turns' history re-contains earlier ones.
	nudges := 0
	prev := 0
	for _, msgs := range p.messages {
		inThis := 0
		for _, msg := range msgs {
			if msg.Role == providers.SystemRole && strings.Contains(msg.Content, "planned steps are still open") {
				inThis++
			}
		}
		if inThis > prev {
			nudges += inThis - prev
		}
		prev = inThis
	}
	if nudges != 2 {
		t.Errorf("continuation nudges = %d, want exactly 2", nudges)
	}

	if !strings.Contains(resp.Content, "Paused:") {
		t.Errorf("stalled-out run must pause with an explicit reason, got:\n%s", resp.Content)
	}
	if strings.Contains(resp.Content, "Status: completed") {
		t.Errorf("unconcluded run claimed completion:\n%s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Status: paused") {
		t.Errorf("preview missing paused status:\n%s", resp.Content)
	}
}

// TestExplicitMarkersFinalizeImmediately guards the refinement: a model
// that says "Task complete:" or "Task blocked:" is respected even when
// it never emitted [x] step markers.
func TestExplicitMarkersFinalizeImmediately(t *testing.T) {
	cases := []struct {
		text   string
		status task.Status
	}{
		{"Task complete: all done.", task.StatusCompleted},
		{"Task blocked: need user input.", task.StatusBlocked},
	}
	for _, tc := range cases {
		p := &scriptedProvider{}
		rt, _, _ := taskRuntimeWithProvider(t, p)
		p.turns = [][]providers.StreamEvent{
			planTurn("c1", "echo"),
			toolTurn("c2", "echo"),
			finalTurn(tc.text),
		}
		resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: complexGoal}})
		if err != nil {
			t.Fatal(err)
		}
		if p.calls != 3 {
			t.Errorf("%q: consumed %d model turns, want 3 (no nudge after marker)", tc.text, p.calls)
		}
		store, err := taskStoreForRoot(rt.workspace.Root)
		if err != nil {
			t.Fatal(err)
		}
		all := store.All()
		if len(all) != 1 || all[0].Status != tc.status {
			t.Errorf("%q: status = %+v, want %v", tc.text, all, tc.status)
		}
		_ = resp
	}
}

// varyingCallProvider requests a tool call with fresh arguments every
// turn, so the repetition guard never fires and the TURN BUDGET is the
// only thing that can stop it.
type varyingCallProvider struct{ n int }

func (v *varyingCallProvider) StreamChat(_ context.Context, _ []providers.Message, _ []tools.Definition) (<-chan providers.StreamEvent, error) {
	v.n++
	events := make(chan providers.StreamEvent, 1)
	events <- providers.StreamEvent{
		ToolCalls: []providers.ToolCall{{
			ID:        fmt.Sprintf("call-%d", v.n),
			Name:      "echo",
			Arguments: map[string]any{"value": fmt.Sprintf("run-%d", v.n)},
		}},
		Done: true,
	}
	close(events)
	return events, nil
}

func (v *varyingCallProvider) ListModels(context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

// TestPerLevelTurnBudgetsExecuted proves — by execution, not constants —
// that each effort level really uses its intended bound.
func TestPerLevelTurnBudgetsExecuted(t *testing.T) {
	cases := map[effort.Level]int{
		effort.Low:    6,
		effort.Medium: 12,
		effort.High:   18,
		effort.Ultra:  24,
		effort.LatoX:  32,
	}
	for lvl, want := range cases {
		rt := newTestRuntime(&varyingCallProvider{})
		rt.effort = lvl

		resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: complexGoal}})
		if err != nil {
			t.Fatalf("%s: %v", lvl, err)
		}

		wantMsg := fmt.Sprintf("Execution budget reached after %d model turns.", want)
		if !strings.Contains(resp.Content, wantMsg) {
			t.Errorf("%s: budget message %q missing:\n%s", lvl, wantMsg, resp.Content)
		}
	}
}

// TestEffortSurvivesModelAndProviderSwitches pins propagation: effort
// set through any public path survives provider rebuilds and session-
// only model changes; nothing silently falls back to medium.
func TestEffortSurvivesModelAndProviderSwitches(t *testing.T) {
	isolateUserConfig(t)
	cfgDir := t.TempDir()
	t.Setenv("LATO_HOME", cfgDir)
	if err := writeTestConfig(cfgDir, "openrouter"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENROUTER_API_KEY", "k")

	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.SetEffort("lato-x", false); err != nil {
		t.Fatal(err)
	}

	// Session-only model switch keeps the level.
	if err := rt.SetSessionModelWithEffort("some/model", ""); err != nil {
		t.Fatal(err)
	}
	if got := rt.CurrentEffort(); got != "lato-X" {
		t.Errorf("after session switch effort = %q, want lato-X", got)
	}

	// Persisted provider switch also preserves it.
	if err := rt.SetEffort("ultra", true); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetSessionModelWithEffort("other/model", ""); err != nil {
		t.Fatal(err)
	}
	if rt.Effort() != effort.Ultra {
		t.Errorf("effort = %v, want ultra", rt.Effort())
	}

	// And the persisted default round-trips through Load.
	rt2, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if rt2.Effort() != effort.Ultra {
		t.Errorf("reloaded effort = %v, want ultra from config.yaml", rt2.Effort())
	}
}

// TestMarkerDetectionScansAllLines pins the resume-scenario shape: [x]
// progress markers may precede the concluding line, and the terminal
// marker must still be honored wherever it appears.
func TestMarkerDetectionScansAllLines(t *testing.T) {
	markerAfterProgress := "[x] 3. Run tests\nTask complete: validation verified."
	if !hasCompletionMarker(markerAfterProgress) {
		t.Error("completion marker after progress lines not detected")
	}
	if !hasTerminalMarker(markerAfterProgress) {
		t.Error("terminal marker after progress lines not detected")
	}

	var tk task.Task
	tk.Goal = "Fix the validation bug in the api, test it, and verify."
	tk.SetPlanFromText("1. Inspect handler\n2. Fix validation\n3. Run tests")
	tr := &taskTracker{rt: rt0(), t: &tk, enabled: true}
	if tr.needsContinuation(markerAfterProgress) {
		t.Error("concluded output treated as a stall")
	}
	if !tr.needsContinuation("Halfway through step two, reconsidering.") {
		t.Error("genuine narration not detected as stall")
	}
}

// rt0 provides a minimal nil-runtime for tracker-level unit checks.
func rt0() *Runtime { return &Runtime{} }

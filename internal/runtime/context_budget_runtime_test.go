// Phase 3A integration tests. history_budget_test.go proves TrimHistory
// is correct in isolation; these prove the runtime actually applies it,
// at the right point in the loop, while real tool calls grow the working
// set. The wiring itself is otherwise untested: a provider that never
// received a trimmed slice would look perfectly healthy.
package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"lato/internal/config"
	"lato/internal/providers"
	"lato/internal/tools"
	"lato/internal/workspace"
)

// capturingProvider records the exact message slice handed to the
// provider on every model turn, then drives a loop of tool calls so the
// working set grows the way a real agent run makes it grow. Tool
// arguments vary per turn because the runtime's repetition guard stops a
// loop that keeps issuing identical calls.
type capturingProvider struct {
	mu          sync.Mutex
	received    [][]providers.Message
	toolTurn    int
	maxToolTurn int
	// resultBytes is the exact length of each tool result. Zero selects a
	// small default, which is deliberately too small to outgrow a budget.
	resultBytes int
}

func (c *capturingProvider) StreamChat(_ context.Context, msgs []providers.Message, _ []tools.Definition) (<-chan providers.StreamEvent, error) {
	c.mu.Lock()
	c.received = append(c.received, append([]providers.Message(nil), msgs...))
	n := c.toolTurn
	final := n >= c.maxToolTurn
	if !final {
		c.toolTurn++
	}
	resultBytes := c.resultBytes
	c.mu.Unlock()

	events := make(chan providers.StreamEvent, 2)
	if final {
		events <- providers.StreamEvent{Text: "done", Done: true}
		close(events)
		return events, nil
	}

	seed := fmt.Sprintf("tool-output-%d ", n)
	value := strings.Repeat(seed, 20)
	if resultBytes > 0 {
		value = strings.Repeat(seed, 1+resultBytes/len(seed))[:resultBytes]
	}
	events <- providers.StreamEvent{
		ToolCalls: []providers.ToolCall{{
			ID:        fmt.Sprintf("call-%d", n),
			Name:      "echo",
			Arguments: map[string]any{"value": value},
		}},
	}
	events <- providers.StreamEvent{Done: true}
	close(events)
	return events, nil
}

func (c *capturingProvider) ListModels(context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

// newBudgetRuntime builds a runtime with a real config so the production
// EffectiveLimits path is exercised.
//
// rt.perms is deliberately left nil: with no policy attached the runtime
// executes tools directly, and these tests need real results flowing back
// into the working set. A policy would deny them (nothing interactive is
// attached), and denied calls still produce tool results, which would let
// a broken budget look healthy.
func newBudgetRuntime(t *testing.T, p providers.ModelProvider, l config.Limits) *Runtime {
	t.Helper()
	isolateUserConfig(t)
	root := t.TempDir()
	rt := newTestRuntime(p)
	rt.workspace = workspace.DiscoverDir(root)
	rt.cfg = &config.Config{Limits: l}
	return rt
}

// budgetBody returns the messages TrimHistory is allowed to trim: the
// leading system run is pinned and is deliberately outside the byte
// budget, so measuring it would assert something untrue.
func budgetBody(msgs []providers.Message) []providers.Message {
	_, body := splitLeadingSystem(msgs)
	return body
}

func budgetBodyBytes(msgs []providers.Message) int {
	total := 0
	for _, m := range budgetBody(msgs) {
		total += messageSize(m)
	}
	return total
}

// TestRuntimeAppliesContextBudgetEveryTurn proves the budget is enforced
// per model turn on the slice actually sent to the provider, not merely
// present in the source tree. The loop is driven through several tool
// calls so the working set outgrows a deliberately tiny budget.
func TestRuntimeAppliesContextBudgetEveryTurn(t *testing.T) {
	p := &capturingProvider{maxToolTurn: 6}
	const maxTurns = 8
	// Small enough that the loop cannot possibly stay under it.
	rt := newBudgetRuntime(t, p, config.Limits{
		ContextBudget:   4 << 10,
		MaxHistoryTurns: maxTurns,
	})

	if _, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "please investigate thoroughly"}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	p.mu.Lock()
	received := append([][]providers.Message(nil), p.received...)
	p.mu.Unlock()

	if len(received) < 3 {
		t.Fatalf("expected several model turns, got %d", len(received))
	}

	for i, msgs := range received {
		// Structure must survive the live agent loop, where real tool
		// calls and real ToolCallIDs are in play.
		assertNoOrphanToolResults(t, msgs)
		assertCompleteToolGroups(t, msgs)

		// The system prompt is pinned, so history always starts with it.
		if len(msgs) == 0 || msgs[0].Role != providers.SystemRole {
			t.Fatalf("turn %d: provider must still see the system prompt first, got %s", i, roles(msgs))
		}

		body := budgetBody(msgs)
		if n := countUserMessages(body); n > maxTurns {
			t.Fatalf("turn %d: %d user turns exceeds MaxHistoryTurns=%d (%s)", i, n, maxTurns, roles(msgs))
		}

		// The request being served is never evicted.
		joined := strings.Join(contents(body), "|")
		if !strings.Contains(joined, "please investigate thoroughly") {
			t.Fatalf("turn %d: current request missing from provider input: %s", i, joined)
		}
	}
}

// TestRuntimeEvictsOldHistoryAsLoopGrows verifies the budget actually
// bites inside a single request's tool loop. Tool results are sized well
// past the budget so the accumulated history cannot possibly fit, which is
// what makes this a real regression test: with an implementation that pins
// everything from the current request onward, nothing is ever evicted and
// every assertion below still holds, but the eviction checks fail.
func TestRuntimeEvictsOldHistoryAsLoopGrows(t *testing.T) {
	const (
		maxToolTurns = 8
		resultBytes  = 2000
		budget       = 4 << 10
	)
	p := &capturingProvider{maxToolTurn: maxToolTurns, resultBytes: resultBytes}
	rt := newBudgetRuntime(t, p, config.Limits{
		ContextBudget:   budget,
		MaxHistoryTurns: 20,
	})

	if _, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "investigate the failing integration tests now"}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	p.mu.Lock()
	received := append([][]providers.Message(nil), p.received...)
	executed := p.toolTurn
	p.mu.Unlock()

	if executed != maxToolTurns {
		t.Fatalf("expected %d executed tool turns, got %d", maxToolTurns, executed)
	}
	if len(received) < 2 {
		t.Fatalf("expected several model turns, got %d", len(received))
	}

	// The loop must have produced far more history than the budget allows.
	untrimmed := executed * resultBytes
	if untrimmed <= budget {
		t.Fatalf("test is not discriminating: %d bytes of history never exceeds a %d-byte budget", untrimmed, budget)
	}

	last := received[len(received)-1]
	body := budgetBody(last)

	// Only the two pins may exceed the budget, so retained history is
	// bounded by the budget plus one request unit and one tool group.
	ceiling := budget + resultBytes + 2*messageOverheadBytes + 2*toolCallOverheadBytes
	if n := budgetBodyBytes(last); n > ceiling {
		t.Errorf("retained %d bytes of history, want <= %d (%d messages)", n, ceiling, len(body))
	}

	// The decisive check: a single request's tool loop must not accumulate
	// every result it ever produced.
	if n := countToolResults(body); n >= executed {
		t.Errorf("no eviction inside one tool loop: %d results retained across %d turns", n, executed)
	}
	if n := countToolResults(body); n == 0 {
		t.Fatalf("the most recent tool result must be retained (%d messages)", len(body))
	}
	if n := budgetBodyBytes(last); n >= untrimmed {
		t.Errorf("retained %d bytes of an untrimmed %d; nothing was evicted", n, untrimmed)
	}

	// Both pins survive.
	joined := strings.Join(contents(body), "|")
	if !strings.Contains(joined, "investigate the failing integration tests now") {
		t.Errorf("current request evicted (%d messages)", len(body))
	}
	freshest := fmt.Sprintf("call-%d", executed-1)
	found := false
	for _, m := range body {
		if m.ToolCallID == freshest {
			found = true
		}
	}
	if !found {
		t.Errorf("freshest tool result %s evicted (%d messages)", freshest, len(body))
	}
}

// TestRuntimeHistoryBudgetNeverOrphansAcrossAGrowingLoop is the invariant
// that matters most at runtime: every turn the provider is handed a
// structurally valid sequence, no matter how the budget interacts with
// the growing tool-call trail.
func TestRuntimeHistoryBudgetNeverOrphansAcrossAGrowingLoop(t *testing.T) {
	for _, budget := range []int{512, 1 << 10, 2 << 10, 8 << 10, 1 << 20} {
		for _, turns := range []int{1, 2, 5, 40} {
			p := &capturingProvider{maxToolTurn: 5}
			rt := newBudgetRuntime(t, p, config.Limits{
				ContextBudget:   budget,
				MaxHistoryTurns: turns,
			})
			if _, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "check the failing tests carefully"}}); err != nil {
				t.Fatalf("budget=%d turns=%d: Run() error = %v", budget, turns, err)
			}
			p.mu.Lock()
			received := append([][]providers.Message(nil), p.received...)
			p.mu.Unlock()
			for i, msgs := range received {
				assertNoOrphanToolResults(t, msgs)
				assertCompleteToolGroups(t, msgs)
				if len(budgetBody(msgs)) == 0 {
					t.Fatalf("budget=%d turns=%d turn %d: history emptied, current request lost (%s)",
						budget, turns, i, roles(msgs))
				}
			}
		}
	}
}

// TestRuntimeAlwaysShowsTheModelItsFreshToolResult pins the invariant that
// matters most for correctness: the result the model just asked for is
// always visible on the very next model turn, whatever the budget.
// Evicting older results is the point; evicting the current one would
// hand the model a request it can no longer see the answer to.
func TestRuntimeAlwaysShowsTheModelItsFreshToolResult(t *testing.T) {
	for _, budget := range []int{256, 512, 1 << 10, 8 << 10, 1 << 20} {
		for _, turns := range []int{1, 2, 20} {
			p := &capturingProvider{maxToolTurn: 5}
			rt := newBudgetRuntime(t, p, config.Limits{
				ContextBudget:   budget,
				MaxHistoryTurns: turns,
			})
			resp, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "check the failing tests carefully"}})
			if err != nil {
				t.Fatalf("budget=%d turns=%d: Run() error = %v", budget, turns, err)
			}
			if !strings.Contains(resp.Content, "done") {
				t.Fatalf("budget=%d turns=%d: loop never reached its final turn: %q", budget, turns, resp.Content)
			}

			p.mu.Lock()
			received := append([][]providers.Message(nil), p.received...)
			executed := p.toolTurn
			p.mu.Unlock()

			if executed < 2 {
				t.Fatalf("budget=%d turns=%d: expected a multi-turn tool loop, got %d", budget, turns, executed)
			}

			// The turn that followed the last executed tool call must carry
			// both halves of that call: the assistant message and its result.
			last := received[len(received)-1]
			freshest := fmt.Sprintf("call-%d", executed-1)
			hasCall, hasResult := false, false
			for _, m := range last {
				for _, tc := range m.ToolCalls {
					if tc.ID == freshest {
						hasCall = true
					}
				}
				if m.ToolCallID == freshest {
					hasResult = true
				}
			}
			if !hasCall || !hasResult {
				t.Fatalf("budget=%d turns=%d: freshest tool call %s incomplete on the next model turn (call=%v result=%v): %s",
					budget, turns, freshest, hasCall, hasResult, roles(last))
			}
		}
	}
}

func countToolResults(msgs []providers.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == providers.ToolRole {
			n++
		}
	}
	return n
}

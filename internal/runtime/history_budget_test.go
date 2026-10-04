package runtime

import (
	"fmt"
	"strings"
	"testing"

	"lato/internal/providers"
)

// --- test helpers -----------------------------------------------------------

// assistant returns an assistant message with the given content and tool
// call ids, mirroring runtime.go: the runtime records ToolCalls on
// assistant messages only.
func assistant(content string, ids ...string) providers.Message {
	m := providers.Message{Role: providers.AssistantRole, Content: content}
	for _, id := range ids {
		m.ToolCalls = append(m.ToolCalls, providers.ToolCall{ID: id, Name: "tool_" + id})
	}
	return m
}

// toolResult returns a tool result message, mirroring runtime.go: role
// ToolRole with ToolCallID set to the issuing call's id.
func toolResult(id, content string) providers.Message {
	return providers.Message{
		Role:       providers.ToolRole,
		Name:       "tool_" + id,
		Content:    content,
		ToolCallID: id,
	}
}

func userMsg(content string) providers.Message {
	return providers.Message{Role: providers.UserRole, Content: content}
}

func systemMsg(content string) providers.Message {
	return providers.Message{Role: providers.SystemRole, Content: content}
}

func roles(msgs []providers.Message) string {
	var parts []string
	for _, m := range msgs {
		parts = append(parts, string(m.Role)+":"+m.Content)
	}
	return strings.Join(parts, "|")
}

// contents returns the content of every message, for order assertions.
func contents(msgs []providers.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

// assertNoOrphanToolResults verifies the central invariant: every retained
// tool result is preceded, somewhere earlier in the slice, by the
// assistant message that issued it.
func assertNoOrphanToolResults(t *testing.T, msgs []providers.Message) {
	t.Helper()
	for i, m := range msgs {
		if m.Role != providers.ToolRole {
			continue
		}
		if toolCallOwner(msgs, i) < 0 {
			t.Fatalf("orphaned tool result at index %d (ToolCallID=%q) in %s",
				i, m.ToolCallID, roles(msgs))
		}
	}
}

// assertCompleteToolGroups verifies that every retained assistant tool
// call has all of its results present in the slice.
func assertCompleteToolGroups(t *testing.T, msgs []providers.Message) {
	t.Helper()
	present := map[string]int{}
	for _, m := range msgs {
		if m.Role == providers.ToolRole {
			present[m.ToolCallID]++
		}
	}
	for i, m := range msgs {
		for _, tc := range m.ToolCalls {
			if present[tc.ID] == 0 {
				t.Fatalf("incomplete tool-call group at index %d: call %q has no result in %s",
					i, tc.ID, roles(msgs))
			}
		}
	}
}

// assertInvariants runs every structural guarantee TrimHistory promises.
func assertInvariants(t *testing.T, in, out []providers.Message) {
	t.Helper()
	if len(in) > 0 && len(out) == 0 {
		t.Fatalf("TrimHistory returned an empty slice for %d input messages", len(in))
	}
	assertNoOrphanToolResults(t, out)
	assertCompleteToolGroups(t, out)
}

// --- degenerate inputs ------------------------------------------------------

func TestTrimHistoryEmptyInput(t *testing.T) {
	got := TrimHistory(nil, HistoryBudget{MaxBytes: 10, MaxTurns: 1})
	if got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}
	got = TrimHistory([]providers.Message{}, HistoryBudget{MaxBytes: 10, MaxTurns: 1})
	if len(got) != 0 {
		t.Fatalf("expected empty result for empty input, got %v", got)
	}
}

func TestTrimHistoryNoLimitsReturnsEverything(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("a1"),
		userMsg("q2"),
	}
	got := TrimHistory(in, HistoryBudget{})
	if len(got) != len(in) {
		t.Fatalf("zero budget must not trim: got %d messages, want %d", len(got), len(in))
	}
	assertInvariants(t, in, got)
}

func TestTrimHistoryNegativeLimitsAreUnset(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("a1"),
		userMsg("q2"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: -1, MaxTurns: -5})
	if len(got) != len(in) {
		t.Fatalf("negative limits must not trim: got %d, want %d", len(got), len(in))
	}
}

func TestTrimHistoryOnlySystemMessages(t *testing.T) {
	in := []providers.Message{systemMsg("s1"), systemMsg("s2")}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 1, MaxTurns: 1})
	if len(got) != 2 {
		t.Fatalf("all-system history must be preserved, got %d", len(got))
	}
}

// --- within budget ----------------------------------------------------------

func TestTrimHistoryWithinBudgetIsUnchanged(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("hello"),
		assistant("hi"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 1 << 20, MaxTurns: 20})
	if len(got) != len(in) {
		t.Fatalf("history within budget must be unchanged: got %s", roles(got))
	}
	if got[0].Content != "sys" || got[1].Content != "hello" || got[2].Content != "hi" {
		t.Fatalf("unexpected content: %s", roles(got))
	}
}

// --- byte budget -----------------------------------------------------------

func TestTrimHistoryByteBudgetEvictsOldestTurns(t *testing.T) {
	var in []providers.Message
	in = append(in, systemMsg("sys"))
	for _, q := range []string{"q1", "q2", "q3", "q4"} {
		in = append(in, userMsg(q), assistant("reply-"+q))
	}
	// Room for roughly two turns.
	got := TrimHistory(in, HistoryBudget{MaxBytes: 60})
	assertInvariants(t, in, got)
	if got[0].Content != "sys" {
		t.Fatalf("system message must be preserved, got %s", roles(got))
	}
	cs := contents(got)
	joined := strings.Join(cs, ",")
	if !strings.Contains(joined, "q4") || !strings.Contains(joined, "reply-q4") {
		t.Fatalf("most recent turn must survive, got %s", joined)
	}
	if strings.Contains(joined, "q1") {
		t.Fatalf("oldest turn should have been evicted, got %s", joined)
	}
	assertOrderedSuffix(t, in, got)
}

func TestTrimHistoryKeepsCurrentRequestEvenWhenOversized(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("a1"),
		userMsg(strings.Repeat("X", 5000)),
	}
	// Byte budget far smaller than the current request.
	got := TrimHistory(in, HistoryBudget{MaxBytes: 10})
	assertInvariants(t, in, got)
	if got[len(got)-1].Content != strings.Repeat("X", 5000) {
		t.Fatalf("current user request must be retained in full, got %s", roles(got))
	}
	if got[0].Content != "sys" {
		t.Fatalf("system message must be preserved, got %s", roles(got))
	}
	// Structural integrity wins over the byte bound.
	total := 0
	for _, m := range got {
		total += messageSize(m)
	}
	if total <= 10 {
		t.Fatalf("expected the budget to be exceeded to preserve structure, got %d bytes", total)
	}
}

// --- turn budget -----------------------------------------------------------

func TestTrimHistoryTurnBudget(t *testing.T) {
	var in []providers.Message
	in = append(in, systemMsg("sys"))
	for _, q := range []string{"q1", "q2", "q3", "q4", "q5"} {
		in = append(in, userMsg(q), assistant("r-"+q))
	}
	got := TrimHistory(in, HistoryBudget{MaxTurns: 2})
	assertInvariants(t, in, got)
	if n := countUserMessages(got); n != 2 {
		t.Fatalf("expected 2 retained turns, got %d in %s", n, roles(got))
	}
	joined := strings.Join(contents(got), ",")
	if !strings.Contains(joined, "q5") {
		t.Fatalf("most recent turn must survive, got %s", joined)
	}
	if strings.Contains(joined, "q1") {
		t.Fatalf("oldest turn should be evicted, got %s", joined)
	}
}

func TestTrimHistoryTurnBudgetOfOneKeepsOnlyLastTurn(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("r1"),
		userMsg("q2"),
		assistant("r2"),
	}
	got := TrimHistory(in, HistoryBudget{MaxTurns: 1})
	if n := countUserMessages(got); n != 1 {
		t.Fatalf("expected exactly 1 turn, got %d in %s", n, roles(got))
	}
	joined := strings.Join(contents(got), ",")
	if strings.Contains(joined, "q1") || !strings.Contains(joined, "q2") {
		t.Fatalf("expected only the last turn, got %s", joined)
	}
}

func TestTrimHistoryTurnBudgetZeroMeansUnlimited(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"), assistant("r1"),
		userMsg("q2"), assistant("r2"),
		userMsg("q3"), assistant("r3"),
	}
	got := TrimHistory(in, HistoryBudget{MaxTurns: 0})
	if len(got) != len(in) {
		t.Fatalf("MaxTurns=0 must not trim: got %d, want %d", len(got), len(in))
	}
}

// --- system messages -------------------------------------------------------

func TestTrimHistoryAlwaysPreservesLeadingSystemPrompt(t *testing.T) {
	var in []providers.Message
	in = append(in, systemMsg("SYSTEM-INSTRUCTIONS"))
	for _, q := range []string{"q1", "q2", "q3"} {
		in = append(in, userMsg(q), assistant("r-"+q))
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 40})
	if len(got) == 0 || got[0].Role != providers.SystemRole {
		t.Fatalf("system instructions must lead the retained history, got %s", roles(got))
	}
	if got[0].Content != "SYSTEM-INSTRUCTIONS" {
		t.Fatalf("system content altered: %q", got[0].Content)
	}
}

func TestTrimHistoryPreservesMultipleLeadingSystemMessages(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys-a"),
		systemMsg("sys-b"),
		userMsg("q1"),
		assistant("r1"),
		userMsg("q2"),
		assistant("r2"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 30})
	if len(got) < 2 || got[0].Content != "sys-a" || got[1].Content != "sys-b" {
		t.Fatalf("both leading system messages must survive, got %s", roles(got))
	}
}

func TestTrimHistoryMidConversationSystemMessageIsEvictable(t *testing.T) {
	// A steering message is appended mid-loop by the runtime; unlike the
	// leading prompt it is ordinary history and may be dropped.
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		systemMsg("steering"),
		assistant("r1"),
		userMsg("q2"),
		assistant("r2"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 40})
	joined := strings.Join(contents(got), ",")
	if strings.Contains(joined, "steering") {
		t.Fatalf("expected the steering message to be evicted as the oldest history, got %s", joined)
	}
	if !strings.Contains(joined, "q2") || !strings.Contains(joined, "r2") {
		t.Fatalf("recent turn must survive, got %s", joined)
	}
	if got[0].Content != "sys" {
		t.Fatalf("system message must survive, got %s", roles(got))
	}
}

// --- tool-call groups ------------------------------------------------------

func TestTrimHistoryKeepsCompleteToolCallGroup(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("calling", "a", "b"),
		toolResult("a", "result-a"),
		toolResult("b", "result-b"),
		assistant("done"),
	}
	// Large enough to keep everything.
	got := TrimHistory(in, HistoryBudget{MaxBytes: 1 << 20, MaxTurns: 20})
	assertInvariants(t, in, got)
	if len(got) != len(in) {
		t.Fatalf("nothing should be evicted, got %s", roles(got))
	}
}

func TestTrimHistoryEvictsGroupWholesaleNotPartially(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("old"),
		assistant("old-calls", "x", "y", "z"),
		toolResult("x", "rx"),
		toolResult("y", "ry"),
		toolResult("z", "rz"),
		userMsg("new"),
		assistant("new-reply"),
	}
	// A budget that fits the recent tail but not the whole tool group.
	got := TrimHistory(in, HistoryBudget{MaxBytes: 70})
	assertInvariants(t, in, got)

	// Either the group is entirely present or entirely absent.
	joined := strings.Join(contents(got), ",")
	_, hasX := indexOfContent(got, "rx")
	_, hasY := indexOfContent(got, "ry")
	_, hasZ := indexOfContent(got, "rz")
	if hasX != hasY || hasY != hasZ {
		t.Fatalf("tool group was split: rx=%v ry=%v rz=%v in %s", hasX, hasY, hasZ, joined)
	}
	if hasX {
		t.Fatal("the oversized old group should have been evicted wholesale")
	}
	if !strings.Contains(joined, "new") || !strings.Contains(joined, "new-reply") {
		t.Fatalf("recent turn must survive, got %s", joined)
	}
	if got[0].Content != "sys" {
		t.Fatalf("system message must survive, got %s", roles(got))
	}
}

func TestTrimHistoryNeverOrphansToolResultAcrossManyGroups(t *testing.T) {
	// Ten turns, each with a two-call tool group. Every possible budget is
	// exercised so no cut point can produce an orphan.
	var in []providers.Message
	in = append(in, systemMsg("sys"))
	for i := 0; i < 10; i++ {
		q := string(rune('a' + i))
		in = append(in,
			userMsg("q"+q),
			assistant("call"+q, "t"+q+"1", "t"+q+"2"),
			toolResult("t"+q+"1", "r"+q+"1"),
			toolResult("t"+q+"2", "r"+q+"2"),
			assistant("done"+q),
		)
	}
	for budget := 1; budget < 400; budget += 7 {
		for _, turns := range []int{1, 2, 3, 5, 20} {
			got := TrimHistory(in, HistoryBudget{MaxBytes: budget, MaxTurns: turns})
			assertInvariants(t, in, got)
			if got[0].Content != "sys" {
				t.Fatalf("budget=%d turns=%d: system message lost", budget, turns)
			}
			if !strings.Contains(strings.Join(contents(got), ","), "qj") {
				t.Fatalf("budget=%d turns=%d: current request lost in %s",
					budget, turns, roles(got))
			}
			assertOrderedSuffix(t, in, got)
		}
	}
}

func TestTrimHistoryMultipleParallelToolCallsStayGrouped(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("many", "c1", "c2", "c3", "c4", "c5"),
		toolResult("c1", "r1"),
		toolResult("c2", "r2"),
		toolResult("c3", "r3"),
		toolResult("c4", "r4"),
		toolResult("c5", "r5"),
		assistant("final"),
	}
	for budget := 20; budget < 300; budget += 5 {
		got := TrimHistory(in, HistoryBudget{MaxBytes: budget})
		assertInvariants(t, in, got)
	}
}

func TestTrimHistoryDropsUnownedToolResult(t *testing.T) {
	// A tool result with no owning assistant message can never be
	// retained safely; it must be dropped rather than orphaned.
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("r1"),
		toolResult("missing", "orphan"),
		userMsg("q2"),
		assistant("r2"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 1 << 20, MaxTurns: 20})
	for _, m := range got {
		if m.ToolCallID == "missing" {
			t.Fatalf("unowned tool result must be dropped, got %s", roles(got))
		}
	}
	if n := countUserMessages(got); n != 2 {
		t.Fatalf("both real turns must survive, got %d in %s", n, roles(got))
	}
}

func TestTrimHistoryAllMessagesAreUnownedToolResults(t *testing.T) {
	in := []providers.Message{
		toolResult("a", "ra"),
		toolResult("b", "rb"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 1, MaxTurns: 1})
	// Nothing can be retained safely; the input is returned unchanged
	// rather than being silently emptied.
	if len(got) != len(in) {
		t.Fatalf("expected input returned unchanged, got %d messages", len(got))
	}
}

func TestTrimHistoryTailIsOnlyToolResults(t *testing.T) {
	// A mid-loop snapshot can end on a tool result. The window must still
	// start at a non-tool-result message.
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("calling", "z"),
		toolResult("z", "rz"),
	}
	for budget := 1; budget < 120; budget += 3 {
		got := TrimHistory(in, HistoryBudget{MaxBytes: budget})
		if len(got) == 0 {
			t.Fatalf("budget=%d: result must not be empty", budget)
		}
		if got[0].Role != providers.SystemRole {
			t.Fatalf("budget=%d: expected system message first, got %s", budget, roles(got))
		}
		assertNoOrphanToolResults(t, got)
	}
}

func TestTrimHistoryToolGroupExactlyAtBoundary(t *testing.T) {
	// Two turns, so the tool group belongs to an older turn and is
	// therefore evictable: q2 is the current request and is retained
	// unconditionally, which would pin the whole history if q1 were the
	// only user message.
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1-old"),
		assistant("calling", "g"),
		toolResult("g", "result-g"),
		assistant("old-done"),
		userMsg("q2-current"),
		assistant("new-done"),
	}
	current := messageSize(in[5]) + messageSize(in[6])

	cases := []struct {
		name       string
		budget     int
		wantResult bool
		wantOld    bool
	}{
		{"current turn only", current, false, false},
		{"plus old assistant", current + messageSize(in[4]), false, true},
		{"plus whole group", current + messageSize(in[4]) + messageSize(in[2]) + messageSize(in[3]), true, true},
	}
	for _, tc := range cases {
		got := TrimHistory(in, HistoryBudget{MaxBytes: tc.budget})
		assertInvariants(t, in, got)
		joined := strings.Join(contents(got), ",")
		_, hasResult := indexOfContent(got, "result-g")
		_, hasOld := indexOfContent(got, "old-done")
		if hasResult != tc.wantResult {
			t.Fatalf("%s: group result present=%v, want %v (%s)", tc.name, hasResult, tc.wantResult, joined)
		}
		if hasOld != tc.wantOld {
			t.Fatalf("%s: old assistant present=%v, want %v (%s)", tc.name, hasOld, tc.wantOld, joined)
		}
		// The current request is never lost, whatever the budget.
		if !strings.Contains(joined, "q2-current") || !strings.Contains(joined, "new-done") {
			t.Fatalf("%s: current turn lost (%s)", tc.name, joined)
		}
		if got[0].Content != "sys" {
			t.Fatalf("%s: system message lost (%s)", tc.name, joined)
		}
		assertOrderedSuffix(t, in, got)
	}
}

// --- ordering and immutability --------------------------------------------

func TestTrimHistoryPreservesOrder(t *testing.T) {
	in := []providers.Message{
		systemMsg("s0"),
		userMsg("s1"), assistant("s2", "k"), toolResult("k", "s3"), assistant("s4"),
		userMsg("s5"), assistant("s6", "m"), toolResult("m", "s7"), assistant("s8"),
		userMsg("s9"), assistant("s10"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 120})
	assertOrderedSuffix(t, in, got)
}

func TestTrimHistoryDoesNotMutateInput(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("call", "t1"),
		toolResult("t1", "r1"),
		userMsg("q2"),
		assistant("r2"),
	}
	before := make([]providers.Message, len(in))
	copy(before, in)
	beforeLen := len(in)

	for budget := 1; budget < 200; budget += 5 {
		TrimHistory(in, HistoryBudget{MaxBytes: budget})
		TrimHistory(in, HistoryBudget{MaxTurns: 1})
	}

	if len(in) != beforeLen {
		t.Fatalf("input length changed: %d -> %d", beforeLen, len(in))
	}
	for i := range in {
		if in[i].Role != before[i].Role || in[i].Content != before[i].Content ||
			in[i].ToolCallID != before[i].ToolCallID || len(in[i].ToolCalls) != len(before[i].ToolCalls) {
			t.Fatalf("input mutated at index %d: %+v vs %+v", i, in[i], before[i])
		}
	}
}

func TestTrimHistoryDoesNotAliasInput(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("r1"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 1 << 20})
	if len(got) == 0 || len(in) == 0 {
		t.Fatal("unexpected empty result")
	}
	if &got[0] == &in[0] {
		t.Fatal("result aliases the input slice")
	}
	// Mutating the result must not affect the input.
	original := in[0].Content
	got[0].Content = "MUTATED"
	if in[0].Content != original {
		t.Fatal("mutating the result changed the input")
	}
}

func TestTrimHistoryInputUnchangedWhenResultIsTruncated(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"),
		assistant("r1"),
		userMsg("q2"),
		assistant("r2"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 20})
	if len(got) >= len(in) {
		t.Skip("budget did not trim; aliasing covered by the previous test")
	}
	if in[3].Content != "q2" || in[4].Content != "r2" {
		t.Fatalf("input was modified during trimming: %s", roles(in))
	}
}

// --- determinism -----------------------------------------------------------

func TestTrimHistoryIsDeterministic(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		userMsg("q1"), assistant("c1", "a"), toolResult("a", "ra"), assistant("d1"),
		userMsg("q2"), assistant("c2", "b"), toolResult("b", "rb"), assistant("d2"),
		userMsg("q3"), assistant("d3"),
	}
	first := roles(TrimHistory(in, HistoryBudget{MaxBytes: 90, MaxTurns: 2}))
	for i := 0; i < 50; i++ {
		if got := roles(TrimHistory(in, HistoryBudget{MaxBytes: 90, MaxTurns: 2})); got != first {
			t.Fatalf("non-deterministic result:\n%s\nvs\n%s", first, got)
		}
	}
}

// --- realistic runtime shape ----------------------------------------------

func TestTrimHistoryRealisticAgentLoopHistory(t *testing.T) {
	// Mirrors what runtime.run accumulates: a system prompt, then several
	// user turns each with tool groups, then the current turn.
	in := []providers.Message{
		systemMsg("SYSTEM"),
		userMsg("add a health endpoint"),
		assistant("let me look", "s1"),
		toolResult("s1", "found internal/runtime"),
		assistant("found it"),
		userMsg("now write the test"),
		assistant("reading", "r1", "r2"),
		toolResult("r1", "body of truncate.go"),
		toolResult("r2", "body of truncate_test.go"),
		assistant("writing the test"),
		userMsg("also update the docs"),
		assistant("reading docs", "d1"),
		toolResult("d1", "body of PHASE_2E doc"),
		assistant("", "d2"),
		toolResult("d2", "body of Index.md"),
	}
	got := TrimHistory(in, HistoryBudget{MaxBytes: 200, MaxTurns: 2})
	assertInvariants(t, in, got)
	assertOrderedSuffix(t, in, got)
	if got[0].Content != "SYSTEM" {
		t.Fatalf("system prompt lost: %s", roles(got))
	}
	if got[len(got)-1].ToolCallID != "d2" {
		t.Fatalf("latest tool result lost: %s", roles(got))
	}
	if n := countUserMessages(got); n > 2 {
		t.Fatalf("turn bound violated: %d turns in %s", n, roles(got))
	}
}

// --- pinning the current request and the freshest tool group --------------

// toolLoopHistory builds one user request followed by n assistant/tool
// groups, the shape an agent loop produces while a single request is still
// being worked on. Results are given real bulk so byte budgets have
// something to bite on.
func toolLoopHistory(n int) []providers.Message {
	in := []providers.Message{systemMsg("sys"), userMsg("do the whole job")}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c%d", i)
		in = append(in,
			assistant("calling-"+id, id),
			toolResult(id, "result-"+id+strings.Repeat("x", 256)),
		)
	}
	return in
}

// groupSize is the measured cost of the messages in in[lo:hi], which for
// a tool loop is one assistant/tool-result group.
func groupSize(t *testing.T, in []providers.Message, lo, hi int) int {
	t.Helper()
	total := 0
	for _, m := range in[lo:hi] {
		total += messageSize(m)
	}
	return total
}

// TestTrimHistoryEvictsOlderToolGroupsWithinOneTurn is the regression test
// for the defect that made context_budget inert during tool loops: every
// message a request produces sits after that request's unit, so retaining
// a contiguous range from the request pinned the entire loop and the byte
// budget had nothing left to evict. Selection is per unit, so the budget
// must bite here.
func TestTrimHistoryEvictsOlderToolGroupsWithinOneTurn(t *testing.T) {
	const groups = 4
	in := toolLoopHistory(groups)

	// The newest group is the last two messages; the current request is the
	// only user message and is always pinned.
	newest := groupSize(t, in, len(in)-2, len(in))
	previous := groupSize(t, in, len(in)-4, len(in)-2)

	cases := []struct {
		name        string
		budget      int
		wantEvicted []string
		wantKept    []string
	}{
		{"room for every group", newest + previous + previous + previous, nil, []string{"result-c0", "result-c1", "result-c2", "result-c3"}},
		{"room for three groups", newest + previous + previous, []string{"result-c0"}, []string{"result-c1", "result-c2", "result-c3"}},
		{"room for two groups", newest + previous, []string{"result-c0", "result-c1"}, []string{"result-c2", "result-c3"}},
		{"room for one group", newest, []string{"result-c0", "result-c1", "result-c2"}, []string{"result-c3"}},
	}

	for _, tc := range cases {
		got := TrimHistory(in, HistoryBudget{MaxBytes: tc.budget})
		assertInvariants(t, in, got)
		assertOrderedSuffix(t, in, got)

		joined := strings.Join(contents(got), ",")
		for _, want := range tc.wantKept {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s: %s must be retained, got %s", tc.name, want, joined)
			}
		}
		for _, gone := range tc.wantEvicted {
			if strings.Contains(joined, gone) {
				t.Fatalf("%s: %s should have been evicted, got %s", tc.name, gone, joined)
			}
		}
		// Both pins survive every budget, including the tightest.
		if !strings.Contains(joined, "do the whole job") {
			t.Fatalf("%s: current request evicted (%s)", tc.name, joined)
		}
		if !strings.Contains(joined, "result-c3") {
			t.Fatalf("%s: freshest tool result evicted (%s)", tc.name, joined)
		}
	}
}

// TestTrimHistoryBudgetBoundsASingleToolLoop is the quantitative version:
// retained bytes must now track the budget instead of the request size.
// Only the two pins may exceed it.
func TestTrimHistoryBudgetBoundsASingleToolLoop(t *testing.T) {
	const groups = 12
	in := toolLoopHistory(groups)
	request := messageSize(in[1])
	newest := groupSize(t, in, len(in)-2, len(in))
	full := 0
	for _, m := range in {
		full += messageSize(m)
	}

	// Budgets are fractions of the full history so every case has something
	// to evict regardless of how the content is sized.
	for _, frac := range []int{32, 16, 8, 4} {
		budget := full / frac
		got := TrimHistory(in, HistoryBudget{MaxBytes: budget})
		assertInvariants(t, in, got)
		assertOrderedSuffix(t, in, got)

		retained := 0
		for _, m := range got {
			retained += messageSize(m)
		}
		// Only the two pins may push past the budget.
		if ceiling := budget + request + newest; retained > ceiling {
			t.Fatalf("budget=%d: retained %d bytes, want <= %d (budget plus the two pins)", budget, retained, ceiling)
		}
		// And the budget must actually remove something.
		if retained >= full {
			t.Fatalf("budget=%d: retained %d bytes of %d; nothing was evicted", budget, retained, full)
		}
	}
}

// TestTrimHistoryKeepsRequestWhenTheRunStopsShort pins the one gap the
// selection may introduce: the retained recent run can end before the
// current request, and the request is then re-added on its own rather than
// dragging the evicted middle back in.
func TestTrimHistoryKeepsRequestWhenTheRunStopsShort(t *testing.T) {
	in := toolLoopHistory(5)
	newest := groupSize(t, in, len(in)-2, len(in))

	got := TrimHistory(in, HistoryBudget{MaxBytes: newest})
	assertInvariants(t, in, got)
	assertOrderedSuffix(t, in, got)

	joined := strings.Join(contents(got), ",")
	if !strings.Contains(joined, "do the whole job") {
		t.Fatalf("current request lost when the run stopped short of it: %s", joined)
	}
	if !strings.Contains(joined, "result-c4") {
		t.Fatalf("freshest result lost: %s", joined)
	}
	// Exactly one hole: the run and the request, nothing in between.
	if n := len(got); n != 4 { // system + request + assistant + result
		t.Fatalf("expected system, request and one group, got %d messages (%s)", n, roles(got))
	}
}

// TestTrimHistoryWithoutAnyUserMessage covers history that contains no
// user message, where there is no current request to pin. There must be no
// request to satisfy and no out-of-range access.
func TestTrimHistoryWithoutAnyUserMessage(t *testing.T) {
	in := []providers.Message{
		systemMsg("sys"),
		assistant("thinking", "a"),
		toolResult("a", "r"),
		assistant("done"),
	}
	for _, budget := range []HistoryBudget{{MaxBytes: 1}, {MaxBytes: 40}, {MaxBytes: 1 << 20}, {MaxTurns: 1}} {
		got := TrimHistory(in, budget)
		assertInvariants(t, in, got)
		assertOrderedSuffix(t, in, got)
		if len(got) == 0 {
			t.Fatalf("budget=%+v: result must never be empty", budget)
		}
		if got[0].Content != "sys" {
			t.Fatalf("budget=%+v: system prompt lost (%s)", budget, roles(got))
		}
	}
}

// TestTrimHistoryWithoutUserMessageDoesNotPinOldestUnit is the regression
// test for the defect where lastUserUnit reported 0 when the history held
// no user message. That made TrimHistory pin the OLDEST unit as though it
// were the current request, so an oversized oldest unit was force-retained
// and the byte budget was silently ignored.
func TestTrimHistoryWithoutUserMessageDoesNotPinOldestUnit(t *testing.T) {
	oversized := strings.Repeat("B", 2000)
	in := []providers.Message{
		systemMsg("sys"),
		assistant(oversized), // oldest unit; no user request exists anywhere
		assistant("small tail"),
	}

	got := TrimHistory(in, HistoryBudget{MaxBytes: 20})
	assertInvariants(t, in, got)
	assertOrderedSuffix(t, in, got)

	if _, pinned := indexOfContent(got, oversized); pinned {
		t.Fatalf("oversized oldest unit was pinned despite a 20-byte budget (%d messages retained)", len(got))
	}
	if _, ok := indexOfContent(got, "small tail"); !ok {
		t.Fatalf("newest unit must be retained (%d messages)", len(got))
	}
	if got[0].Content != "sys" {
		t.Fatalf("system prompt lost (%d messages)", len(got))
	}
	if n := budgetBodyBytes(got); n > 20 {
		t.Fatalf("retained %d bytes of history against a 20-byte budget", n)
	}
}

// --- helpers ---------------------------------------------------------------

// assertOrderedSuffix verifies that the retained history is a subsequence
// of the input appearing in the same relative order, which is what
// "preserve message order" means once old turns are evicted.
func assertOrderedSuffix(t *testing.T, in, got []providers.Message) {
	t.Helper()
	i := 0
	for _, m := range got {
		found := false
		for ; i < len(in); i++ {
			if in[i].Role == m.Role && in[i].Content == m.Content && in[i].ToolCallID == m.ToolCallID {
				found = true
				i++
				break
			}
		}
		if !found {
			t.Fatalf("result is not an ordered subsequence of the input at %q; got %s", m.Content, roles(got))
		}
	}
}

func countUserMessages(msgs []providers.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == providers.UserRole {
			n++
		}
	}
	return n
}

func indexOfContent(msgs []providers.Message, want string) (int, bool) {
	for i, m := range msgs {
		if m.Content == want {
			return i, true
		}
	}
	return -1, false
}

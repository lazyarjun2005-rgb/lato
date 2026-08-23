// Planning support for bounded multi-step tasks.
//
// M10 adds a PLAN → ACT → OBSERVE → REPLAN cycle on top of the existing
// tool loop without creating a second execution mechanism: complex
// requests receive a compact task protocol in their system prompt, and
// every request — simple or complex — runs inside hard bounds that stop
// cleanly when the model keeps going for too long or repeats itself.
package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Execution bounds. maxAgentTurns caps how many MODEL turns one user
// request may consume (each tool round-trip costs one turn); the repeat
// thresholds bound identical consecutive tool calls before Lato steers,
// then stops, the agent.
//
// These are the MEDIUM-effort (balanced) values and the hard ceilings
// for profile derivation: every effort profile is bounded, and no
// effort level can remove these limits — see internal/runtime/effort.go.
const (
	maxAgentTurns    = 12
	repeatNudgeAfter = 3
	repeatStopAfter  = 4
)

// taskVerbs are action verbs that mark imperative, work-like requests.
// Question verbs ("explain", "describe") are deliberately absent so
// ordinary questions stay lightweight.
var taskVerbs = []string{
	"add", "implement", "create", "write", "build", "refactor",
	"fix", "debug", "test", "run", "update", "migrate", "install",
	"remove", "rename", "extend", "integrate",
}

// taskMarkers are phrases that signal multi-step intent even when the
// wording is short.
var taskMarkers = []string{
	" then ", "after that", "make sure", "and verify", "verify that",
	"verify the", "and fix", "fix any", "until it passes",
	"until all tests", "step by step", "1.", "2.",
}

// isComplexTask reports whether a user request warrants the multi-step
// task protocol. The decision is deliberately conservative: anything
// conversational or single-action stays simple; only clear multi-action
// goals activate planning.
func isComplexTask(goal string) bool {
	t := strings.ToLower(strings.TrimSpace(goal))
	if t == "" || conversationalTurn(t) {
		return false
	}

	for _, m := range taskMarkers {
		if strings.Contains(t, m) {
			return true
		}
	}

	verbs := 0
	for _, v := range taskVerbs {
		if hasWord(t, v) {
			verbs++
			if verbs >= 3 {
				return true // three distinct actions: clearly multi-step
			}
		}
	}
	// Two actions joined by an explicit connector also count.
	if verbs >= 2 && (strings.Contains(t, " and ") || strings.Contains(t, " then ")) {
		return true
	}
	// A long imperative is treated as a task even with one verb.
	if len(strings.Fields(t)) >= 25 && verbs >= 1 {
		return true
	}
	return false
}

func hasWord(text, word string) bool {
	for _, f := range strings.Fields(strings.ReplaceAll(text, ",", " ")) {
		if f == word {
			return true
		}
	}
	return false
}

// taskDirective is injected into the system prompt for complex tasks.
// It instructs plan-first, observe-each-result behavior and demands a
// marked final summary, while keeping internal reasoning out of the
// transcript: only plans, actions, and results are visible.
const taskDirective = `## Multi-step task protocol

This request needs several coordinated actions.

1. Start by outputting a short numbered plan (three to seven steps, one line each).
2. Execute the steps using tools. After each result, check it before choosing the next step.
3. When a numbered step is finished, output its line once with an [x] prefix, for example: "[x] 2. Implement login handler". Leave unfinished steps unmarked.
4. If a step fails, diagnose from the actual error and adjust the approach. Never rerun an identical failing command unchanged.
5. After modifying files, run targeted verification (format, build, or tests) appropriate to the change before declaring success.
6. Finish with a concise summary beginning with either "Task complete:" or "Task blocked:", listing what was done.

Keep the visible output limited to plans, actions, and results.`

// toolSignature builds a comparable identity for an executed tool call:
// its name plus canonical argument JSON. Identical consecutive
// signatures indicate a loop.
func toolSignature(name string, args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sortStrings(keys)

	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		v, _ := json.Marshal(args[k])
		fmt.Fprintf(&b, "|%s=%s", k, v)
	}
	return b.String()
}

// budgetSummary renders the clean-stop message shown when the agent
// reaches its execution budget.
func budgetSummary(turns int, toolsUsed []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Execution budget reached after %d model turns.", turns)
	if n := len(toolsUsed); n > 0 {
		fmt.Fprintf(&b, " %d tool action(s) were executed (%s).", n, strings.Join(distinct(toolsUsed), ", "))
	} else {
		b.WriteString(" No tool actions were executed.")
	}
	b.WriteString(" Progress so far is preserved above — continue with a follow-up request if needed.")
	return b.String()
}

// repeatSummary renders the clean-stop message for detected loops.
func repeatSummary(toolName string, stopAfter int) string {
	return fmt.Sprintf(
		"Stopped: %s was repeated with identical arguments %d times without progress. The results above are preserved — adjust the approach and continue with a follow-up request.",
		toolName, stopAfter,
	)
}

// steeringMessage is appended once as guidance when repetition starts.
const steeringMessage = "You have now performed the same tool call with identical arguments several times without new information. Change the approach: read different files, fix the underlying cause, or finish with Task blocked: explaining what prevents progress."

func distinct(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

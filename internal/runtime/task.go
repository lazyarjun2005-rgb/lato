// Session/task continuity integration (M12). Complex requests create a
// persistent task record that checkpoints at meaningful boundaries,
// survives interruption, and can be resumed into the SAME M10 loop.
package runtime

import (
	"context"
	"fmt"
	"strings"

	"lato/internal/memory"
	"lato/internal/permissions"
	"lato/internal/providers"
	"lato/internal/task"
)

// resumePhrases are exact normalized utterances treated as "continue my
// previous work". Deliberately narrow so ordinary sentences containing
// "continue" never hijack a request.
var resumePhrases = []string{
	"continue where we left off",
	"resume where we left off",
	"pick up where we left off",
	"continue the previous task",
	"resume the previous task",
	"continue the task",
	"resume the task",
	"continue our task",
	"finish the previous task",
}

// isResumeRequest reports whether the message explicitly asks to resume
// prior work.
func isResumeRequest(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, p := range resumePhrases {
		if t == p || strings.HasPrefix(t, p+" ") {
			return true
		}
	}
	return strings.HasPrefix(t, "continue task ") || strings.HasPrefix(t, "resume task ")
}

// TaskStore resolves the current workspace's task store lazily.
func (r *Runtime) TaskStore() *task.Store {
	root := strings.TrimSpace(r.workspace.Root)
	if root == "" {
		return &task.Store{} // empty, non-persisting
	}
	s, err := task.Load(memory.ProjectID(root))
	if err != nil {
		return &task.Store{}
	}
	return s
}

// ResumableTasks lists this project's resumable tasks (newest first).
func (r *Runtime) ResumableTasks() []task.Task { return r.TaskStore().Resumable() }

// --- tracker ---------------------------------------------------------------

// taskTracker checkpoints structured progress for one complex request.
// For simple requests it is inert: no records are created.
type taskTracker struct {
	rt      *Runtime
	t       *task.Task
	enabled bool
}

func newTaskTracker(rt *Runtime, goal string, existing *task.Task) *taskTracker {
	tr := &taskTracker{rt: rt}
	if strings.TrimSpace(rt.workspace.Root) == "" {
		return tr // no workspace identity: cannot attribute state
	}
	if existing == nil && !isComplexTask(goal) {
		return tr // simple requests stay lightweight
	}
	tr.enabled = true
	store := rt.TaskStore()
	if existing != nil {
		tr.t = existing
	} else if t, err := store.Start(goal); err == nil {
		tr.t = t
	}
	if tr.t != nil {
		tr.t.NoteAction("started")
		tr.checkpoint()
	}
	return tr
}

func (tr *taskTracker) checkpoint() {
	if !tr.enabled || tr.t == nil || tr.rt == nil {
		return
	}
	_ = tr.rt.TaskStore().Save(tr.t)
}

// taskID reports the tracked task's persistent ID, or "" for simple
// requests — the scope key permission approvals are bound to (M13).
func (tr *taskTracker) taskID() string {
	if !tr.enabled || tr.t == nil {
		return ""
	}
	return tr.t.ID
}

// noteDenied records a refused action so a paused/resumed task explains
// why it stopped where it did. The action was NOT executed.
func (tr *taskTracker) noteDenied(action permissions.Action) {
	if !tr.enabled || tr.t == nil {
		return
	}
	tr.t.NoteAction("permission denied: " + action.Tool + " (" + briefToolArgs(action.Tool, action.Args) + ")")
	if next, ok := tr.t.NextPending(); ok {
		tr.t.NextAction = next.Title
	} else if strings.TrimSpace(tr.t.NextAction) == "" {
		tr.t.NextAction = "replan after the denied action"
	}
	tr.checkpoint()
}

// observePlan captures the model's numbered plan once, turning visible
// plan text into structured steps for accurate Progress reporting.
func (tr *taskTracker) observePlan(content string) {
	if !tr.enabled || tr.t == nil || len(tr.t.Steps) > 0 {
		return
	}
	if tr.t.SetPlanFromText(content) {
		tr.checkpoint()
	}
}

// observeProgress applies the model's "[x] N." step-completion markers
// from each turn, so Progress reflects reported reality. It checkpoints
// only when something changed.
func (tr *taskTracker) observeProgress(content string) {
	if !tr.enabled || tr.t == nil || len(tr.t.Steps) == 0 {
		return
	}
	if tr.t.ProgressFromText(content) {
		tr.checkpoint()
	}
}

// observeTool records the boundary after every executed tool call.
func (tr *taskTracker) observeTool(name string, args map[string]any, result ToolResult) {
	if !tr.enabled || tr.t == nil {
		return
	}
	tr.t.NoteAction(name + ": " + briefToolArgs(name, args))
	switch name {
	case "edit_file", "create_file", "write_file":
		if p, ok := args["path"].(string); ok {
			tr.t.AddChangedFile(p)
		}
	case "run_command":
		cmd, _ := args["command"].(string)
		outcome := "failed"
		if result.Success && !result.IsError {
			outcome = "passed"
		}
		tr.t.SetVerification(strings.TrimSpace(cmd) + " → " + outcome)
	}
	tr.checkpoint()
}

// finish marks the honest end state for this request and returns the
// compact preview block appended to the final answer:
//
//   - a visible "Task blocked:" marker or failed verification keeps the
//     task blocked (resumable), never completed;
//   - completion requires positive evidence that work was executed:
//     at least one tool ran, something was verified, or every planned
//     step was reported done. Otherwise the task stays paused —
//     announcing success over unexecuted work would be a lie.
func (tr *taskTracker) finish(finalText string, toolsUsed int) string {
	if !tr.enabled || tr.t == nil {
		return ""
	}
	switch {
	case hasBlockedMarker(finalText), tr.t.VerificationOutcome() == "fail":
		return tr.finishBlocked()
	case tr.hasExecutionEvidence(toolsUsed):
		tr.t.Status = task.StatusCompleted
		tr.checkpoint()
		return "\n\n" + tr.t.Preview()
	default:
		return tr.finishPaused()
	}
}

// hasExecutionEvidence reports whether the run demonstrably did work.
func (tr *taskTracker) hasExecutionEvidence(toolsUsed int) bool {
	if toolsUsed > 0 || strings.TrimSpace(tr.t.Verification) != "" {
		return true
	}
	if _, open := tr.t.NextPending(); open {
		return false // planned steps remain untouched
	}
	return len(tr.t.Steps) > 0 // every captured step was reported done
}

// finishBlocked records blocked state (still resumable) and renders the
// preview.
func (tr *taskTracker) finishBlocked() string {
	tr.t.Status = task.StatusBlocked
	if strings.TrimSpace(tr.t.NextAction) == "" {
		if next, ok := tr.t.NextPending(); ok {
			tr.t.NextAction = next.Title
		} else {
			tr.t.NextAction = "address the blocker above"
		}
	}
	tr.checkpoint()
	return "\n\n" + tr.t.Preview()
}

// hasTerminalMarker reports whether the model output contains its
// visible M10 protocol conclusion ("Task complete: …" / "Task
// blocked: …"). Any line may carry it — models commonly emit [x]
// progress markers before the concluding line.
func hasTerminalMarker(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(line, "task complete:") || strings.HasPrefix(line, "task blocked:") {
			return true
		}
	}
	return false
}

// hasBlockedMarker reports whether the output contains the visible
// failure marker from the M10 directive ("Task blocked: …").
func hasBlockedMarker(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "task blocked:") {
			return true
		}
	}
	return false
}

// maxStallContinuations bounds how many times ONE request may steer the
// model back to work after a zero-tool-call turn mid-task. It is
// absolute for the whole request — real tool progress does NOT reset it
// — applies identically at every effort level, and keeps the shared
// loop finite: at most this many extra turns can ever be consumed by
// narration.
const maxStallContinuations = 2

// continuationNudge is injected after a stalled turn: the model spoke
// but neither acted nor concluded while plan steps remain open.
const continuationNudge = "You stopped without acting and without concluding the task, and planned steps are still open. " +
	"Continue with the next step now using the appropriate tool. " +
	"End your turn only with \"Task complete: …\" or \"Task blocked: …\"."

// needsContinuation reports whether a zero-tool-call turn looks like a
// mid-task stall rather than a conclusion: the task protocol is active,
// neither terminal marker was given, and planned steps remain open.
func (tr *taskTracker) needsContinuation(finalText string) bool {
	if !tr.enabled || tr.t == nil {
		return false
	}
	if hasTerminalMarker(finalText) {
		return false // explicit conclusion: respect it
	}
	if _, open := tr.t.NextPending(); !open {
		return false // every captured step reported done
	}
	return true
}

// hasCompletionMarker reports whether the output contains the success
// marker from the M10 directive.
func hasCompletionMarker(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "task complete:") {
			return true
		}
	}
	return false
}

// finishPaused keeps interrupted/budget-stopped work resumable.
func (tr *taskTracker) finishPaused() string {
	if !tr.enabled || tr.t == nil {
		return ""
	}
	tr.t.Status = task.StatusPaused
	if next, ok := tr.t.NextPending(); ok {
		tr.t.NextAction = next.Title
	} else {
		tr.t.NextAction = "review results above"
	}
	tr.checkpoint()
	return "\n\n" + tr.t.Preview()
}

func briefToolArgs(name string, args map[string]any) string {
	keys := []string{"command", "path", "query", "content"}
	for _, k := range keys {
		if v, ok := args[k].(string); ok && v != "" {
			v = strings.ReplaceAll(v, "\n", " ")
			if len(v) > 60 {
				v = v[:59] + "…"
			}
			return v
		}
	}
	return ""
}

// --- resume -----------------------------------------------------------------

const resumePromptTemplate = `Continue the previously started task.

%s

The repository may have changed since this task was paused: re-inspect the current state of relevant files before continuing and do not assume earlier observations still hold. Pick up from the appropriate next step, verify your changes, and finish with "Task complete:" or "Task blocked:" plus a short summary.`

func resumePrompt(t task.Task) string {
	goal := t.Goal
	body := fmt.Sprintf("Original goal: %s\n\nSaved progress:\n%s", goal, t.ResumeBrief())
	return strings.TrimSpace(resumePromptTemplate) + "\n\n" + body
}

// ResumeStream continues a saved task through the standard M10 loop.
// idOrEmpty selects a task; with an empty ID exactly one resumable task
// is chosen, otherwise an error lists the options.
func (r *Runtime) ResumeStream(ctx context.Context, idOrPrefix string) (<-chan Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resumable := r.ResumableTasks()
	var chosen task.Task
	switch {
	case len(resumable) == 0:
		return nil, fmt.Errorf("no resumable task for this project")
	case idOrPrefix == "":
		if len(resumable) > 1 {
			return nil, fmt.Errorf("%d resumable tasks — choose one:\n%s",
				len(resumable), formatTaskOptions(resumable))
		}
		chosen = resumable[0]
	default:
		found := false
		for _, tk := range resumable {
			if tk.ID == idOrPrefix || strings.HasPrefix(tk.ID, idOrPrefix) {
				chosen = tk
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no resumable task matching %q — options:\n%s",
				idOrPrefix, formatTaskOptions(resumable))
		}
	}

	store := r.TaskStore()
	chosen.Status = task.StatusActive
	_ = store.Save(&chosen)

	history := []providers.Message{{Role: providers.UserRole, Content: resumePrompt(chosen)}}
	return r.stream(ctx, history, &chosen)
}

func formatTaskOptions(tasks []task.Task) string {
	lines := make([]string, 0, len(tasks))
	for _, t := range tasks {
		lines = append(lines, fmt.Sprintf("  %s — %s (%s)", t.ID[:6], t.Title(), t.Status))
	}
	return strings.Join(lines, "\n")
}

// handleResumeRequest answers natural-language continuation requests
// deterministically: zero tasks explains, one resumes, many list.
func (r *Runtime) handleResumeRequest(ctx context.Context, emit func(Event) bool) {
	resumable := r.ResumableTasks()
	switch {
	case len(resumable) == 0:
		finishPlain(emit, "No resumable task found for this project.")
	case len(resumable) == 1:
		events, err := r.ResumeStream(ctx, resumable[0].ID)
		if err != nil {
			emit(Event{Type: EventError, Err: err})
			return
		}
		for e := range events {
			if !emit(e) {
				return
			}
		}
	default:
		finishPlain(emit, fmt.Sprintf(
			"%d resumable tasks — pick one instead of guessing:\n%s\nUse /task resume <id>.",
			len(resumable), formatTaskOptions(resumable)))
	}
}

func finishPlain(emit func(Event) bool, text string) {
	emit(Event{Type: EventText, Text: text})
	emit(Event{Type: EventDone, Response: &providers.Response{Content: text}})
}

// withPausePreview appends the task's compact status preview to a
// clean-stop message and keeps the task resumable.
func withPausePreview(trk *taskTracker, statusText string) string {
	if pv := trk.finishPaused(); pv != "" {
		return statusText + pv
	}
	return statusText
}

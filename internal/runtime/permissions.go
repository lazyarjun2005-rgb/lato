// Centralized permission enforcement (M13). Every tool call the model
// requests passes through executeTool before it may run: the call is
// classified against the workspace boundary and command-safety rules,
// allowed outright when harmless, confirmed through the user interface
// when risky, and refused with a structured result otherwise. The check
// lives here — around tool execution, never inside providers — so every
// provider backend is constrained identically.
//
// Interactivity has exactly one mechanism: an Asker. The TUI attaches
// one at startup; non-interactive entry points run asker-less, which
// fails safe — anything needing confirmation is refused, never silently
// executed.
package runtime

import (
	"context"
	"fmt"

	"lato/internal/permissions"
	"lato/internal/providers"
	"lato/internal/tools"
)

// PermissionChoice is what a user answered to a confirmation prompt.
type PermissionChoice int

const (
	PermissionDeny PermissionChoice = iota
	PermissionAllowOnce
	PermissionAllowTask
)

// PermissionRequest describes one action awaiting explicit confirmation.
// The runtime creates it and blocks on Answer; the Asker shows it to a
// human, who replies through Respond exactly once.
type PermissionRequest struct {
	Tool    string            // requesting tool
	Summary string            // redacted, human-readable effect description
	Reason  string            // why confirmation is required
	Class   permissions.Class // risk class assigned by the classifier

	reply chan PermissionChoice
}

// NewPermissionRequest builds a standalone request with its answer
// channel. The runtime constructs prompts internally; this constructor
// exists for tests and alternative interfaces implementing Asker.
func NewPermissionRequest(tool, summary, reason string, class permissions.Class) *PermissionRequest {
	return &PermissionRequest{
		Tool:    tool,
		Summary: summary,
		Reason:  reason,
		Class:   class,
		reply:   make(chan PermissionChoice, 1),
	}
}

// Respond delivers the user's choice to the waiting runtime goroutine.
// Only the first answer is used.
func (r *PermissionRequest) Respond(choice PermissionChoice) {
	select {
	case r.reply <- choice:
	default:
	}
}

// Answer returns the channel the decision will arrive on.
func (r *PermissionRequest) Answer() <-chan PermissionChoice { return r.reply }

// Asker resolves confirmation prompts. Implementations must block until
// the user answers or ctx is done, and return PermissionDeny on
// cancellation. A nil Asker means Lato is running non-interactively.
type Asker interface {
	AskPermission(ctx context.Context, req PermissionRequest) PermissionChoice
}

// SetAsker attaches the interactive confirmation mechanism. Passing nil
// restores fail-safe mode.
func (r *Runtime) SetAsker(a Asker) {
	r.askMu.Lock()
	defer r.askMu.Unlock()
	r.asker = a
}

// currentAsker returns the attached confirmation mechanism, if any.
func (r *Runtime) currentAsker() Asker {
	r.askMu.RLock()
	defer r.askMu.RUnlock()
	return r.asker
}

// Permissions renders the compact status block for /permissions.
func (r *Runtime) Permissions() string { return r.perms.Summary() }

// ResetPermissions clears every temporary approval (allow-once,
// allow-for-task, session grants) and returns how many were dropped.
// Grants are memory-only by design, so a fresh process starts clean too:
// resumed tasks re-ask for dangerous actions after any restart.
func (r *Runtime) ResetPermissions() int { return r.perms.Reset() }

// refusal builds the structured tool result the agent loop receives when
// an action may not run. The model observes it like any other tool
// outcome and can replan — there is no way for it to bypass the decision.
func refusal(verdict permissions.Verdict, action permissions.Action) tools.Result {
	var reason string
	switch {
	case verdict.Reason != "" && verdict.Decision == permissions.Deny:
		reason = verdict.Reason
	case verdict.Reason != "":
		reason = "not approved: " + verdict.Reason
	default:
		reason = "no interactive approval mechanism is available in this session"
	}
	content := fmt.Sprintf(
		"Permission denied. %s was NOT executed.\nReason: %s\n"+
			"Choose a different approach that stays inside the workspace and avoids destructive operations.",
		action.Tool, reason,
	)
	return tools.Result{IsError: true, Content: content}
}

// executeTool runs one tool call behind the permission gate. A returned
// error reports a real execution failure (unknown tool, invalid
// arguments, failed I/O); the agent loop feeds it back to the model as
// the call's structured result instead of ending the request. A refusal
// is a normal result the model can observe and react to.
func (r *Runtime) executeTool(ctx context.Context, call providers.ToolCall, trk *taskTracker) (tools.Result, error) {
	if r.perms == nil {
		// Bare test runtimes without a policy keep legacy behavior.
		return r.manager.Execute(ctx, call.Name, call.Arguments)
	}

	taskID := trk.taskID()
	action := r.perms.Classify(call.Name, call.Arguments)
	verdict := r.perms.Decide(action, taskID)

	if verdict.NeedsApproval() {
		verdict = r.confirm(ctx, action, verdict, taskID)
	}
	if verdict.Decision != permissions.Allow {
		trk.noteDenied(action)
		return refusal(verdict, action), nil
	}

	return r.manager.Execute(ctx, call.Name, call.Arguments)
}

// confirm resolves an Ask verdict. Without an Asker it fails safe;
// with one, it blocks until the user answers or ctx is done. A missing
// or negative answer never executes the action.
func (r *Runtime) confirm(ctx context.Context, action permissions.Action, verdict permissions.Verdict, taskID string) permissions.Verdict {
	asker := r.currentAsker()
	if asker == nil {
		return permissions.Verdict{
			Decision: permissions.Deny,
			Class:    verdict.Class,
			Reason:   "action requires approval but no interactive session is attached",
		}
	}

	req := PermissionRequest{
		Tool:    action.Tool,
		Summary: action.Summary,
		Reason:  verdict.Reason,
		Class:   verdict.Class,
		reply:   make(chan PermissionChoice, 1),
	}

	switch asker.AskPermission(ctx, req) {
	case PermissionAllowOnce:
		r.perms.Approve(action.Signature(), permissions.ScopeOnce, taskID)
	case PermissionAllowTask:
		r.perms.Approve(action.Signature(), permissions.ScopeTask, taskID)
	default:
		return permissions.Verdict{
			Decision: permissions.Deny,
			Class:    verdict.Class,
			Reason:   "user declined this action",
		}
	}
	return r.perms.Decide(action, taskID)
}

// Package permissions is Lato's centralized safety boundary (M13).
//
// Every action the model requests — reading a file, editing the
// workspace, running a command — is classified into a risk class and
// checked against the permission policy before it may execute. The
// policy answers with one of three decisions:
//
//	Allow  — execute immediately (harmless reads, normal workspace edits)
//	Deny   — never execute
//	Ask    — require explicit user confirmation first
//
// The classification is provider-independent: it inspects only the tool
// name and arguments, so a model behind Ollama, OpenRouter, or any other
// backend has exactly the same privileges.
//
// Approvals granted through Ask are deliberately temporary: allow-once
// covers one exact action, allow-for-task covers matching actions for
// the lifetime of one task, and both live only in memory. Nothing is
// persisted, so a resumed task re-asks for every dangerous action after
// a restart.
package permissions

import "fmt"

// Class groups actions by their risk level. The zero value is not
// meaningful; use the constants below.
type Class string

const (
	// ClassReadOnly marks actions that observe without changing
	// anything: repository reads, searches, listings, metadata.
	ClassReadOnly Class = "read_only"

	// ClassWorkspaceWrite marks actions that modify files inside the
	// active workspace: create/edit/write/rename/format.
	ClassWorkspaceWrite Class = "workspace_write"

	// ClassCommandExecution marks shell/command-runner invocations.
	// Safety within the class depends on the command itself.
	ClassCommandExecution Class = "command_execution"

	// ClassHighRisk marks destructive or boundary-breaking actions:
	// deletions, recursive wipes, destructive git operations, writes
	// outside the workspace.
	ClassHighRisk Class = "high_risk"
)

// Decision is what the policy instructs the caller to do with an action.
type Decision int

const (
	// Allow executes the action without user interaction.
	Allow Decision = iota
	// Deny refuses the action; it must not be executed.
	Deny
	// Ask requires explicit user confirmation before execution.
	Ask
)

// String renders the decision for transcripts and logs.
func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Ask:
		return "ask"
	default:
		return fmt.Sprintf("decision(%d)", int(d))
	}
}

// Action describes one model-requested action awaiting authorization.
// It carries enough context for a human to decide: what will happen,
// where, and why it was classified the way it was. Summary and Reason
// must already be free of secrets.
type Action struct {
	Tool    string         // requesting tool, e.g. run_command
	Args    map[string]any // raw arguments as requested by the model
	Summary string         // one-line human description of the effect
	Reason  string         // why the classifier assigned its class

	// signature is the approval-matching identity computed by the
	// classifier (tool + canonical target). Empty falls back to Tool.
	signature string

	// class is the base risk bucket assigned by the classifier; the
	// policy may escalate it when boundaries are violated.
	class Class

	// inWorkspace reports whether every filesystem target resolves
	// inside the workspace boundary (true for actions without paths).
	inWorkspace bool

	// dirOutside flags run_command working directories outside the
	// workspace (the runner would refuse them anyway).
	dirOutside bool
}

// Signature returns a stable identity for approval matching: two calls
// with the same signature describe the same effect. Signatures fold
// whitespace in command lines but keep targets distinct, so approving
// one deletion does not silently approve a different one.
func (a Action) Signature() string {
	if sig := a.signature; sig != "" {
		return sig
	}
	return a.Tool
}

// Verdict couples a Decision with its classification and explanation.
type Verdict struct {
	Decision Decision
	Class    Class
	Reason   string
}

// NeedsApproval reports whether the verdict requires confirmation.
func (v Verdict) NeedsApproval() bool { return v.Decision == Ask }

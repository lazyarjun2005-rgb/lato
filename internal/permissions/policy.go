// Policy: classification plus temporary approvals. The policy is the
// single authority the runtime consults before executing any tool call.
package permissions

import (
	"fmt"
	"strings"
	"sync"
)

// Policy decides actions against one workspace boundary. It is safe for
// concurrent use; the runtime's agent loop is the only caller in
// practice, but /permissions reads it from another goroutine.
type Policy struct {
	mu       sync.Mutex
	boundary Boundary
	once     map[string]bool            // unconsumed allow-once signatures
	task     map[string]map[string]bool // taskID -> approved signatures
	session  map[string]bool            // approvals for requests with no task
}

// NewPolicy returns a policy guarding root.
func NewPolicy(root string) *Policy {
	return &Policy{
		boundary: NewBoundary(root),
		once:     map[string]bool{},
		task:     map[string]map[string]bool{},
		session:  map[string]bool{},
	}
}

// Root reports the guarded workspace root.
func (p *Policy) Root() string { return p.boundary.Root() }

// Classify builds an Action for a tool invocation, including its risk
// class and approval signature. Summaries are secret-redacted.
func (p *Policy) Classify(tool string, args map[string]any) Action {
	a := classifyAction(p.boundary, tool, args)
	a.Summary = RedactSecrets(a.Summary)
	a.Reason = RedactSecrets(a.Reason)
	return a
}

// Decide returns the verdict for action in the scope of taskID (empty
// when the current request has no tracked task). Existing approvals
// short-circuit an Ask; nothing else escalates a Deny.
func (p *Policy) Decide(a Action, taskID string) Verdict {
	if p.approved(a.Signature(), taskID) {
		scope := "session"
		if taskID != "" {
			scope = "task " + taskID
		}
		return Verdict{Decision: Allow, Class: p.classOf(a), Reason: "previously approved for this " + scope}
	}

	class, decision, reason := p.evaluate(a)
	// The summary was redacted during classification; judge the raw
	// arguments so credential-bearing commands are flagged even though
	// their display is masked.
	if decision == Ask && secretShaped(commandLine(a)) {
		reason += "; review carefully — it contains credential-like text"
	}
	return Verdict{Decision: decision, Class: class, Reason: reason}
}

// evaluate maps an already-classified action to a concrete verdict.
func (p *Policy) evaluate(a Action) (Class, Decision, string) {
	switch a.class {
	case ClassReadOnly:
		if !a.inWorkspace {
			return ClassHighRisk, Ask, orDefault(a.Reason,
				"target is outside the workspace boundary")
		}
		return ClassReadOnly, Allow, orDefault(a.Reason, "read-only inspection")

	case ClassWorkspaceWrite:
		if !a.inWorkspace {
			return ClassHighRisk, Ask, orDefault(a.Reason,
				"target is outside the workspace boundary")
		}
		return ClassWorkspaceWrite, Allow, orDefault(a.Reason, "normal modification inside the workspace")

	case ClassCommandExecution:
		class, decision, reason := classifyCommand(commandLine(a))
		if a.dirOutside && decision != Deny {
			return ClassHighRisk, Ask,
				orDefault(reason, "command") + "; working directory resolves outside the workspace"
		}
		return class, decision, reason

	case ClassHighRisk:
		return ClassHighRisk, Ask, orDefault(a.Reason, "this operation can destroy work")

	default:
		return ClassHighRisk, Ask, "unrecognized action requires confirmation"
	}
}

func (p *Policy) classOf(a Action) Class {
	if a.class == "" {
		return ClassHighRisk
	}
	return a.class
}

// Approve records a user grant. Scope once consumes on first matching
// action; scope task binds to one task; scope session covers this Lato
// process only when no task is active. Nothing is ever persisted.
type ApprovalScope int

const (
	ScopeOnce ApprovalScope = iota
	ScopeTask
	ScopeSession
)

// Approve stores a grant made through the confirmation UI.
func (p *Policy) Approve(sig string, scope ApprovalScope, taskID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch scope {
	case ScopeOnce:
		p.once[sig] = true
	case ScopeTask:
		if taskID == "" {
			taskID = "_session_"
		}
		if p.task[taskID] == nil {
			p.task[taskID] = map[string]bool{}
		}
		p.task[taskID][sig] = true
	default:
		p.session[sig] = true
	}
}

// approved checks stored grants. Allow-once entries are consumed here.
func (p *Policy) approved(sig, taskID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.once[sig] {
		delete(p.once, sig)
		return true
	}
	for _, id := range []string{taskID, "_session_"} {
		if id != "" && p.task[id][sig] {
			return true
		}
	}
	if taskID == "" && p.session[sig] {
		return true
	}
	return false
}

// Reset clears every temporary grant. Called by /permissions reset; also
// the reason resumed tasks re-ask after restart: grants never reach disk.
func (p *Policy) Reset() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.once) + len(p.session)
	for _, m := range p.task {
		n += len(m)
	}
	p.once = map[string]bool{}
	p.task = map[string]map[string]bool{}
	p.session = map[string]bool{}
	return n
}

// Counts summarizes active grants for /permissions.
func (p *Policy) Counts() (once, session int, tasks []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	once = len(p.once)
	session = len(p.session)
	for id, m := range p.task {
		if len(m) > 0 && id != "_session_" {
			tasks = append(tasks, id)
		}
	}
	sortStrings(tasks)
	return once, session, tasks
}

// Summary renders the compact status block shown by /permissions.
// PendingApproval is always "none" between runs: confirmation is
// synchronous inside the agent loop, so nothing can be left waiting.
func (p *Policy) Summary() string {
	var b strings.Builder
	b.WriteString("Permission policy\n")
	fmt.Fprintf(&b, "Workspace: %s\n", p.Root())
	b.WriteString("Pending approval: none\n")

	onceN, sessionN, tasks := p.Counts()
	total := onceN + sessionN + len(tasks)
	if total == 0 {
		b.WriteString("Task approvals: 0\n")
	} else {
		if len(tasks) > 0 {
			fmt.Fprintf(&b, "Task approvals: %d\n", len(tasks))
		}
		if onceN > 0 || sessionN > 0 {
			fmt.Fprintf(&b, "One-time/session approvals: %d\n", onceN+sessionN)
		}
	}
	b.WriteString("Reads and normal workspace edits run automatically;\n" +
		"destructive actions and unusual commands ask first.\n" +
		"Approvals are temporary and never persisted across restarts.")
	return b.String()
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

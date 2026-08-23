// Package command implements Lato's slash-command system for the
// interactive chat (e.g. /help, /clear, /model, /exit).
//
// The package is deliberately independent of the TUI and the runtime: it
// knows how to parse a line, look up a command by name or alias, and run
// it against a small Context interface. Nothing in here imports Bubble
// Tea. That keeps every piece, parsing, lookup, suggestion, dispatch,
// unit-testable in isolation, and it means the TUI is the only thing
// that has to know how a Context is actually implemented.
//
// Adding a new command never requires touching this package or any
// existing command: define a type that satisfies Command and register an
// instance of it at startup.
package command

import (
	"lato/internal/index"
	"lato/internal/session"
	"lato/internal/workspace"
)

// Context is the set of operations a Command is allowed to perform on
// the session. It exists so commands never need to know about Bubble
// Tea, the runtime, or any other concrete type — only this interface.
// The interactive TUI model is the production implementation; tests can
// supply a small fake instead.
type Context interface {
	Println(format string, args ...any)
	Clear()
	Quit()
	Model() string
	Provider() string
	SetModel(name string) error
	SetProvider(name string) error

	// CurrentEffort reports the active effort label; SetEffort switches
	// to a ladder level, persisting it as the default when asked (M16).
	CurrentEffort() string
	SetEffort(level string, persist bool) error
	OpenSessionPicker(sessions []session.Session)
	OpenProviderPicker()
	OpenModelPicker()
	OpenConnectFlow()
	OpenImportFlow()
	OpenAddModelFlow()
	RefreshModels() error
	LatestResponse() string
	TranscriptText() string
	WriteToClipboard(text string) error
	MemorySummary() string
	RememberFact(text string) error
	ForgetMemory(id string) error
	ClearMemory() error
	TaskList() string
	ResumeTask(idOrEmpty string) error
	AbandonTask(id string) error
	SkillsSummary() string
	Workspace() workspace.Info
	Index() *index.Index
	PermissionsSummary() string
	ResetPermissions() int

	// SubmitPrompt sends prompt into the existing agent loop as a
	// genuine user turn: it is recorded in the transcript and session,
	// then answered through the same streaming runtime path as normal
	// chat. Development commands use it so they inherit the current
	// model/provider, the tool system, the permission gate, and the
	// loop's bounded, honest termination without owning any of that.
	SubmitPrompt(prompt string) error

	// RenameSession gives the active session a persistent
	// human-readable title and saves it immediately.
	RenameSession(title string) error

	// ClearConversation resets the CURRENT conversation: visible
	// transcript and persisted Messages are emptied while the session
	// itself (ID, CreatedAt, Title) survives. It must refuse while a
	// stream is active. The next request then starts from a clean
	// history.
	ClearConversation() error

	// ExportConversation writes the current conversation to path as a
	// Markdown document and returns the path actually written. An
	// empty path selects a safe default filename. Existing files are
	// never overwritten; failures leave no success claim behind.
	ExportConversation(path string) (string, error)

	// ResumeSession resolves idOrTitle (exact ID, unique ID prefix, or
	// exact Title — never fuzzy, never ambiguous guessing) and makes
	// that session the active conversation, exactly as if it had been
	// chosen in /sessions.
	ResumeSession(idOrTitle string) error

	// RewindConversation removes the most recent turns conversation
	// turns from the CURRENT session and persists the result. It must
	// refuse while a stream is active, validate before mutating, and
	// leave memory and disk untouched when persistence fails. The
	// returned count is the number of turns actually removed.
	RewindConversation(turns int) (int, error)

	// BranchSession creates an independent new session from the
	// current conversation snapshot and switches to it. An empty title
	// selects the derived default ("<current> (branch)"). The original
	// session is never modified; the switch happens only after the
	// branch has been persisted successfully. The returned value is
	// the new session's ID.
	BranchSession(title string) (string, error)
}

// Command is a single slash command. Implementations are independent,
// self-contained units, adding one never means modifying another or the
// dispatch logic that runs them.
type Command interface {
	Name() string
	Aliases() []string
	Description() string
	Usage() string
	Execute(ctx Context, args []string) error
}

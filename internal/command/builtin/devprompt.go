// Development slash commands.
//
// Every command here is a thin, data-driven prompt builder: it turns
// its arguments into a clear instruction and hands it to
// Context.SubmitPrompt, which records it as a genuine user turn and
// streams the answer through the ONE existing agent loop. That is the
// whole mechanism — these commands never call a model or provider,
// never touch the runtime directly, and add no execution logic of
// their own. Everything downstream (tool system, permission gate,
// effort profile, bounded turns, automatic tool-failure recovery,
// honest completion) is inherited by construction.
//
// Adding or removing a development command means adding or removing
// one entry in devCommands — nothing else changes: registration, /help
// grouping, and the slash palette all derive from the registry.
package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// devCommand describes one development command: where its arguments go
// and what the agent should be asked to do with them.
type devCommand struct {
	name        string
	usage       string
	description string
	// directive is the instruction sent to the agent ahead of the
	// user's arguments. It must read naturally on its own, because
	// commands with optional arguments submit it without any request
	// line attached.
	directive   string
	requireArgs bool // true: bare invocation is a usage error
}

func (d devCommand) Name() string        { return d.name }
func (devCommand) Aliases() []string     { return nil }
func (d devCommand) Description() string { return d.description }
func (d devCommand) Usage() string       { return d.usage }

// Execute validates arguments, renders the prompt, and submits it into
// the existing agent loop.
func (d devCommand) Execute(ctx command.Context, args []string) error {
	if d.requireArgs && len(args) == 0 {
		return fmt.Errorf("usage: %s", d.usage)
	}

	prompt := d.directive
	if request := strings.Join(args, " "); request != "" {
		prompt += "\n\nRequest: " + request
	}
	return ctx.SubmitPrompt(prompt)
}

// devCommands is the single source of truth for this command family.
var devCommands = []devCommand{
	{
		name:        "search",
		usage:       "/search <topic>",
		description: "Search the repository for code matching a topic.",
		directive: "Search this repository for the topic below. Start with the search_repo tool, " +
			"follow up with read_repo_file or read_file on the strongest matches, and report " +
			"concrete file paths with line references plus a short summary of what you found.",
		requireArgs: true,
	},
	{
		name:        "explain",
		usage:       "/explain <target>",
		description: "Explain a file, symbol, or concept from real source.",
		directive: "Explain the target below in the context of this repository. Read the relevant " +
			"source files with tools before answering, and ground every claim in the actual code " +
			"you inspected.",
		requireArgs: true,
	},
	{
		name:        "debug",
		usage:       "/debug <symptom>",
		description: "Trace a problem to its root cause with evidence.",
		directive: "Investigate the reported problem in this repository. Use tools to trace or " +
			"reproduce it — read code, run targeted commands — before proposing anything, and " +
			"report the root cause with evidence.",
		requireArgs: true,
	},
	{
		name:        "fix",
		usage:       "/fix <problem>",
		description: "Diagnose, fix, and verify a reported problem.",
		directive: "Diagnose and fix the reported problem in this repository. Inspect first, apply " +
			"the smallest correct change, then verify with the project's build or tests and react " +
			"to any failures before concluding.",
		requireArgs: true,
	},
	{
		name:        "test",
		usage:       "/test [target]",
		description: "Run tests and diagnose failures.",
		directive: "Run the relevant tests in this repository using run_command — the whole suite " +
			"unless a narrower target is given below — diagnose every failure honestly, and report " +
			"what passed, what failed, and why.",
	},
	{
		name:        "build",
		usage:       "/build [target]",
		description: "Build the project and report diagnostics.",
		directive: "Build this project with its own build system using run_command, report all " +
			"diagnostics, and fix only what blocks a successful build.",
	},
	{
		name:        "run",
		usage:       "/run [what]",
		description: "Run something in the workspace and explain the result.",
		directive: "Run what the request below describes inside this workspace using run_command, " +
			"capture its output, and explain the result.",
	},
	{
		name:        "review",
		usage:       "/review [target]",
		description: "Review code or working-tree changes; advisory only.",
		directive: "Review the code described below — or, if nothing specific is given, the current " +
			"working-tree changes (read-only git commands through run_command are fine) — for " +
			"correctness, edge cases, and clarity. Cite file paths and lines, and suggest concrete " +
			"improvements instead of editing files yourself.",
	},
	{
		name:        "refactor",
		usage:       "/refactor <goal>",
		description: "Restructure code without changing behavior.",
		directive: "Refactor this repository as described below without changing behavior. Inspect " +
			"before editing, work incrementally, keep the build and tests green throughout, and " +
			"verify everything you touched at the end.",
		requireArgs: true,
	},
	{
		name:        "code",
		usage:       "/code <task>",
		description: "Implement a task end-to-end with verification.",
		directive: "Implement the request below in this repository. Plan briefly, inspect the " +
			"affected code with tools, make the change, then verify with the project's build or " +
			"tests and finish with an honest Task complete or Task blocked summary.",
		requireArgs: true,
	},
}

// NewDevCommands returns the development command set in display order,
// ready for registry registration.
func NewDevCommands() []command.Command {
	out := make([]command.Command, 0, len(devCommands))
	for _, d := range devCommands {
		out = append(out, d)
	}
	return out
}

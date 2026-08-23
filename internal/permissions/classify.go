// Tool-call classification: turns a model's tool invocation into an
// Action with a risk class, a human-readable summary, and an approval
// signature. This is the only place that knows which Lato tools exist;
// new tools register here and inherit the whole permission mechanism.
package permissions

import (
	"fmt"
	"strings"
)

// toolClasses assigns every known tool its base risk class.
//
//	read_only          observe without changing anything
//	workspace_write    modify files inside the workspace
//	command_execution  run programs
var toolClasses = map[string]Class{
	// Repository inspection (M3–M5): served from the cached index.
	"search_repo":    ClassReadOnly,
	"read_repo_file": ClassReadOnly,
	"load_skill":     ClassReadOnly,

	// Generic filesystem reads.
	"read_file":             ClassReadOnly,
	"list_files":            ClassReadOnly,
	"pwd":                   ClassReadOnly,
	"recall_project_memory": ClassReadOnly, // reading memory (M11)

	// Workspace modifications (M6). Paths are boundary-checked per call.
	"create_file":      ClassWorkspaceWrite,
	"edit_file":        ClassWorkspaceWrite,
	"write_file":       ClassWorkspaceWrite,
	"move_file":        ClassWorkspaceWrite,
	"rename_file":      ClassWorkspaceWrite,
	"format_file":      ClassWorkspaceWrite,
	"create_directory": ClassWorkspaceWrite,

	// Project-memory mutations are bounded, reversible through
	// forget_project_memory, and secret-rejected by the store itself
	// (M11); they never touch repository files.
	"remember_project_fact": ClassWorkspaceWrite,
	"update_project_memory": ClassWorkspaceWrite,
	"forget_project_memory": ClassWorkspaceWrite,

	// Command execution is judged by the command classifier.
	"run_command": ClassCommandExecution,

	// Deletion-style operations start high-risk regardless of target.
	"delete_file":      ClassHighRisk,
	"delete_directory": ClassHighRisk,
}

// pathArgs lists argument keys carrying filesystem targets, most
// significant first, for tools that operate on paths.
var pathArg = map[string]string{
	"read_file": "path", "write_file": "path", "list_files": "path",
	"edit_file": "path", "create_file": "path", "delete_file": "path",
	"rename_file": "path", "move_file": "path", "format_file": "path",
	"create_directory": "path", "delete_directory": "path",
}

// classifyAction inspects one pending tool call. Unknown tools classify
// as high-risk so future tools fail closed until registered here.
func classifyAction(b Boundary, tool string, args map[string]any) Action {
	a := Action{
		Tool:        tool,
		Args:        args,
		inWorkspace: true, // actions without paths don't leave the workspace
		class:       toolClasses[tool],
	}

	if a.class == "" {
		a.class = ClassHighRisk
		a.Summary = fmt.Sprintf("Unrecognized action %q", tool)
		a.Reason = "tool is not part of Lato's known tool set; refusing to run it unattended"
		return a
	}

	switch {
	case tool == "run_command":
		return classifyRunCommand(b, args)

	case pathArg[tool] != "":
		p, _ := args[pathArg[tool]].(string)
		return classifyPathAction(b, a, p)
	}

	// Tools without filesystem targets get stable summaries.
	switch {
	case strings.HasPrefix(tool, "remember_") || strings.HasPrefix(tool, "update_"):
		a.Summary = "Update project memory"
	case strings.HasPrefix(tool, "forget_"):
		a.Summary = "Remove a project memory entry"
	case tool == "recall_project_memory":
		a.Summary = "Read project memory"
	case tool == "search_repo":
		q, _ := args["query"].(string)
		a.Summary = "Search the repository" + forTarget(q)
		a.signature = "search_repo:" + collapseSpace(q)
	case tool == "load_skill":
		id, _ := args["id"].(string)
		a.Summary = "Load skill instructions" + forTarget(id)
	default:
		a.Summary = fmt.Sprintf("Run %s", tool)
	}
	if a.class == ClassReadOnly {
		a.Reason = orDefault(a.Reason, "read-only inspection")
	}
	return a
}

// classifyPathAction resolves a path-based action against the workspace
// boundary and describes what it would do.
func classifyPathAction(b Boundary, a Action, rawPath string) Action {
	switch {
	case strings.TrimSpace(rawPath) == "":
		a.inWorkspace = false
		a.Summary = fmt.Sprintf("%s with no target path", describeVerb(a.Tool))
		a.Reason = "missing target path"
		a.signature = a.Tool
		return a
	}

	abs, ok := b.Contains(rawPath)
	rel := displayPath(b.Root(), abs, ok, rawPath)
	a.Summary = fmt.Sprintf("%s %s", describeVerb(a.Tool), rel)
	a.signature = a.Tool + ":" + firstNonEmpty(abs, collapseSpace(normalizeSeparators(rawPath)))

	if !ok {
		a.inWorkspace = false
		a.Reason = fmt.Sprintf("%q resolves outside the workspace (%s)", RedactSecrets(rawPath), b.Root())
		return a
	}

	switch a.class {
	case ClassReadOnly:
		a.Reason = "read-only access inside the workspace"
	case ClassWorkspaceWrite:
		a.Reason = "modifies a file inside the workspace"
	}
	return a
}

// classifyRunCommand builds the action for run_command, including its
// working-directory check. The command line itself is judged by
// classifyCommand during Decide.
func classifyRunCommand(b Boundary, args map[string]any) Action {
	line, _ := args["command"].(string)
	a := Action{Tool: "run_command", Args: args, inWorkspace: true, class: ClassCommandExecution}
	a.Summary = "Run command: " + truncateSummary(collapseSpace(line), 120)
	a.signature = "run_command:" + collapseSpace(line)

	if d, ok := args["dir"].(string); ok && strings.TrimSpace(d) != "" {
		if _, contained := b.Contains(d); !contained {
			a.dirOutside = true
			a.inWorkspace = false
		}
	}
	return a
}

// commandLine extracts the raw command string from run_command args.
func commandLine(a Action) string {
	s, _ := a.Args["command"].(string)
	return s
}

// describeVerb renders what a tool does to a path, in words a user can
// judge at a glance in the confirmation prompt.
func describeVerb(tool string) string {
	switch tool {
	case "create_file":
		return "Create file"
	case "edit_file":
		return "Edit file"
	case "write_file":
		return "Overwrite file"
	case "delete_file":
		return "Delete file"
	case "delete_directory":
		return "Delete directory"
	case "rename_file":
		return "Rename file"
	case "move_file":
		return "Move file"
	case "format_file":
		return "Format file"
	case "create_directory":
		return "Create directory"
	case "read_file":
		return "Read file"
	case "list_files":
		return "List files under"
	default:
		return tool + " on"
	}
}

// displayPath prefers the path relative to the workspace root so prompts
// stay short; falls back to the raw request when resolution failed.
func displayPath(root, abs string, ok bool, raw string) string {
	if ok && root != "" {
		if rel, err := relPath(root, abs); err == nil && rel != "." && rel != "" {
			return "./" + rel
		}
	}
	return RedactSecrets(collapseSpace(raw))
}

func forTarget(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return ": " + truncateSummary(collapseSpace(s), 60)
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Package context assembles a structured description of the repository
// Lato is running inside, for injection into a model prompt when the
// user asks repository-related questions.
//
// It is deliberately narrow: it only *reads* the workspace that was
// already discovered. It never calls a model, never modifies files, and
// contains no UI code. Later milestones (editing, planning, agents) can
// extend it without re-discovering the workspace.
package context

import (
	"fmt"
	"strings"

	"lato/internal/workspace"
)

// GoMod holds the module-level facts read from a go.mod file. It is
// non-nil only for Go projects with a readable go.mod.
type GoMod struct {
	Module   string   // module path from the "module" directive
	Go       string   // go version from the "go" directive
	Requires []string // direct dependencies, formatted "path version"
}

// Context is the assembled repository context for one workspace. It is
// produced by a Builder and treated as read-only afterwards.
type Context struct {
	Workspace workspace.Info
	Readme    string // first readmeLines of README.md, "" if absent
	GoMod     *GoMod // nil unless the project is Go
}

// readmeLines is the maximum number of README lines included in the
// context, so a very large README cannot bloat the prompt.
const readmeLines = 200

// Text renders Context as a single formatted block for injection into a
// system prompt. Only sections with data are included; empty ones are
// dropped so a bare workspace still produces a clean block.
func (c Context) Text() string {
	w := c.Workspace
	add := func(b *[]string, label, value string) {
		if value == "" {
			return
		}
		*b = append(*b, label+"\n"+value)
	}

	var sections []string
	add(&sections, "Repository:", w.Repository)
	add(&sections, "Language:", w.Language)
	add(&sections, "Module:", w.Module)
	add(&sections, "Build:", w.BuildSystem)

	if tree := packageList(w); len(tree) > 0 {
		var b strings.Builder
		for _, d := range tree {
			b.WriteString("- " + d + "\n")
		}
		sections = append(sections, "Tree:\n"+strings.TrimRight(b.String(), "\n"))
	}

	if c.Readme != "" {
		add(&sections, "README Summary:", c.Readme)
	}

	if c.GoMod != nil {
		var b strings.Builder
		if c.GoMod.Module != "" {
			fmt.Fprintf(&b, "Module: %s\n", c.GoMod.Module)
		}
		if c.GoMod.Go != "" {
			fmt.Fprintf(&b, "Go: %s\n", c.GoMod.Go)
		}
		if len(c.GoMod.Requires) > 0 {
			b.WriteString("Dependencies:\n")
			for _, dep := range c.GoMod.Requires {
				fmt.Fprintf(&b, "- %s\n", dep)
			}
		}
		sections = append(sections, "go.mod:\n"+strings.TrimRight(b.String(), "\n"))
	}

	if len(w.ImportantFiles) > 0 {
		var b strings.Builder
		for _, f := range w.ImportantFiles {
			b.WriteString(f + "\n")
		}
		sections = append(sections, "Important Files:\n"+strings.TrimRight(b.String(), "\n"))
	}

	return strings.Join(sections, "\n\n")
}

// RepositoryQuestion reports whether text reads as a request to explain
// or describe the current repository as a whole. Used by the runtime to
// decide whether to inject repository context. The check is a simple,
// deterministic substring match on phrases; it does not call a model.
func RepositoryQuestion(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	for _, phrase := range repositoryPhrases {
		if strings.Contains(t, phrase) {
			return true
		}
	}
	return false
}

// LooksLikeCodeQuestion reports whether text reads as a question about
// the repository or the code in it — either the project as a whole
// (RepositoryQuestion) or a specific part of it ("how does the main
// function work?", "where is fmt.Println used?"). The runtime uses this
// broader signal to decide whether deterministic retrieval should run
// before the model answers. It is deliberately generous: a false
// positive costs a bounded evidence lookup, while a false negative
// leaves the model answering from guesswork.
func LooksLikeCodeQuestion(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if RepositoryQuestion(t) {
		return true
	}
	for _, p := range codeQuestionPatterns {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

// repositoryPhrases are the known whole-repository question patterns.
var repositoryPhrases = []string{
	"explain this repository",
	"explain the repository",
	"explain this project",
	"explain this codebase",
	"explain this code",
	"how does this project work",
	"how does this repository work",
	"how does this codebase work",
	"how is this project structured",
	"how is this repository structured",
	"how is this codebase structured",
	"describe this repository",
	"describe the repository",
	"describe this project",
	"describe this codebase",
	"describe the codebase",
	"what architecture is used",
	"architecture of this project",
	"what does this project do",
	"what does this repository do",
	"what is this codebase",
	"what is this repository",
	"what is this project",
}

// codeQuestionPatterns extend detection to questions about specific
// parts of the code. They match the interrogative shape rather than any
// particular subject, so new subjects need no configuration.
var codeQuestionPatterns = []string{
	"how does ", "how do ", "how is ", "how are ",
	"where is ", "where are ", "where can ", "where do ", "where does ",
	"what does ", "what is ", "what are ",
	"who calls ", "who uses ",
	"explain ", "describe ", "walk me through ", "show me ", "tell me about ",
	"in this repo", "in this repository", "in this project", "in this codebase", "in the code",
	"usage of ", "implementation of ", "definition of ",
}

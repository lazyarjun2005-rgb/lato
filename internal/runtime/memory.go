// Project-memory integration. Memory supplements repository retrieval
// and M10 planning: relevant durable facts are injected into the system
// prompt for the current request, and the agent can persist new facts
// through explicit tools. Storage is per-project, user-level, and
// bounded (see internal/memory).
package runtime

import (
	"strings"

	"lato/internal/memory"
	"lato/internal/providers"
)

// injectLimits bound how much memory one request may carry.
const (
	injectMaxEntries = 8
)

// memoryStore resolves the current workspace's memory store lazily.
// Load errors degrade to an empty in-process store rather than failing
// requests; nothing is persisted until an Add/Update succeeds.
func (r *Runtime) memoryStore() *memory.Store {
	root := strings.TrimSpace(r.workspace.Root)
	if root == "" {
		return &memory.Store{} // no workspace: empty, non-persisting
	}
	s, err := memory.Load(memory.ProjectID(root))
	if err != nil {
		return &memory.Store{}
	}
	return s
}

// Memory returns the workspace's project-memory store (tools and
// commands use this).
func (r *Runtime) Memory() *memory.Store { return r.memoryStore() }

// relevantMemory renders the bounded prompt block of memories related
// to goal, or "" when nothing matches — irrelevant memory is never
// injected.
func (r *Runtime) relevantMemory(goal string) string {
	if strings.TrimSpace(goal) == "" || conversationalTurn(goal) {
		return ""
	}
	return memory.RenderBlock(r.memoryStore().Relevant(goal, injectMaxEntries))
}

// memoryRelevanceCount reports how many entries would be injected for
// goal, for the TUI's concise "Memory: N relevant facts" indication.
func (r *Runtime) memoryRelevanceCount(goal string) int {
	block := r.relevantMemory(goal)
	if block == "" {
		return 0
	}
	return strings.Count(block, "\n- [")
}

// buildMessages assembles the request payload... (see original comment)
// The memory count is returned so StreamChat can surface a status hint.
func (r *Runtime) buildMessages(history []providers.Message) ([]providers.Message, int) {
	system := r.agent.BuildSystemPrompt()

	goal := lastUserMessage(history)

	// Repository context + source evidence for code questions.
	if ctxtext := r.contextFor(history); ctxtext != "" {
		system += "\n\n" + ctxtext
	}

	// Relevant durable project facts (bounded, lexical).
	memCount := 0
	if block := r.relevantMemory(goal); block != "" {
		system += "\n\n" + block
		memCount = strings.Count(block, "\n- [")
	}

	// Multi-step task protocol for complex goals, scaled by the active
	// effort level. Medium injects nothing extra: balanced mode keeps
	// the exact pre-M16 prompt.
	if isComplexTask(goal) {
		system += "\n\n" + taskDirective
		if guide := r.profile().Directive; guide != "" {
			system += "\n\n## Effort: " + r.effort.String() + "\n" + guide
		}
	}

	messages := []providers.Message{
		{
			Role:    providers.SystemRole,
			Content: system,
		},
	}
	messages = append(messages, history...)
	return messages, memCount
}

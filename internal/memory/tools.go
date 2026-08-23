// Memory tools expose project memory to the model as explicit, opt-in
// operations. The agent decides which discoveries are durable enough to
// remember; nothing is stored automatically.
package memory

import (
	"context"
	"fmt"
	"strings"

	"lato/internal/tools"
)

// RememberFact stores a durable project fact discovered by the agent.
type RememberFact struct{ provider Provider }

// NewRememberFact returns a ready-to-register remember tool.
func NewRememberFact(p Provider) *RememberFact { return &RememberFact{provider: p} }

// Provider supplies the current workspace's memory store. The runtime
// implements it; the store is resolved lazily per call so tests and
// workspaces without a root degrade gracefully.
type Provider interface {
	Memory() *Store
}

func (RememberFact) Name() string { return "remember_project_fact" }

func (RememberFact) Description() string {
	return "Persist a durable, reusable fact about this project for future sessions " +
		"(e.g. build commands, directory conventions, technology choices). " +
		"Use only for stable knowledge, not transient task details."
}

func (RememberFact) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The durable fact, stated in one short sentence.",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "One of: architecture, technology, convention, command, structure, decision. Optional.",
			},
		},
		"required": []string{"content"},
	}
}

func (t RememberFact) Execute(ctx context.Context, args map[string]any) (tools.Result, error) {
	content, err := tools.StringArg(args, "content")
	if err != nil {
		return tools.Result{}, err
	}
	category, _ := tools.StringArg(args, "category")
	e, err := t.provider.Memory().Add(content, category, KindDiscovered)
	if err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}
	return tools.Result{Content: fmt.Sprintf("remembered %s [%s]: %s", e.ID, e.Category, e.Content)}, nil
}

type UpdateMemory struct{ provider Provider }

func NewUpdateMemory(p Provider) *UpdateMemory { return &UpdateMemory{provider: p} }

func (UpdateMemory) Name() string { return "update_project_memory" }

func (UpdateMemory) Description() string {
	return "Replace the content of an existing project memory entry by its ID. " +
		"Use when previously remembered information has become outdated."
}

func (UpdateMemory) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "string", "description": "ID of the memory entry to update."},
			"content": map[string]any{"type": "string", "description": "The corrected fact."},
		},
		"required": []string{"id", "content"},
	}
}

func (t UpdateMemory) Execute(ctx context.Context, args map[string]any) (tools.Result, error) {
	id, err := tools.StringArg(args, "id")
	if err != nil {
		return tools.Result{}, err
	}
	content, err := tools.StringArg(args, "content")
	if err != nil {
		return tools.Result{}, err
	}
	e, err := t.provider.Memory().Update(id, content)
	if err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}
	return tools.Result{Content: fmt.Sprintf("updated %s: %s", e.ID, e.Content)}, nil
}

type ForgetMemory struct{ provider Provider }

func NewForgetMemory(p Provider) *ForgetMemory { return &ForgetMemory{provider: p} }

func (ForgetMemory) Name() string { return "forget_project_memory" }

func (ForgetMemory) Description() string {
	return "Delete one project memory entry by ID. Use when a remembered fact is wrong or no longer relevant."
}

func (ForgetMemory) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "ID of the memory entry to delete."},
		},
		"required": []string{"id"},
	}
}

func (t ForgetMemory) Execute(ctx context.Context, args map[string]any) (tools.Result, error) {
	id, err := tools.StringArg(args, "id")
	if err != nil {
		return tools.Result{}, err
	}
	if err := t.provider.Memory().Remove(id); err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}
	return tools.Result{Content: fmt.Sprintf("forgot memory %s", id)}, nil
}

type RecallMemory struct{ provider Provider }

func NewRecallMemory(p Provider) *RecallMemory { return &RecallMemory{provider: p} }

func (RecallMemory) Name() string { return "recall_project_memory" }

func (RecallMemory) Description() string {
	return "List stored project memories relevant to a topic. Use before planning when background about this project would help."
}

func (RecallMemory) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"topic": map[string]any{
				"type":        "string",
				"description": "Topic to recall memories about, e.g. \"testing\" or \"database\".",
			},
		},
		"required": []string{"topic"},
	}
}

func (t RecallMemory) Execute(ctx context.Context, args map[string]any) (tools.Result, error) {
	topic, err := tools.StringArg(args, "topic")
	if err != nil {
		return tools.Result{}, err
	}
	entries := t.provider.Memory().Relevant(topic, 8)
	if len(entries) == 0 {
		return tools.Result{Content: fmt.Sprintf("no stored memories relate to %q", topic)}, nil
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s [%s/%s] %s\n", e.ID, e.Kind, e.Category, e.Content)
	}
	return tools.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

// Register adds all four memory tools to m.
func Register(m *tools.Manager, p Provider) error {
	all := []tools.Tool{
		RememberFact{provider: p},
		NewUpdateMemory(p),
		NewForgetMemory(p),
		NewRecallMemory(p),
	}
	for _, t := range all {
		if err := m.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// Package editing exposes Lato's safe edit engine to the model as
// tools: edit_file for targeted replacements in an existing file and
// create_file for new files. Both operate only inside the target
// workspace and return a diff-style summary so the model can see
// exactly what changed.
package editing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"lato/internal/edit"
	"lato/internal/tools"
)

// maxEditSize caps the file size an edit tool will touch. It matches
// edit.MaxFileSize; files beyond it are refused before any work.
const maxEditSize = edit.MaxFileSize

// EditFile applies targeted replacements to an existing workspace file.
type EditFile struct {
	workspace *edit.Workspace
}

// NewEditFile returns a ready-to-register edit_file tool bound to ws.
func NewEditFile(ws *edit.Workspace) *EditFile {
	return &EditFile{workspace: ws}
}

func (EditFile) Name() string { return "edit_file" }

func (EditFile) Description() string {
	return "Apply targeted replacements to an existing repository file. Provide old_text as the exact " +
		"current text to replace (include enough surrounding lines to be unique) and new_text as its " +
		"replacement. The file is only changed when old_text occurs exactly once; missing or ambiguous " +
		"old text fails safely with no changes. Returns a diff of the change. " +
		fmt.Sprintf("Files larger than %d bytes are not editable.", maxEditSize)
}

func (EditFile) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path of the file relative to the workspace root, using forward slashes.",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "Exact text currently in the file to replace. Must match exactly one location; include surrounding lines for uniqueness.",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "Replacement text. May be empty to delete old_text.",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

func (t *EditFile) Execute(_ context.Context, args map[string]any) (tools.Result, error) {
	path, err := tools.StringArg(args, "path")
	if err != nil {
		return tools.Result{}, err
	}
	oldText, err := tools.StringArg(args, "old_text")
	if err != nil {
		return tools.Result{}, err
	}
	newText, err := tools.StringArg(args, "new_text")
	if err != nil {
		return tools.Result{}, err
	}

	res, err := t.workspace.ReplaceFile(path, edit.Op{Old: oldText, New: newText})
	if err != nil {
		return tools.Result{IsError: true, Content: editFailure(t.workspace, path, err)}, nil
	}
	return tools.Result{Content: describe(res)}, nil
}

// CreateFile writes a brand-new file inside the workspace.
type CreateFile struct {
	workspace *edit.Workspace
}

// NewCreateFile returns a ready-to-register create_file tool bound to ws.
func NewCreateFile(ws *edit.Workspace) *CreateFile {
	return &CreateFile{workspace: ws}
}

func (CreateFile) Name() string { return "create_file" }

func (CreateFile) Description() string {
	return "Create a new file inside the repository with the given content, creating parent directories " +
		"as needed. Fails if the file already exists — use edit_file to change an existing file. " +
		"The path is relative to the workspace root."
}

func (CreateFile) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path of the new file relative to the workspace root, using forward slashes.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full text content for the new file.",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *CreateFile) Execute(_ context.Context, args map[string]any) (tools.Result, error) {
	path, err := tools.StringArg(args, "path")
	if err != nil {
		return tools.Result{}, err
	}
	content, err := tools.StringArg(args, "content")
	if err != nil {
		return tools.Result{}, err
	}

	res, err := t.workspace.CreateFile(path, content)
	if err != nil {
		return tools.Result{IsError: true, Content: editFailure(t.workspace, path, err)}, nil
	}
	return tools.Result{Content: describe(res)}, nil
}

// editFailure renders an edit error for the model: the reason plus how
// to recover.
func editFailure(ws *edit.Workspace, path string, err error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "could not edit %s: %v", path, err)
	switch {
	case errors.Is(err, edit.ErrNotFound):
		b.WriteString("\nRead the file again with read_repo_file and retry with the exact current text.")
	case errors.Is(err, edit.ErrAmbiguous):
		b.WriteString("\nInclude more surrounding lines in old_text so the location is unique.")
	case errors.Is(err, edit.ErrExists):
		b.WriteString("\nThe file already exists; use edit_file to change it instead.")
	case errors.Is(err, edit.ErrInvalidPath):
		fmt.Fprintf(&b, "\nUse a relative path inside the workspace (root: %s).", ws.Root())
	}
	return b.String()
}

// describe renders a successful operation for the model: what happened,
// where, and a unified-style diff so the change is verifiable.
func describe(res edit.Result) string {
	var b strings.Builder
	switch {
	case res.Created:
		fmt.Fprintf(&b, "created %s (%d bytes)", res.Path, len(res.After))
	default:
		fmt.Fprintf(&b, "edited %s", res.Path)
		if !res.Changed() {
			b.WriteString(": replacement matched the existing content; file unchanged")
			return b.String()
		}
		fmt.Fprintf(&b, " (%d replacement(s))", res.Replaced)
	}
	if d := edit.Diff(res.Path, res.Before, res.After); d != "" {
		b.WriteString("\n\n")
		b.WriteString(d)
	}
	return b.String()
}

// Register adds both editing tools to m, confined to ws.
func Register(m *tools.Manager, ws *edit.Workspace) error {
	all := []tools.Tool{
		NewEditFile(ws),
		NewCreateFile(ws),
	}
	for _, t := range all {
		if err := m.Register(t); err != nil {
			return err
		}
	}
	return nil
}

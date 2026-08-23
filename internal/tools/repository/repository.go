// Package repository provides tools that let the agent query the
// workspace index without sending the whole repository to the model.
//
// The tools are deterministic and local: search_repo runs against the
// in-memory index, and read_repo_file returns the cached text of an
// indexed source file. They operate through the narrow Store interface
// so they stay decoupled from the runtime (no import cycle), while the
// runtime satisfies Store by delegating to its cached index.
package repository

import (
	"context"
	"fmt"
	"strings"

	"lato/internal/index"
	"lato/internal/tools"
)

// Store is the minimal set of index-backed operations the repository
// tools need. The runtime implements it; tests can supply a fake.
type Store interface {
	Search(opts index.Search) (index.SearchResult, error)
	ReadIndexedFile(ctx context.Context, relPath string) (string, error)
}

// SearchRepository searches the workspace index.
type SearchRepository struct {
	store Store
}

// NewSearchRepository returns a ready-to-register search tool.
func NewSearchRepository(store Store) *SearchRepository {
	return &SearchRepository{store: store}
}

func (SearchRepository) Name() string { return "search_repo" }

func (SearchRepository) Description() string {
	return "Search the repository for file names, paths, Go symbols, and file contents. " +
		"Content search reads source files and reports matching line numbers with a snippet, " +
		"so use it to find where code lives — e.g. search for \"fmt.Println\" or a function name. " +
		"Results are ranked with content matches first; read_repo_file then shows the full file."
}

func (SearchRepository) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Text to search for. Matches file names, paths, Go symbols, and file contents (case-insensitive by default).",
			},
			"contents": map[string]any{
				"type":        "boolean",
				"description": "Also search file contents for the query text. Enabled by default; set false to search names and paths only.",
			},
			"symbols": map[string]any{
				"type":        "boolean",
				"description": "When true, also match Go symbol names (functions, methods, structs, interfaces, types) with declaration line numbers.",
			},
			"case_sensitive": map[string]any{
				"type":        "boolean",
				"description": "Match case exactly instead of the default case-insensitive matching.",
			},
			"max": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return. Defaults to 20.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchRepository) Execute(_ context.Context, args map[string]any) (tools.Result, error) {
	query, err := tools.StringArg(args, "query")
	if err != nil {
		return tools.Result{}, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return tools.Result{IsError: true, Content: "search query cannot be empty"}, nil
	}

	max, err := tools.OptionalIntArg(args, "max", 0)
	if err != nil {
		return tools.Result{}, err
	}
	// Content search is on by default: most model queries ("where is X
	// called?", "find fmt.Println") are content queries, and a name-only
	// default silently misses them.
	contents, err := tools.OptionalBoolArgDef(args, "contents", true)
	if err != nil {
		return tools.Result{}, err
	}
	symbols, err := tools.OptionalBoolArg(args, "symbols")
	if err != nil {
		return tools.Result{}, err
	}
	caseSensitive, err := tools.OptionalBoolArg(args, "case_sensitive")
	if err != nil {
		return tools.Result{}, err
	}

	res, err := t.store.Search(index.Search{
		Query:         query,
		Max:           max,
		Contents:      contents,
		Symbols:       symbols,
		CaseSensitive: caseSensitive,
	})
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("search failed: %v", err)}, nil
	}

	if len(res.Matches) == 0 {
		hint := ""
		if contents {
			hint = " (content search ran; try a shorter or different spelling of the query)"
		}
		return tools.Result{Content: fmt.Sprintf("no matches for %q%s", query, hint)}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d match(es) for %q", res.Count, query)
	if res.Truncated {
		fmt.Fprintf(&b, " (showing the first %d)", len(res.Matches))
	}
	b.WriteString(":\n")
	for _, m := range res.Matches {
		switch m.Kind {
		case "content":
			fmt.Fprintf(&b, "%s:%d: %s\n", m.Path, m.Line, m.Text)
		case "symbol":
			fmt.Fprintf(&b, "%s:%d — symbol %s\n", m.Path, m.Line, m.Text)
		default:
			fmt.Fprintf(&b, "%s (%s)\n", m.Path, m.Kind)
		}
	}
	b.WriteString("\nUse read_repo_file with a listed path to see the full source.")
	return tools.Result{Content: b.String()}, nil
}

// ReadRepositoryFile returns the cached text of an indexed file.
type ReadRepositoryFile struct {
	store Store
}

// NewReadRepositoryFile returns a ready-to-register file-read tool.
func NewReadRepositoryFile(store Store) *ReadRepositoryFile {
	return &ReadRepositoryFile{store: store}
}

func (ReadRepositoryFile) Name() string { return "read_repo_file" }

func (ReadRepositoryFile) Description() string {
	return "Read the full text of a source file from the repository index by its path relative " +
		"to the workspace root (e.g. \"internal/server/server.go\"). Fails for files that are not " +
		"indexed (ignored, binary, or over the size limit). Prefer this over read_file for " +
		"repository files since it uses the index and stays within the workspace."
}

func (ReadRepositoryFile) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path of the file relative to the workspace root, using forward slashes.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadRepositoryFile) Execute(ctx context.Context, args map[string]any) (tools.Result, error) {
	path, err := tools.StringArg(args, "path")
	if err != nil {
		return tools.Result{}, err
	}
	content, err := t.store.ReadIndexedFile(ctx, path)
	if err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}
	if content == "" {
		return tools.Result{Content: fmt.Sprintf("%s is empty or has no text content", path)}, nil
	}
	return tools.Result{Content: content}, nil
}

// Register adds every repository tool to m. Call after the runtime is
// constructed (the tools need the runtime's index store).
func Register(m *tools.Manager, store Store) error {
	all := []tools.Tool{
		NewSearchRepository(store),
		NewReadRepositoryFile(store),
	}
	for _, t := range all {
		if err := m.Register(t); err != nil {
			return err
		}
	}
	return nil
}

package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lato/internal/index"
	"lato/internal/tools"
)

// fakeStore is an in-memory Store for tool tests.
type fakeStore struct {
	search func(index.Search) (index.SearchResult, error)
	read   func(path string) (string, error)
}

func (f *fakeStore) Search(opts index.Search) (index.SearchResult, error) {
	if f.search != nil {
		return f.search(opts)
	}
	return index.SearchResult{}, nil
}

func (f *fakeStore) ReadIndexedFile(_ context.Context, path string) (string, error) {
	if f.read != nil {
		return f.read(path)
	}
	return "", nil
}

// searchOptions captures the options the tool passed to the store so
// tests can assert on defaulting behavior.
type searchOptions struct {
	opts index.Search
	err  error
}

func TestSearchRepositoryTool(t *testing.T) {
	store := &fakeStore{search: func(opts index.Search) (index.SearchResult, error) {
		return index.SearchResult{
			Matches: []index.Match{
				{Path: "internal/server/server.go", Line: 12, Column: 5, Text: "server.Listen()", Kind: "content"},
				{Path: "internal/server/server.go", Kind: "filename"},
				{Path: "pkg/api", Kind: "path"},
				{Path: "internal/server/server.go", Line: 3, Text: "struct Server", Kind: "symbol"},
			},
			Count: 4,
		}, nil
	}}
	tool := NewSearchRepository(store)
	res, err := tool.Execute(context.Background(), map[string]any{"query": "server"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Content)
	}
	for _, want := range []string{
		"4 match(es)",
		"internal/server/server.go:12: server.Listen()", // path:line: text, like grep
		"internal/server/server.go (filename)",
		"pkg/api (path)",
		"symbol struct Server",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("output missing %q:\n%s", want, res.Content)
		}
	}
}

// TestSearchRepositoryDefaultsToContentSearch pins the Milestone 5 fix:
// a plain query must reach the store with Contents enabled, since model
// queries like "find fmt.Println" are content queries and a name-only
// default silently missed them.
func TestSearchRepositoryDefaultsToContentSearch(t *testing.T) {
	var got index.Search
	store := &fakeStore{search: func(opts index.Search) (index.SearchResult, error) {
		got = opts
		return index.SearchResult{}, nil
	}}
	tool := NewSearchRepository(store)

	cases := []map[string]any{
		{"query": "fmt.Println"},
		{"query": "fmt.Println", "contents": true},
		{"query": "fmt.Println", "contents": "true"}, // models send strings sometimes
	}
	for _, args := range cases {
		got = index.Search{}
		if _, err := tool.Execute(context.Background(), args); err != nil {
			t.Fatalf("Execute(%v) error: %v", args, err)
		}
		if !got.Contents {
			t.Errorf("args %v: store received Contents=false, want content search enabled by default", args)
		}
	}
}

func TestSearchRepositoryContentsFalseIsHonored(t *testing.T) {
	var got index.Search
	store := &fakeStore{search: func(opts index.Search) (index.SearchResult, error) {
		got = opts
		return index.SearchResult{}, nil
	}}
	tool := NewSearchRepository(store)
	if _, err := tool.Execute(context.Background(), map[string]any{"query": "x", "contents": false}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got.Contents {
		t.Error("store received Contents=true, want explicit false honored")
	}
}

func TestSearchRepositoryPassesCaseAndMax(t *testing.T) {
	var got index.Search
	store := &fakeStore{search: func(opts index.Search) (index.SearchResult, error) {
		got = opts
		return index.SearchResult{}, nil
	}}
	tool := NewSearchRepository(store)
	args := map[string]any{"query": "Run", "case_sensitive": true, "max": float64(7)}
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !got.CaseSensitive || got.Max != 7 {
		t.Errorf("store options = %+v, want CaseSensitive with Max 7", got)
	}
}

func TestSearchRepositoryToolEmptyQuery(t *testing.T) {
	tool := NewSearchRepository(&fakeStore{})
	res, err := tool.Execute(context.Background(), map[string]any{"query": "   "})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Error("expected error for empty query")
	}
}

func TestSearchRepositoryToolNoMatches(t *testing.T) {
	store := &fakeStore{search: func(_ index.Search) (index.SearchResult, error) {
		return index.SearchResult{}, nil
	}}
	tool := NewSearchRepository(store)
	res, err := tool.Execute(context.Background(), map[string]any{"query": "zzz"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(res.Content, "no matches") {
		t.Errorf("output = %q, want no-match message", res.Content)
	}
}

func TestReadRepositoryFileTool(t *testing.T) {
	store := &fakeStore{read: func(path string) (string, error) {
		if path == "main.go" {
			return "package main\n", nil
		}
		return "", errors.New("not found")
	}}
	tool := NewReadRepositoryFile(store)

	res, err := tool.Execute(context.Background(), map[string]any{"path": "main.go"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if strings.TrimSpace(res.Content) != "package main" {
		t.Errorf("content = %q, want package main", res.Content)
	}

	res, err = tool.Execute(context.Background(), map[string]any{"path": "missing.go"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Error("expected error for missing file")
	}
}

func TestSearchRepositoryRegistersBothTools(t *testing.T) {
	m := tools.NewManager(tools.NewRegistry())
	if err := Register(m, &fakeStore{}); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	defs := m.Definitions()
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["search_repo"] || !names["read_repo_file"] {
		t.Errorf("registered tools = %v, want search_repo and read_repo_file", names)
	}
}

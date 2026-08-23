// Runtime-level access to the repository index. The runtime owns the
// index lifecycle: it builds the workspace index on first use and caches
// it, so /index, context building, and search all reuse the same
// in-memory snapshot instead of re-scanning the disk for every request.
package runtime

import (
	"context"
	"fmt"
	"strings"

	"lato/internal/index"
)

// Index returns the workspace index, building it lazily on first call
// and caching it. The index is bound to the runtime's discovered
// workspace root, so Lato always indexes the project it was launched in,
// never its own source tree. Building is deterministic and never fails.
func (r *Runtime) Index() *index.Index {
	root := r.Workspace().Root
	if r.index != nil && r.index.root == root {
		return r.index.idx
	}
	idx := index.NewBuilder(root).Build()
	r.index = &indexCache{root: root, idx: idx}
	return idx
}

// Search runs a repository search over the cached index and returns the
// top matches. Content search may stream unread file tails from disk for
// files whose indexed body was truncated; everything stays local.
func (r *Runtime) Search(opts index.Search) (index.SearchResult, error) {
	opts.Query = strings.TrimSpace(opts.Query)
	if opts.Query == "" {
		return index.SearchResult{}, fmt.Errorf("search query cannot be empty")
	}
	return r.Index().Search(opts), nil
}

// RelevantFiles returns up to n files from the index that are likely
// useful for answering a repository question. With a query it combines
// structural signals (manifests, READMEs, top-level source) with lexical
// overlap between the question's words and each file's metadata.
func (r *Runtime) RelevantFiles(n int, query string) []index.File {
	return r.Index().Relevance(index.Options{MaxRootFiles: n, Query: query})
}

// ReadIndexedFile returns the text content of a file by its
// slash-separated relative path, "" when it is missing, binary, or too
// large for the body bound.
func (r *Runtime) ReadIndexedFile(_ context.Context, relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}
	f, ok := r.Index().Lookup(relPath)
	if !ok {
		return "", fmt.Errorf("file %q is not in the index", relPath)
	}
	return f.Body, nil
}

// ForceReindex rebuilds the index from disk, discarding the cache. Used
// by /index to reflect files changed since startup.
func (r *Runtime) ForceReindex() *index.Index {
	r.index = nil
	return r.Index()
}

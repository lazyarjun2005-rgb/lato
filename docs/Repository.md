# Repository Indexing & Search

Lato builds a local, deterministic index of the workspace repository so
the agent can find files, directories, and symbols without sending the
whole tree to the model. The index is pure filesystem work — no AI, no
network, no telemetry — and is rebuilt in memory on demand.

## Package: `internal/index`

The `internal/index` package owns indexing and search. It is kept
separate from `workspace` (discovery), `context` (prompt assembly), and
`runtime` (orchestration), so each package has one responsibility.

### Indexing

When you ask Lato to index a workspace, it walks the tree once and
records for each file:

- Relative path (always slash-separated, even on Windows)
- File name and extension
- Detected language
- File size
- Whether the content is text or binary
- Go symbols, when the file is Go (`go/parser` + `go/ast`)

Every read is bounded:

- Content is only kept for files up to 4 MiB; larger files keep a
  bounded prefix and are marked truncated so search can stream the rest
  from disk on demand.
- Binary detection scans at most 8 KiB.
- The index holds at most 200 000 files.

Ignored directories are deterministic and version-control-aware:

- Always skipped: `.git`, `.hg`, `.svn`, `node_modules`, `vendor`,
  `target`, `dist`, `build`, `coverage`, `__pycache__`, `.venv`,
  `.idea`, `.vscode`, `.next`, `.nuxt`, `.cache`, `.terraform`, `.tox`,
  `bower_components`, `Pods`, `DerivedData`.
- A root `.gitignore` is honored, including basic negation (`!`) and
  directory-only (`dir/`) rules. Rules are normalized to forward slashes
  so the same `.gitignore` behaves identically on Linux and Windows.
- Lockfiles and generated files (`go.sum`, `package-lock.json`, …) are
  skipped.

### Search

The index supports deterministic local search over four distinct match
kinds:

| Kind     | What it finds                                              |
| -------- | ---------------------------------------------------------- |
| Content  | Lines of file text containing the query (line + snippet).  |
| Symbol   | Go symbols whose name matches, with the declaration line.  |
| Filename | Files whose base name contains the query.                  |
| Path     | Files whose relative path contains the query.              |

Content search inspects actual file contents — not just index metadata —
so a query like `fmt.Println` or `StreamChat` finds real code:

```text
main.go:6: fmt.Println("Hello from Lato test")
```

Behavioral details:

- Matching is case-insensitive by default; pass `case_sensitive` to
  require exact case.
- One content match is reported per matching line (capped per file so a
  single hot file cannot dominate results).
- Binary files are excluded from content search entirely.
- Files larger than the 4 MiB body bound keep a bounded prefix in the
  index; content search streams their remaining lines from disk on
  demand, under the same bounded-read rules, so needles beyond the
  prefix are still found with correct line numbers.

Results are ordered deterministically: content matches first, then
symbols, then filename/path matches; ties break by path, then line. The
same query against the same tree always yields the same output.

### API

| Type / function                  | Purpose                                                  |
| -------------------------------- | -------------------------------------------------------- |
| `index.NewBuilder(root)`         | Build an index for a workspace root.                     |
| `(*index.Builder).Build()`       | Walk the root and return an `*Index`.                    |
| `(*index.Index).Search(Search)`  | Run a search; returns `SearchResult`.                    |
| `(*index.Index).Lookup(path)`    | O(1) file lookup by slash-separated relative path.       |
| `(*index.Index).Relevance(opts)` | Top files for a question, scored against `opts.Query`.   |
| `(*index.Index).Stats()`         | Files, directories, languages, Go packages, symbols.     |
| `(*index.Index).Summary()`       | Renders the `/index` report.                             |

### Retrieval

Beyond exact search, the index answers "which files matter for this
question?" via `Relevance`. With no query it ranks structural importance
(READMEs, manifests, shallow source paths). With a query — typically the
user's natural-language question — files are scored by lexical overlap
between the question's words and each file's name, path, Go symbols, and
indexed content; structural signals shrink to tiebreakers so a deep file
about the subject outranks a generic root file. Results are capped and
deterministic. The runtime injects the top files into its repository
context snapshot when the user asks about the codebase.

## `/index` Command

`/index` prints a concise summary of the repository index:

```text
Index
  Repository      demo
  Root            /home/me/work/demo
  Files           42
  Directories     8
  Languages       Go (30), Markdown (6), YAML (4)
  Source files    30
  Symbols         156
  Ignored paths   .git/, vendor/, dist/
  Status          built
```

The command does no indexing of its own: it renders the runtime's cached
index, which is built lazily on first use, so unrelated conversations
never pay the walk cost.

## Repository tools

Two tools let the model query the index instead of reading the whole
repository:

- `search_repo` — takes `query` (required) and optional `contents`,
  `symbols`, `case_sensitive`, and `max`. Content search is **enabled by
  default**, because model queries like "find fmt.Println" or "where is
  StreamChat defined" are content queries; a name-only default silently
  missed them. Output is grep-style (`path:line: snippet`) for content,
  `path:line — symbol kind Name` for symbols, and `path (kind)` for
  filename/path hits, followed by a pointer to `read_repo_file`.
- `read_repo_file` — reads an indexed source file by its slash-separated
  relative path, returning the cached text (bounded).

The intended agent flow is: `search_repo` locates candidates with exact
line numbers → `read_repo_file` shows a chosen file in full. The model
never needs the repository dumped into context.

These tools are registered by the runtime and live in
`internal/tools/repository`. They communicate with the runtime through a
small `Store` interface, so the index stays runtime-independent.

## Repository context

When the user's message is a repository question ("explain this
repository", "how is this project structured", …), the runtime injects a
focused snapshot derived from the index alongside the existing workspace
context — file and symbol counts, language breakdown, and the most
relevant files. This keeps the prompt focused instead of dumping the
tree.

## Working from any directory

Lato always operates against the workspace it was started in, not its
own source tree:

- `Index` is bound to `workspace.Info.Root`, which `DiscoverDir`
  resolves by finding the nearest project marker (`go.mod`,
  `package.json`, `.git`, …) when you launch from a subdirectory.
- Launch from any project directory and `/workspace`, `/index`, the
  repository context, and the repository tools all target that project.

```text
~/Projects/my-go-project $ lato
/workspace   → Root: ~/Projects/my-go-project
/index        → indexes ~/Projects/my-go-project
"explain this repository" → context built from ~/Projects/my-go-project
```

Windows is supported: `cd C:\Projects\my-project && lato.exe` behaves
identically. Relative paths are reported with forward slashes; all
filesystem I/O uses `path/filepath`; `.gitignore` rule matching is
normalized so it does not depend on the platform's path separator.

## Design notes

- Indexing is **not persisted** yet. The index is rebuilt on demand and
  cached in memory by the runtime. The walk is structured so incremental
  or on-disk persistence can be layered on later without changing the
  traversal or search semantics.
- Everything is deterministic: the same workspace always produces the
  same index and the same search results.
- No cloud, no network: indexing, search, and retrieval are entirely
  local. Content search reads only workspace files.
- Search is substring-based, not semantic. "How does session recovery
  work?" finds files mentioning *session recovery* only through word
  overlap in `Relevance`; exact `search_repo` queries need literal text,
  a symbol name, or a file name. A vector/embedding layer could come
  later without changing these interfaces.

## Known limitations

- Symbol extraction is Go-only; other languages are searched by content
  and name but have no symbol index.
- `.gitignore` rules are read from the workspace root only (nested
  ignore files are not merged).
- Content matches are capped per file (5) and per query (10 000 raw
  matches before ranking), which keeps huge repositories bounded at the
  cost of completeness for pathological queries.
- Files modified after indexing are found only after a re-index
  (`ForceReindex` or `/index`'s rebuild); there is no filesystem watcher.
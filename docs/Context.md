# Context

Package: `internal/context`

The `context` package assembles a structured description of the repository Lato is running inside, for injection into a model prompt when the user asks repository-related questions. It is deliberately narrow: it only *reads* the workspace that was already discovered by `internal/workspace`. It never calls a model, never modifies files, and contains no UI code.

## Purpose

Before answering a question like "Explain this repository", Lato should know something real about the repository instead of letting the model hallucinate a generic architecture. The context package turns the discovered workspace into a compact, model-readable block that the runtime prepends to the system prompt.

## Core Types

### `Context`

The assembled repository context for one workspace.

| Field     | Description                                         |
| --------- | --------------------------------------------------- |
| `Workspace` | The discovered workspace (`workspace.Info`).      |
| `Readme`  | The first 200 lines of `README.md`, `""` if absent. |
| `GoMod`   | Parsed go.mod facts, `nil` unless the project is Go.|

### `GoMod`

Module-level facts read from a `go.mod` file.

| Field      | Description                                      |
| ---------- | ------------------------------------------------ |
| `Module`   | Module path from the `module` directive.         |
| `Go`       | Go version from the `go` directive.              |
| `Requires` | Direct dependencies, formatted `path version`.   |

## Entry Points

| Function                 | Description                                            |
| ------------------------ | ------------------------------------------------------ |
| `NewBuilder(ws)`         | Builds a `Builder` for a workspace. Does no I/O.       |
| `(Builder).Build()`      | Reads README and go.mod, re-discovers the workspace, returns a `Context`. |
| `(Context).Text()`       | Renders the context as one formatted block.            |
| `RepositoryQuestion(text)` | Reports whether text asks about the repository as a whole. |
| `LooksLikeCodeQuestion(text)` | Reports whether text asks about the repository or any specific part of its code (superset of `RepositoryQuestion`). |

## Context Generation Flow

1. The runtime calls `context.NewBuilder(r.workspace).Build()` when a repository question is detected.
2. `Build` re-discovers the workspace root (so a builder is standalone), then reads `README.md` (first 200 lines) and `go.mod`.
3. `Text()` renders a block of sections, dropping any that are empty:

```
Repository:
Lato

Language:
Go

Module:
lato

Build:
Go modules

Tree:
- cmd
- internal

README Summary:
# Lato
...

go.mod:
Module: github.com/acme/lato
Go: 1.26
Dependencies:
- gopkg.in/yaml.v3 v3.0.1

Important Files:
README.md
go.mod
```

## Runtime Integration

The runtime injects the context block only when the user's latest message is a repository-related question. Detection is a deterministic substring match on phrases such as "explain this repository", "how does this project work", "describe this codebase", and "what architecture is used". Unrelated chat is left untouched.

Injection happens in `buildMessages`, once per request. The context is appended to the system prompt; tool-loop continuation turns pass through the agent loop directly and never re-inject.

## Source Evidence for Code Questions

Questions about specific parts of the code ("How does the main function work?", "Where is fmt.Println used?") are detected by `LooksLikeCodeQuestion` — a broader interrogative-pattern check that is a superset of `RepositoryQuestion`. For those questions the runtime additionally retrieves deterministic source evidence from the cached index via the `internal/retrieve` package and appends it to the system prompt. See [Retrieval](Retrieval.md).

The full injection for a code question is:

```
static context block        (this package: workspace facts, README, go.mod)
repository snapshot         (runtime: index stats + most relevant files)
source evidence block       (retrieve: excerpts, declarations, related files)
```

## Boundaries

- No model calls, no embeddings, no vector databases, no semantic search.
- No file modification; the package only reads README and go.mod.
- No planning, no editing, no autonomous agents.
- Deterministic, pure file-based behavior.

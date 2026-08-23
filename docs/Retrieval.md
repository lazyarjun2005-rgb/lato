# Retrieval

Package: `internal/retrieve`

The `retrieve` package turns a natural-language question about the code into bounded, evidence-backed source excerpts from the repository index, so the model can answer repository questions from actual code instead of README-and-tree guesswork. It is the deterministic retrieval layer between the user's question and model reasoning.

## Purpose

Before this layer, repository questions were answered with static metadata only: the workspace description, README summary, and a *list* of relevant file names. The model had to guess what was inside the files or spend tool calls discovering them. Retrieval closes that gap: for a code question, the runtime injects real excerpts, symbol declarations, and import relationships before the model says a word.

The pipeline this enables:

```
user question
     ↓
question detection        (context.LooksLikeCodeQuestion)
     ↓
term extraction           (retrieve.Terms)
     ↓
scored retrieval          (one pass over the cached index)
     ↓
bounded evidence block    (excerpts + declarations + related files)
     ↓
injected into system prompt
     ↓
model reasons from actual source
```

The full repository is never sent to the model. Only the retrieved evidence is.

## Design

- **Deterministic and local.** One pass over the already-cached index. No rescan of the disk per question, no model calls, no network, no telemetry.
- **Bounded by construction.** Every stage has a cap, so a question over any repository yields a compact block, never a dump.
- **Never fails.** A question with no usable terms or no matches yields empty evidence; the runtime then injects nothing.

## How It Works

### 1. Term extraction (`Terms`)

The question is lowercased and split into terms. Stop-words ("how", "does", "the", "work", …) and words under three characters are dropped; identifiers stay intact (`fmt.Println` remains one term). At most 8 terms are kept.

### 2. Scoring (`ForQuestion`)

Every non-binary indexed file is scored against the terms in a single pass:

| Signal                        | Points per hit | Cap |
| ----------------------------- | -------------- | --- |
| Term in file name             | +4             | —   |
| Term in path                  | +2             | —   |
| Term in a Go symbol name      | +4             | +12 |
| Term on a content line        | +3 per line    | +15 |

Symbol and name hits outvalue incidental content hits because they identify purpose. Per-category caps keep a file that repeats one term everywhere from dominating the block. Ties break by path, so results are stable. The top 6 files form the evidence block.

### 3. Evidence assembly

For each winning file the block carries:

- **Declarations** — the file's Go symbols (functions, methods, structs, interfaces, types, constants, variables) with declaration line numbers, up to 8 per file. This reuses the symbol extraction the index already performs at build time; nothing is re-parsed.
- **Excerpts** — up to 3 line-numbered ranges around the matching lines, 3 lines of context on each side, at most 40 excerpt lines per file. Nearby matches merge into one range.
- **Related files** (Go only) — for each import of the winning file whose last path segment matches another indexed file's directory or package name, that file is listed with its own top symbols (up to 4 related files across the whole block). Generic segments (`internal`, `pkg`, hosts, module-version suffixes like `v2`) are ignored.

### 4. Rendering (`Text`)

The evidence renders as one compact block:

```
Source evidence from "acme" for: How does the main function work?

--- main.go (Go) ---
Declarations: function main (line 5)
main.go:5:
     1 | package main
     2 |
     3 | import "fmt"
     4 |
     5 | func main() {
     6 |     fmt.Println("Hello")
     7 | }
Related via import example.com/acme/greet: greet/greet.go (package greet)
  function Hello (line 7)
```

The whole block is capped at 12 KiB; if rendering exceeds the cap it is cut at a line boundary with a truncation marker. Empty evidence renders as `""` and the runtime injects nothing.

## Runtime Integration

`Runtime.contextFor` detects code questions with `context.LooksLikeCodeQuestion` and, when the evidence is non-empty, appends `evidence.Text()` after the static context block and the repository snapshot. Injection happens once per request, on the first model turn; tool-loop continuations never re-inject.

The evidence is derived from the runtime's cached index, so it reflects recent `edit_file`/`create_file` changes and command runs automatically — those invalidate the cache and the next question rebuilds.

## Testing

Tests cover: question → source evidence for the asked-about function; "where is X used?" content retrieval; symbol declarations in evidence; import-followed related files; exclusion of unrelated files; bounds under pathological input (thousands of matching lines); arbitrary-workspace isolation; and degenerate inputs (nil index, empty question, stop-word-only question). All tests run against temporary directories with platform-neutral slash paths; none require Ollama.

## Boundaries

- No model calls, no network, no disk writes, no rescan per question.
- No language-server features: no type resolution, no cross-reference database, no call graph. Import relationships are the only code relationships modeled.
- Symbol extraction is Go-only today (reusing the index's parser); other languages contribute content, name, and path signals but no declaration summaries.

# Editing

Package: `internal/edit`, tools in `internal/tools/editing`

The editing engine lets Lato make targeted, verifiable changes to files in the target repository. It is local and deterministic: no model calls, no network, no telemetry. The model decides *what* to change by calling the editing tools; the engine guarantees the change is applied exactly where it was asked for, or not at all.

## Purpose

Editing is deliberately conservative. A replacement states the exact old text it expects to find, and the engine refuses to act when that text is missing or matches more than one location. A vague instruction can therefore never silently modify the wrong part of a file. Whole-file rewrites are `write_file`'s job; the editing engine is for targeted changes.

## Architecture

```
model turn
   │  tool call: edit_file / create_file
   ▼
internal/tools/editing      tool layer: argument parsing, soft errors, output text
   │
   ▼
internal/edit               pure engine: path resolution, replacement, atomic write
   ├── Resolve()             workspace-confined path validation
   ├── ReplaceFile()         exact-match replacements, all-or-nothing
   ├── CreateFile()          create-only writes that never overwrite
   └── Diff()                unified-style diff of before/after
```

The engine knows nothing about tools, providers, or the runtime; the tool layer knows nothing about how paths are validated. The runtime registers the tools against its discovered workspace root (see [Runtime](Runtime.md)).

## Tools

### `edit_file`

Applies one targeted replacement to an existing file.

| Argument   | Required | Description                                              |
| ---------- | -------- | -------------------------------------------------------- |
| `path`     | yes      | File path relative to the workspace root, forward slashes. |
| `old_text` | yes      | Exact current text to replace; must match exactly once.  |
| `new_text` | yes      | Replacement text; may be empty to delete `old_text`.     |

On success the result contains a summary (`edited main.go (1 replacement(s))`) followed by a unified-style diff. When `old_text` does not change the file content, the result says so and nothing is written.

Failure modes are soft errors (`IsError: true`) with recovery hints:

| Condition                        | Result                                                       |
| -------------------------------- | ------------------------------------------------------------ |
| `old_text` absent from the file  | Error suggesting re-reading the file with `read_repo_file`.  |
| `old_text` matches more than once| Error asking for more surrounding lines to disambiguate.     |
| File missing / directory        | Error describing what could not be opened.                   |
| Path outside the workspace       | Invalid-path error naming the workspace root.                |
| File larger than 4 MiB           | Size-limit error; oversized files are not editable.          |

### `create_file`

Writes a brand-new file inside the workspace, creating parent directories as needed. It never overwrites: an existing file is an error pointing the model at `edit_file`. Empty content is allowed.

## Safe Edit Behavior

- **Verify-then-write.** All operations are computed against the read file first. If any check fails, no bytes are written.
- **All-or-nothing across operations.** `edit_file` applies its single op in memory; a failed match means the on-disk file is untouched.
- **Atomic replace.** Successful writes go to a temporary sibling file, then rename over the destination. An interrupted write never leaves a half-edited file behind. Original permissions are preserved.
- **No-op detection.** Replacing text with itself reports success without touching disk or invalidating derived state.
- **Line-ending adaptation.** If the exact `old_text` is absent because of CRLF/LF differences, the other line-ending style is retried, and the replacement is written back in the file's own style so a Windows checkout keeps CRLF endings.
- **No permission prompts.** The agent edits automatically within the safety boundaries above; there is no interactive confirmation system.
- **No deletion.** Files cannot be removed by this milestone's tools; deletion is out of scope until explicitly requested later.

## Diff Behavior

`edit.Diff(path, before, after)` renders a compact unified-style patch:

```
--- main.go
+++ main.go
@@ -1,5 +1,5 @@
 package main

 func main() {
-	fmt.Println("Hello")
+	fmt.Println("Hello from Lato")
 }
```

Properties:

- Deterministic: same inputs always produce the same diff.
- Line-ending normalized: CRLF input produces the same text as LF; a pure ending-style change yields an empty diff.
- Hunks are grouped with up to three lines of context, separated when more than six unchanged lines lie between changes.
- The diff describes the change only; it cannot be applied back as a patch.

The diff is embedded in successful `edit_file` results and available programmatically from `Result.Before`/`Result.After`.

## Workspace Confinement

Every path is resolved against the workspace root discovered at startup — the project Lato was launched in, which may be any directory on the machine, never assumed to be Lato's own source tree.

Path validation is style-independent and identical on Linux and Windows:

- Both `/` and `\` separators are accepted and normalized.
- Absolute forms (`/x`), Windows drive letters (`C:x`), UNC shares (`\\host\share`), and any `..` segment are rejected on every platform.
- Results and diffs report slash-separated workspace-relative paths regardless of platform.

One consequence: because `\` always means "separator", a file whose name literally contains a backslash cannot be addressed by the editing tools. Such names are effectively reserved.

The confinement check is lexical; symlinks that point outside the workspace are not followed or detected.

## Index Invalidation

A successful write calls the workspace's `OnChange` hook, which the runtime sets to drop its cached repository index. Nothing is rebuilt eagerly — the next search, read, or context build re-indexes from disk lazily. No-op edits do not fire the hook.

## Testing

Tests use temporary directories and require no Ollama. Coverage includes: exact replacement, missing/ambiguous old text, unrelated-content preservation, nested files, empty files, multiline and sequential edits, creation and overwrite refusal, permission preservation, oversized-file refusal, traversal and absolute-path rejection, CRLF/LF adaptation, diff rendering, tool integration through `tools.Manager`, and runtime registration with index invalidation.

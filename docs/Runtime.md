# Runtime

Package: `internal/runtime`

The `runtime` package is the Lato harness. It coordinates configuration, agents, providers, tools, skills, and sessions to process a user request and return a response.

## Purpose

The runtime contains no product logic of its own beyond ordering steps. It connects the other packages and runs the agent loop until the model finishes or an error occurs.

## Construction

```go
func New() (*Runtime, error)
```

`New` builds a ready runtime:

1. Load configuration.
2. Resolve the Lato home directory.
3. Build the skill store.
4. Create the agent with the base prompt and skill catalog.
5. Create the model provider.
6. Create the tool manager with built-in tools.
7. Register the `load_skill` tool.
8. Register the repository tools (`search_repo`, `read_repo_file`) against the runtime's index.
9. Register the editing tools (`edit_file`, `create_file`) against the discovered workspace root.
10. Register the command execution tool (`run_command`) against the discovered workspace root; every started run invalidates the cached index (see [Commands](Commands.md)).

## Public Methods

| Method            | Description                                                      |
| ----------------- | ---------------------------------------------------------------- |
| `CurrentModel`    | Returns the active model name.                                   |
| `CurrentProvider` | Returns the active provider name.                                |
| `SetModel`        | Switches the model for the next request (effort preserved).      |
| `SetProvider`     | Switches the provider for the next request (effort preserved).   |
| `CurrentEffort`   | Returns the active effort label (M16).                           |
| `SetEffort`       | Switches effort; persists when asked, session-only otherwise.    |
| `SetModelWithEffort` | Switches model + effort atomically and persists both.         |
| `SetSessionModelWithEffort` | Same, but config.yaml is never written.               |
| `StreamChat`      | Runs the agent loop and emits structured events as they happen.  |
| `Stream`          | Compatibility alias for `StreamChat`.                            |
| `Run`             | Runs the agent loop and returns the final response.              |
| `RunContext`      | Same as `Run`, with caller-controlled cancellation.              |

## Effort

The active effort level (M16) scales the agent loop through a bounded
profile — turn budget, repetition thresholds, and complex-task prompt
guidance — and, where the provider declares support, the request-side
reasoning parameter. See [Effort](Effort.md). Safety bounds are
identical at every level.

## Events

`StreamChat` emits `Event` values on a channel.

| Event type        | Meaning                                              |
| ----------------- | ---------------------------------------------------- |
| `EventThinking`   | The model is thinking or a turn has started.         |
| `EventText`       | Incremental assistant text.                          |
| `EventToolStart`  | A tool call is about to run.                         |
| `EventToolFinish` | A tool call finished with a result or error.         |
| `EventDone`       | The run completed with a final response.             |
| `EventError`      | The run stopped because of an error.                 |

## Agent Loop

The runtime uses one loop for both streaming and non-streaming callers.

```text
build messages (system + history)
        │
        ▼
 stream one model turn
        │
        ├── text / thinking events
        │
        ├── no tool calls ──► EventDone
        │
        └── tool calls
                │
                ▼
        for each tool call
                │
                ├── EventToolStart
                ├── classify + permission check (M13)
                │       ├── allow  → execute tool
                │       ├── ask    → confirm with user, then execute or refuse
                │       └── deny   → structured refusal result, no execution
                ├── EventToolFinish
                └── append tool result message
                │
                ▼
        start next model turn
```

Detailed steps:

1. Build the message list with the agent system prompt and conversation history.
2. Emit thinking, then stream one provider turn.
3. Collect text and tool calls from provider stream events.
4. If there are no tool calls, emit `EventDone` and stop.
5. If there are tool calls, append the assistant message.
6. Execute each tool through the tool manager.
7. Append each tool result as a tool message.
8. Repeat from step 2.

## Skill Loading

The runtime registers a special tool named `load_skill`.

- The agent system prompt includes only the skill catalog.
- When the model needs full skill instructions, it calls `load_skill` with a skill ID.
- The tool reads the skill body from the in-memory skill store.
- No full scan of the skills directory happens during tool execution.

## Entry Points

| Entry point              | Use                                              |
| ------------------------ | ------------------------------------------------ |
| `lato` or `lato chat`    | Interactive TUI, which streams runtime events.   |
| `lato run [task]`        | One-shot prompt through `runtime.Run`.           |
| `runtime.Run(messages)`  | Convenience helper that creates a runtime and runs once. |

## Repository Context

The runtime discovers the workspace once at startup (see `internal/workspace`) and captures it on the `Runtime`. When the user asks a repository-related question — such as "Explain this repository" or "how does this project work" — the runtime builds a context block via `internal/context` and prepends it to the system prompt before the request reaches the model. It also appends a compact snapshot derived from the repository index (file, directory, language, package, and symbol counts, plus the most relevant files). Unrelated chat is never augmented. The workspace is re-scanned from disk at startup, and the context and index are built lazily only when a repository question is detected.

Questions about specific code ("How does the main function work?", "Where is fmt.Println used?") additionally receive deterministic source evidence: `internal/retrieve` scores the cached index against the question's terms and injects bounded excerpts, Go symbol declarations, and import-related files, so the model answers from actual source. See [Retrieval](Retrieval.md).

## Repository Index

The runtime owns the workspace index lifecycle (see `internal/index`). The index is built lazily on first use and cached, so `/index`, repository context, and the repository tools all share the same in-memory snapshot instead of re-scanning the disk for every request. The index is always bound to the discovered workspace root — Lato indexes the project it was launched in, never its own source tree. Two tools let the model query it: `search_repo` and `read_repo_file` (see `internal/tools/repository`).

## Repository Editing

The editing tools (`edit_file`, `create_file`, see `internal/tools/editing` and [Editing](Editing.md)) are registered against the same discovered workspace root. A successful write drops the cached index, so the next search or read rebuilds from disk lazily; no-op edits keep the cache. Edits are confined to the workspace and require no interactive confirmation.

## Bounded Planning Loop (M10)

Multi-step goals get a PLAN → ACT → OBSERVE → REPLAN cycle without a second execution mechanism. When the latest user message matches conservative multi-action heuristics (`isComplexTask`), the system prompt receives a compact task protocol: output a short numbered plan first, act with tools, check each result before continuing, run targeted verification after modifying files, and finish with "Task complete:" or "Task blocked:". Simple requests never see this protocol.

Every request — simple or complex — runs inside hard bounds enforced by `run()`:

- `maxAgentTurns = 12`: at most twelve model turns per user request. Reaching the budget stops execution cleanly with a visible summary of executed actions.
- Repetition guard: identical consecutive tool calls (name + canonical arguments) are counted; after three repeats a one-time steering message is injected, and a fourth identical call stops the run with an explanation.

Both stop paths end as normal completions (visible text + done event), so the TUI and session persistence treat them like final answers. Plans are action-level steps only; internal reasoning is not requested or displayed.

## Permission System (M13)

Every tool call passes through a centralized permission gate before execution: classify → check → allow/deny/ask → execute only if allowed → observe result → continue the same loop. The gate lives around tool execution in `run()` (`internal/runtime/permissions.go`), never inside providers, so Ollama, OpenRouter, 9Router, NVIDIA, OmniRoute, and custom OpenAI-compatible backends are constrained identically — no model receives extra privileges because of where it comes from.

Classification and policy live in `internal/permissions`:

| Class               | Examples                                                   | Default behavior                                   |
| ------------------- | ---------------------------------------------------------- | -------------------------------------------------- |
| `read_only`         | search_repo, read_repo_file, list_files, pwd               | Allowed automatically                              |
| `workspace_write`   | create_file, edit_file, write_file, memory updates         | Allowed inside the workspace                       |
| `command_execution` | run_command                                                | Safe dev commands allowed; anything else asks      |
| `high_risk`         | deletions, destructive git ops, out-of-workspace writes    | Always require explicit confirmation               |

- **Workspace boundary.** Paths are canonicalized (cleaned, absolutized, symlink-resolved through the deepest existing ancestor) before comparison against the workspace root — never a string prefix check. `../` traversal, absolute paths outside the root, drive letters, and symlink escapes require approval or are refused.
- **Command safety.** Commands with shell features (`;`, `&&`, `||`, `|`, `$()`, backticks, redirections) outside quotes are judged conservatively as a whole: `go test ./... && rm -rf something` is high-risk, never "a safe test run". Deletions, `git reset --hard`, `git clean -fd(x)`, force pushes, and privilege escalation ask first. Unknown programs ask. Safe development commands (`go test/build/vet/fmt`, read-only git, grep/find/rg, package test/build/run) stay frictionless.
- **Confirmation flow.** An Ask verdict pauses execution and shows a compact modal ([1] Allow once · [2] Allow for task · [3] Deny). Allow-once covers exactly one action signature; allow-for-task covers matching actions until that task ends. Grants live only in process memory — nothing is persisted, so resumed tasks re-ask after restart, and `/permissions reset` drops them immediately.
- **Denial.** A refused action returns a structured "Permission denied … was NOT executed" tool result into the same M10 loop; the model observes it and replans. There is no bypass by renaming tools, rewriting paths, or switching providers.
- **Non-interactive fail-safe.** Without an interactive session (`lato run`), any action needing confirmation is refused — missing confirmation is never treated as approval.
- **Secrets.** Prompts, refusals, transcripts, and task checkpoints pass through credential redaction (same rules as M11); `API_KEY=…`, bearer tokens, and private keys are masked before display.

## Project Memory (M11)

Lato keeps persistent, project-specific memory: small durable facts that survive restarts and supplement repository retrieval and the M10 planning loop. Memory lives entirely outside the repository, under the user configuration directory (`~/.config/lato/memory/<project-hash>.json` on Linux, `%AppData%\Lato\memory\...` on Windows), keyed by a SHA-256 hash of the workspace root — two projects with identical folder names never share memory.

- **Creation is explicit.** The agent stores durable discoveries through `remember_project_fact` (and can correct or delete via `update_project_memory` / `forget_project_memory`); users add facts with `/memory add TEXT`. Nothing is saved automatically from responses or tool output.
- **Kinds.** Facts are `user` (typed by a human) or `discovered` (inferred by the agent). A user statement of the same fact supersedes discovered evidence.
- **Retrieval is lexical and bounded.** Before each request the runtime scores stored facts against the request's words; only relevant entries (max 8, max ~2 KB) are injected into the system prompt under "## Project memory", and the TUI shows a one-line "Memory: N relevant project fact(s)" activity entry. Irrelevant memory is never injected; simple greetings skip memory entirely.
- **Bounds & safety.** Max 64 entries per project, 400 characters per fact; credential-shaped content (`sk-…`, `password=…`, private keys) is rejected without echoing it. Storage files use restrictive permissions and stay out of git.

Manage memory with `/memory`, `/memory add`, `/memory remove <id>`, `/memory clear`.

## Session / Task Continuity (M12)

Complex multi-step requests create a persistent task record (internal/task) that checkpoints at meaningful boundaries — after the plan is stated, after every tool action, and at completion or interruption. Records live outside the repository under the user configuration directory (`~/.config/lato/tasks/<project-hash>.json`), keyed by the same M11 project identity, and are bounded: at most 20 records per project with active/paused tasks never pruned.

- **Checkpoint contents** are structured only: goal, parsed plan steps with states, last action, next step, verification status ("go test ./... → passed"), bounded changed-file list, status (`active`/`paused`/`completed`/`blocked`/`abandoned`), timestamps. Secrets are redacted on write; chain-of-thought, full prompts, and raw tool output are never stored.
- **Interruption keeps work resumable.** Provider errors, budget exhaustion, repetition stops, Ctrl+C, or process death all leave the record resumable — a completed status is written only when the run genuinely finishes.
- **Resume re-plans instead of replaying.** `/task resume [id]` (or "continue where we left off" with exactly one resumable task; ambiguity produces a list, never a guess) restores goal+state into the standard M10 loop with an explicit warning that the repository may have changed and must be re-inspected. Old tool calls are never replayed.
- **Compact status preview.** Every task boundary appends a small structured block to the transcript (Task / Progress k/N / Last / Next / Verify / Files changed / Status), built from recorded state fields only — plain text, so `/copy` handles it like any other output.

Commands: `/task`, `/task resume [id]`, `/task abandon <id>`. Permission state interacts with M12 deliberately: a resumed task re-inspects the workspace, never replays old tool calls, and re-asks for any dangerous action because approval memory does not survive restarts. A denied action is checkpointed as "permission denied" in the task's last action so the pause is self-explanatory.

## Design Notes

- The runtime owns the multi-turn tool loop. Providers stream only one turn.
- Model and provider switches take effect on the next request.
- Cancellation through context stops emission and ends the run cleanly when possible.

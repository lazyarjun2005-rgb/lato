# Phase 2E — Output & Context Management

## 1. Purpose

Prevent tool output from flooding the model's context window. Bound
individual tool results and provide clear truncation markers so the
model knows when output was shortened. Preserve useful information
while preventing context flooding.

## 2. Architecture Findings (Pre-Implementation)

- **Tool result flow**: `executeTool` → `manager.Execute` → tool
  `Execute` → `result.Content` → `session.FenceToolResult` →
  conversation history (`messages`).
- **Existing size limits**:
  - `read_file`: `maxReadSize = 5 MiB` hardcoded.
  - `search_repo`: Returns structured matches with `Truncated` flag
    in `index.SearchResult`.
  - `run_command`: `process.MaxCapture = 128 KiB` per stream.
  - No unified output cap for arbitrary tool results.
- **Conversation history**: Unbounded growth; `messages` slice
  accumulates all tool results indefinitely.
- **Provider request**: `runModelTurn` sends full `messages` slice to
  provider; no context window budgeting.

## 3. Implementation Summary

### 3.1 Bounded Tool Output (Step 1)

**Config**: `limits.max_tool_output` (default `64 KiB`) in `config.yaml`
under `limits:`.

**Enforcement**: In `Runtime.run`, after each tool execution and
before adding the result to conversation history, `result.Content` is
passed through `TruncateOutput(content, limits.MaxToolOutput)`.

**Truncation algorithm** (`internal/runtime/truncate.go`):
- If `maxBytes <= 0`: no limit.
- If content ≤ `maxBytes`: returned unchanged.
- Otherwise: preserves first and last portions with a clear marker
  inserted in the middle: `\n... [output truncated: showing beginning and end] ...\n`
- Marker byte count included in budget.
- UTF-8 safe: never splits multi-byte characters.
- If `maxBytes` < marker size: simple truncation without marker.

**Applied to**: All tool results uniformly (filesystem, shell,
repository, memory, skills, etc.) at the single point where tool
results enter conversation history.

### 3.2 File and Search Output (Step 2)

- **`read_file`**: Retains existing `maxReadSize = 5 MiB` hard limit
  (file-size check before read). Runtime truncation applies on top as
  a second safety net.
- **`read_repo_file`**: Returns cached file text; bounded by runtime
  truncation.
- **`search_repo`**: Returns structured match list; runtime truncation
  applies to formatted result string.
- **`run_command`**: Retains `process.MaxCapture = 128 KiB` per
  stream; runtime truncation applies on top.
- **Errors**: Never silently discarded; truncation preserves error
  information at both head and tail.

### 3.3 Context Budgeting (Step 3)

**Current state**: No explicit context window budgeting. The
conversation history grows unbounded. Provider request sends full
`messages` slice.

**Phase 2E scope**: Individual tool output bounding (above).
Full conversation-level context budgeting (token counting, history
compaction, sliding window) is **deferred** to a future phase.
Documented as a known limitation.

### 3.4 Configuration (Step 4)

```yaml
limits:
  max_tool_calls: 100
  max_consecutive_failures: 5
  provider_retries: 3
  tool_execution_timeout: 300          # seconds
  max_tool_output: 65536               # bytes; 0/negative = default (64 KiB)
```

- Zero/negative → safe defaults (never "unlimited").
- Stored as bytes (int) for YAML compatibility.

## 4. Tests Added

- `internal/runtime/truncate_test.go`: `TestTruncateOutput`,
  `TestTruncateOutputUTF8`, `TestTruncateOutputSmallMaxBytes`.
- `internal/runtime/permissions_runtime_test.go`:
  `TestToolOutputTruncation`, `TestToolOutputTruncationLarge`.
- `internal/config/config_test.go`: `TestEffectiveLimitsMaxToolOutput`.
- All existing Phase 1/1.1/2A/2B/2C/2D tests pass unchanged.

## 5. Validation Results

```
gofmt -l ./internal ./cmd     → (no files)
go vet ./...                   → clean
go build ./...                → ok
go test ./...                 → all packages pass (17s)
git diff --check              → clean
```

## 6. Known Limitations

- **No conversation-level context budgeting**: History grows
  unbounded; a long session may exceed provider context window.
  Deferred to future phase.
- **No token counting**: Uses byte count as proxy; rough approximation
  (1 token ≈ 4 bytes for English text).
- **No history compaction/summarization**: Old tool results remain in
  history indefinitely.
- **No per-tool output limits**: Single global limit for all tools.
- **UTF-8 safety only**: No grapheme cluster awareness (e.g. emoji
  sequences may be split at byte boundary, though marker prevents
  model confusion).
- **Error output truncation**: Errors also truncated; may lose
  diagnostic detail in very long error outputs.
- **No streaming truncation**: Entire tool result buffered before
  truncation.

## 7. Scope Confirmation

- Phase 2E complete.
- Phase 2E does **not** implement: conversation-level context
  budgeting, token counting, history compaction, summarization, or
  provider-specific context window awareness.
- No push, release, tag modification, or stash operations.

---

**Phase 2E COMPLETE** — ready for next phase authorization.
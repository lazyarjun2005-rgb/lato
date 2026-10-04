# Phase 2C + 2D — Agent Loop Intelligence, Cancellation & Timeouts

## 1. Purpose

Improve agent loop intelligence and harden cancellation propagation with
bounded tool execution timeouts. Preserve normal coding workflows while
preventing runaway agents, stalled loops, and silent hangs.

## 2. Architecture Findings (Pre-Implementation)

- **Effort profiles** (`effort.go`): MaxTurns 6/12/18/24/32 with
  RepeatNudgeAfter/RepeatStopAfter.
- **Runtime loop** (`runtime.go:run`): MaxTurns cap, repeat detection via
  `toolSignature`, stall detection (`maxStallContinuations=2`),
  continuation nudges.
- **Tool execution**: `executeTool` → permission gate → `Manager.Execute`
  (with Phase 2A validation).
- **Provider retries**: `runModelTurn` retries pre-content transient
  failures (Phase 2B).
- **Phase 2B limits**: MaxToolCalls (100), MaxConsecutiveFailures (5),
  ProviderRetries (3).
- **Cancellation**: `ctx.Err()` checked after turns and tool executions.
- **Subprocess execution**: `process.Run` uses `context.WithTimeout`.
- **Provider streaming**: `runModelTurn` with pre-content retry logic.

## 3. Implementation Summary

### 3.1 Tool Execution Timeout (Phase 2C/2D)

**Config**: `limits.tool_execution_timeout_seconds` (default 300s / 5min)
in `config.yaml` under `limits:`.

**Enforcement**: `executeTool` wraps `manager.Execute` with
`context.WithTimeout(ctx, limits.ToolExecutionTimeout)`. Applies to
**all** tools uniformly as a safety net.

**Behavior**:
- Timeout triggers `context.DeadlineExceeded` → converted to structured
  tool result: `"tool X timed out after 5m0s"`.
- **Not counted** as consecutive failure (resource limit, not tool bug).
- Does **not** trigger provider retries.
- Separate from shell command timeout (`run_command` has its own
  `timeout_seconds` argument and `process.MaxTimeout=30m`).

### 3.2 Cancellation Handling (Phase 2D)

- **Propagation**: `ctx` passed through entire call chain:
  `StreamChat` → `runModelTurn` → `run` → `executeTool` →
  `manager.Execute` → tool `Execute` / `process.Run`.
- **Provider retries**: Respect `ctx.Done()` during backoff
  (`runModelTurn` select on `ctx.Done()`).
- **Subprocess**: `process.Run` uses `exec.CommandContext` with
  `context.WithTimeout`; `cmd.Wait()` respects `runCtx.Err()`.
- **Not a failure**: `context.Canceled`/`DeadlineExceeded` from
  top-level `ctx` → immediate `EventError` + return, **never** counted
  as tool failure or retry trigger.
- **No retry on cancel**: Provider retry logic explicitly excludes
  `context.Canceled`/`DeadlineExceeded` from transient errors.
- **TUI cancellation**: `emit` returns `false` on context cancellation
  → loop exits cleanly.

### 3.3 Agent Loop Intelligence Improvements

- **Timeouts not counted as failures**: Tool timeouts (`context.DeadlineExceeded`)
  do **not** increment consecutive failure counter. They are resource
  limits, not tool bugs.
- **Timeouts don't trigger retries**: Provider retry logic explicitly
  excludes `context.DeadlineExceeded` from transient errors.
- **Tool timeout not a consecutive failure**: Consecutive failure
  counter only increments for genuine tool errors (validation,
  permission, execution), not timeouts.
- **Stall detection preserved**: Existing `maxStallContinuations=2`
  with continuation nudges unchanged. Tool execution time contributes
  to stall detection indirectly (long-running tool = longer turn).
- **Distinct stop reasons**:
  - Max turns: "Paused: turn budget exhausted..."
  - Tool budget: "Tool-call budget exhausted (N calls). Run terminated."
  - Consecutive failures: "Consecutive tool failures limit reached (N)."
  - Tool timeout: Returns error to model, run continues (budget permitting).
  - Provider retries exhausted: "provider retries exhausted after N attempts"
  - Cancellation: `ctx.Err()` propagated via `EventError`
  - Normal completion: Unchanged

### 3.4 Configuration

```yaml
limits:
  max_tool_calls: 100                   # hard cap on tool executions per run
  max_consecutive_failures: 5           # stop after this many consecutive failures
  provider_retries: 3                   # extra provider attempts for transient failures
  tool_execution_timeout: 300           # seconds; 0/negative = default (300s / 5min)
```

- Zero/negative → safe defaults (never "unlimited").
- `ToolExecutionTimeout` in **seconds** (int) for YAML compatibility.
- Existing config files load without changes; defaults applied by
  `EffectiveLimits()`.

## 4. Provider Compatibility

- **No provider interface changes**: `ModelProvider.StreamChat` signature
  unchanged.
- Timeouts enforced at runtime layer (`runModelTurn`, `executeTool`),
  transparent to Ollama, OpenAI-compatible, NVIDIA, OpenRouter, 9router,
  Omniroute.
- Provider retries unchanged (pre-content only, respects cancellation).
- Shell command timeout unchanged (`run_command.timeout_seconds`).

## 5. Tests Added

- `internal/tools/args_test.go`: `TestValidate` (unchanged).
- `internal/tools/manager_test.go`: `TestManager_ExecuteValidatesArgsBeforeSideEffect`.
- `internal/runtime/permissions_runtime_test.go`:
  - `TestToolBudgetExhausted` (budget stop at 2 calls)
  - `TestConsecutiveFailureLimit` (4 validation errors → stop)
  - `TestProviderRetryOnTransientError` (helper logic)
  - `TestToolExecutionTimeoutConfig` (config defaults/normalization)
  - `TestMalformedArgumentsYieldToolErrorWithoutExecution` (unchanged)
  - `TestDeniedPermissionDoesNotExecute` (unchanged)
- All existing Phase 1/1.1/2A/2B tests pass.

## 5. Validation Results

```
gofmt -l ./internal ./cmd       → (no files)
go vet ./...                     → clean
go build ./...                   → ok
go test ./...                    → all packages pass (15s)
git diff --check                 → clean
```

## 6. Known Limitations

- **Tool timeout is wall-clock only**: No CPU-time accounting.
- **No per-tool timeout override**: Single global limit applies to all
  tools. Shell commands have their own `timeout_seconds` argument.
- **No forced preemption**: `context.WithTimeout` relies on tool/subprocess
  respecting context cancellation. A tool that ignores context (e.g. tight
  loop without select) will run until the *parent* context is cancelled
  (e.g. user Ctrl-C or request timeout).
- **Provider retries pre-content only**: Mid-stream errors after any
  content emitted are not retried.
- **Subprocess timeout separate**: `run_command` has its own
  `timeout_seconds` (max 30 min). Tool timeout applies to the
  `run_command` tool invocation itself (wall-clock).
- **No automatic tool fallback**: If a tool times out, the model must
  decide next action (retry, different tool, conclude).
- **No CPU-time enforcement**: Go runtime doesn't support per-goroutine
  CPU quotas.

## 7. Scope Confirmation

- Phase 2E (output truncation/context budgeting) — not started
- No push, release, tag modification, or stash operations.
- v1.0.9 tag and `distribution` branch unchanged.
- Lato Free stash (`stash@{0}`) untouched.

---

**Phase 2C + 2D COMPLETE** — ready for Phase 2E authorization.
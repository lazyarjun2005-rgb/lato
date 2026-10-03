# Phase 2B — Execution Limits, Retries & Failure Control

## 1. Purpose

Make Lato's agent execution bounded and resilient. Prevent unlimited tool
execution, endless consecutive failures, unbounded retry loops, and silent
terminations. Preserve normal successful coding workflows.

## 2. Architecture Findings (Pre-Implementation)

- **Effort profiles** (`internal/runtime/effort.go`): Low=6 turns, Medium=12,
  High=18, Ultra=24, Lato-X=32. Each has RepeatNudgeAfter/RepeatStopAfter.
- **Runtime loop** (`internal/runtime/runtime.go:run`): Single agent loop
  with turn limit (`prof.MaxTurns`), repeat detection (`toolSignature`),
  stall detection (`maxStallContinuations=2`), and continuation nudges.
- **Tool execution**: `executeTool` → permission gate → `manager.Execute`.
  No per-run tool-call budget, no consecutive-failure cap, no provider retries.
- **Provider streaming**: `runModelTurn` calls `provider.StreamChat` once;
  no retries on transient failures.
- **Cancellation**: `ctx.Err()` checked after each model turn and tool execution.

## 3. Implementation Summary

### 3.1 Tool Execution Budget (Step 2)
- **Config**: `limits.max_tool_calls` (default 100) in `config.yaml`.
- **Enforcement**: Counter incremented in `run()` for each `executed==true`
  tool call. Checked **before** each tool execution.
- **Behavior**: When budget reached, run terminates with clear message
  "Tool-call budget exhausted (N calls). Run terminated."
- **Validation errors**: Counted toward budget (they reach `manager.Execute`).
- **Permission denials**: NOT counted (no `manager.Execute` call).

### 3.2 Consecutive Failure Limit (Step 3)
- **Config**: `limits.max_consecutive_failures` (default 5).
- **Tracking**: Counter increments on any `result.IsError` (validation
  error, permission denial, execution error). Resets to 0 on success.
- **Enforcement**: When threshold reached, run terminates with
  "Consecutive tool failures limit reached (N). Run terminated."
- **Permission denials**: Counted as failures but do not trigger retries.
- **Resets**: On any successful tool execution (`!result.IsError`).

### 3.3 Bounded Provider Retries (Step 4)
- **Config**: `limits.provider_retries` (default 3 extra attempts, 4 total).
- **Scope**: Only for **pre-content** transient failures (connection
  errors, DNS, 429/5xx). Never retries after any content emitted.
- **Backoff**: Exponential (500ms base, ×2 per attempt) capped at 10s,
  with ±25% jitter using `time.Now().UnixNano()`.
- **Cancellation**: Respects `ctx.Done()` during backoff.
- **Non-retriable**: 4xx (except 429), auth errors, context cancellation.
- **Exhaustion**: Returns "provider retries exhausted after N attempts".

### 3.4 Clear Stop Reasons (Step 5)
- **Max turns**: "Paused: turn budget exhausted..."
- **Tool budget**: "Tool-call budget exhausted (N calls). Run terminated."
- **Consecutive failures**: "Consecutive tool failures limit reached (N). Run terminated."
- **Provider retries**: "provider retries exhausted after N attempts"
- **Cancellation**: Propagated via `ctx.Err()`
- **Normal completion**: Unchanged
- **Fatal error**: Existing `EventError` path

### 3.5 Configuration (Step 6)
```yaml
limits:
  max_tool_calls: 100               # hard cap on tool executions per run
  max_consecutive_failures: 5       # stop after this many consecutive failures
  provider_retries: 3               # extra provider attempts for transient failures
```
- **Defaults applied in `EffectiveLimits()`**: zero/negative → safe defaults.
- **Backward compatible**: missing/empty limits → defaults used.
- **Validation**: zero/negative never means "unlimited".

## 4. Provider Compatibility
- **No changes** to provider interfaces (`ModelProvider`, `StreamChat`).
- Retries happen at runtime layer (`runModelTurn`), transparent to providers.
- Ollama, OpenAI-compatible, NVIDIA, OpenRouter, 9router, Omniroute all
  inherit retry behavior automatically.
- Tool-call budget and failure limits are provider-agnostic.

## 5. Tests Added
- `internal/tools/args_test.go`: `TestValidate` (missing/empty/wrong-type
  required fields, extra keys allowed).
- `internal/tools/manager_test.go`: `TestManager_ExecuteValidatesArgsBeforeSideEffect`
  (execution counter stays 0 on invalid args).
- `internal/runtime/permissions_runtime_test.go`:
  - `TestToolBudgetExhausted` (2 calls then budget stop)
  - `TestConsecutiveFailureLimit` (4 validation errors → stop)
  - `TestProviderRetryOnTransientError` (helper function logic)
- All existing tests pass (Phase 1/1.1/2A protections preserved).

## 6. Validation Results
```
gofmt -l ./internal ./cmd       → (no files listed)
go vet ./...                     → clean
go build ./...                   → ok
go test ./...                    → all packages pass
git diff --check                 → clean
```

## 6. Remaining Limitations
- Provider retries only for pre-content failures; mid-stream errors not
  retried (cannot transparently resume stream).
- Jitter uses `time.Now().UnixNano()` — not crypto-secure but sufficient
  for retry spacing.
- `provider_retries: 0` treated as default (3); no way to disable retries
  entirely via config (documented limitation).
- Tool budget counts validation attempts; permission denials do not
  consume budget.
- No automatic fallback to another provider (deferred to future phase).

## 7. Scope Confirmation
- Phase 2C (loop/stall detection) — not started
- Phase 2D (cancellation/timeout redesign) — not started
- Phase 2E (output truncation/context budgeting) — not started
- No push, release, tag modification, or stash operations performed.

---

**Phase 2B COMPLETE** — ready for Phase 2C authorization.
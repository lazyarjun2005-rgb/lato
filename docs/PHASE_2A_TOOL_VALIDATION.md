# Phase 2A — Tool-call validation

## 1. Purpose

Make Lato's tool execution path predictable and resistant to malformed,
model-generated tool calls. Malformed calls must not cause side effects,
and must be reported back to the model as structured errors it can
recover from.

## 2. Initial architecture

Tool calls flow:

```
Provider response → Runtime.run loop → executeTool
  → perms.Classify → Decide (Allow/Deny/Ask)
  → tools.Manager.Execute → tool.Execute
```

The runtime already wrapped tool failures into a recoverable
`tools.Result{IsError: true}` that is appended to the conversation so
the model can correct itself. Permission classification and the Phase
1/1.1 sensitive-path safeguards all run inside `executeTool` before any
tool executes.

## 3. Existing validation behavior (before Phase 2A)

- Tool name *was* validated: unknown names returned `ErrNotFound`.
- Argument validation was ad-hoc, each tool using
  `tools.StringArg`/`Optional*Arg` against itself; no central contract
  check existed, so a wrong-typed argument could be silently coerced to
  a zero value inside the tool's own readers.

## 4. Problems identified

- Wrong-typed or missing arguments were not uniformly rejected before
  the tool's `Execute` ran; some paths coerced silently.
- No central validation layer existed; each tool's argument checking was
  inconsistent.
- Nothing explicitly guaranteed that a malformed call was a hard stop
  before any side effect.

## 5. New validation flow

```
Provider response → Runtime.run loop → executeTool
  → perms.Classify → Decide (Allow/Deny/Ask)
  → tools.Manager.Execute
     → registry.Lookup (name validation, unknown → ErrNotFound)
     → tools.Validate(args, tool.InputSchema())   ← new
     → tool.Execute
```

`tools.Manager.Execute` now validates the normalized argument map
against the tool's declared `InputSchema()` and returns an error
(wrapped in `ExecutionError` → `ArgumentError`) **before** invoking the
tool. Unknown tools and validation failures therefore produce zero
side effects.

The runtime already converts such errors into `tools.Result{IsError:true,
Content: "...failed: <err>"}` appended to the conversation, so the
model observes the failure and can retry with corrected arguments.

## 6. Argument contract strategy

A narrow internal validator (`tools.Validate`) enforces the one
consistent schema shape used by every built-in tool:
`{"type":"object","properties":{...},"required":[...]}` with fields
typed `string|number|integer|boolean`.

- Required fields are mandatory and must be present and typed correctly.
- Required `string` fields must be non-empty.
- Present optional fields must match their declared type.
- Boolean `"true"`/`"false"` strings are accepted (matching the
  pre-existing `OptionalBoolArg` tolerance).
- JSON numbers arrive as `float64`; the integer checker accepts
  `int`/`int64`/`float64` consistently with the existing readers.
- Extra/unknown keys are currently **not** rejected because no built-in
  schema forbids them; they are ignored by the argument readers exactly
  as before.

Validation never panics and never executes user code.

## 7. Permission integration

- Permission classification still runs before tool execution in
  `executeTool`; validation runs after classification/approval and
  inside `Manager.Execute`, so no path can reach `tool.Execute`
  without both an `Allow` decision and a valid argument set.
- Unknown tools are classified high-risk (fail-closed) before
  execution and also map to `ErrNotFound` if a permission decision
  somehow allowed them — no unknown tool can run.
- Sensitive file operations keep their `Ask` escalation; Phase 1 and
  1.1 protections are untouched.
- Argument validation cannot be used to manipulate classification: the
  classification happens first, and nothing from the validation pass is
  fed back into the classifier.

## 8. Error/result behavior

- Unknown tool → `ErrNotFound` (wrapped in `ExecutionError`).
- Malformed arguments → `ArgumentError` (wrapped in `ExecutionError`).
- Permission denied / not approved → structured `tools.Result{IsError:true}`
  via `refusal(...)`.
- Valid authorized call → normal success result.

All of the above become recoverable results; none crash the runtime or
terminate the request. Tool-call IDs, provider tool roles, and
conversation continuation are unchanged.

## 9. Provider compatibility

Argument maps (`map[string]any`) are already normalized by each
provider (Ollama/NVIDIA/OpenAI-compatible/NDVIA fall back to `map` or
parse the JSON arguments string), so validation operates on the same
normalized shape regardless of backend. The runtime loop and tool
result contract are unchanged, preserving Ollama, OpenRouter, 9router,
Omniroute, NVIDIA, and OpenAI-compatible behavior.

## 10. Tests added

- `internal/tools/args_test.go` — `TestValidate` table covering valid,
  missing/empty required, wrong types, enum-like issues, null, and
  extra keys.
- `internal/tools/manager_test.go` —
  `TestManager_ExecuteValidatesArgsBeforeSideEffect` (execution counter
  stays 0 on invalid args, exactly 1 on a valid call).
- `internal/runtime/permissions_runtime_test.go` —
  `TestMalformedArgumentsYieldToolErrorWithoutExecution` and
  `TestDeniedPermissionDoesNotExecute`, proving wrong-type arguments and
  sensitive-path denial never execute the tool.
- Existing tests for sensitive paths (`internal/permissions`),
  repository filtering, symlink behavior, and session persistence are
  preserved and passing.

## 11. Validation results

```
gofmt -l ./internal          → (no files listed)
go test ./...                → all packages pass
go vet ./...                 → clean
go build ./...               → ok
git diff --check             → clean
```

## 12. Remaining limitations

- `tools.Validate` enforces the single shared schema contract, not the
  full JSON Schema specification (no nested object/array schemas or
  `enum` values exist in tool definitions today).
- Unknown argument keys are ignored, matching current reader behavior;
  no tool has `additionalProperties: false`.
- The validation pass is argument-shape only; it does not verify
  semantics (e.g. that a `path` exists or stays inside the workspace —
  that remains the classification/boundary responsibility).
- Not a sandbox, not race-free (as in Phase 1).

## 13. Scope statement

Phase 2B (execution limits/retries), 2C (loop/stall detection), 2D
(cancellation/timeout redesign), and 2E (output truncation/context
budgeting) were **not** implemented. No new provider systems, plan mode,
agent teams, MCP, or related subsystems were added. No files or Git
history were changed beyond this phase's implementation on the
`feature/phase-2a-tool-validation` branch.

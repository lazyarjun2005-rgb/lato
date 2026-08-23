# Effort

Package: `internal/effort`, orchestration in `internal/runtime`, provider capabilities in `internal/providers`

Effort is Lato's first-class agent-thoroughness setting (M16). It is not
a cosmetic label: the selected level changes both how Lato talks to the
model provider and how the M10 agent loop orchestrates work.

## The Ladder

```text
low → medium → high → ultra → lato-X
```

| Level | Meaning |
| ----- | ------- |
| `low` | Fast and direct. Minimal tool calls and analysis; verification only where correctness requires it. |
| `medium` | Balanced. Normal Lato behavior — identical to pre-M16 releases, and the default. |
| `high` | Serious coding mode (recommended for real tasks). Thorough inspection, strong verification, deeper failure recovery. |
| `ultra` | Deep bounded agentic mode: wider planning, extra targeted checks, deliberate replanning. |
| `lato-X` | Maximum bounded orchestration: best decomposition, uncertainty-driven re-inspection, strongest verification — inside every existing safety bound. |

## What Actually Changes

### 1. Provider request configuration

Whether an effort parameter is sent to the model is decided by the
provider capability layer (`providers.EffortCapabilityFor`). Providers
absent from that table receive **no** effort field at all — Lato never
sends a parameter a provider has not declared support for.

| Provider | Mechanism | Mapping |
| -------- | --------- | ------- |
| OpenRouter | `"reasoning": {"effort": ...}` | low→low, medium→medium, high/ultra/lato-X→high (declared set) |
| Ollama, LM Studio, NVIDIA NIM, 9Router, OmniRoute, custom `/connect` | none | Lato-side orchestration only |

The table is authoritative and data-driven: a capability entry listing
`low, medium, high, xhigh, max` automatically lets ultra send `xhigh`
and lato-X send `max` without any code change.

### 2. Agent orchestration (every provider)

Each level maps to a bounded profile for the existing M10 loop:

| Level | Max model turns | Repeat nudge / stop | Prompt guidance |
| ----- | --------------- | -------------------- | ---------------- |
| low | 6 | 2 / 3 | brevity + minimal tool use |
| medium | 12 | 3 / 4 | none (pre-M16 behavior) |
| high | 18 | 3 / 4 | thorough inspection, real verification |
| ultra | 24 | 4 / 5 | deep planning, targeted checks |
| lato-X | 32 | 4 / 5 | maximum thoroughness inside bounds |

Every level remains strictly bounded: turn limits, repetition detection,
permission gating (M13), task checkpointing (M12), honest completion
(M15) all stay fully in force. **Lato-X is not unsafe mode** — it is
maximum effort within the safety architecture.

## Commands

```text
/effort              show current level and the ladder
/effort low          switch (persisted as default)
/effort medium
/effort high
/effort ultra
/effort lato-x
```

Inside `/model`, `←`/`→` walk the ladder and Enter saves model + effort;
`s` applies the selection to the current session only (config.yaml is
not rewritten). The header always shows the active level:

```text
Lato · agent: default · 9router/oc/big-pickle · lato-X
```

## Configuration

`config.yaml` accepts `model.effort`:

```yaml
model:
  provider: ollama
  endpoint: http://localhost:11434
  name: llama3
  effort: high   # optional; empty = medium
```

An invalid value fails startup with the valid choices named rather than
being silently reinterpreted.

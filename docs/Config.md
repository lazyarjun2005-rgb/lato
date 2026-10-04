# Config

Package: `internal/config`

The `config` package loads, validates, and manages Lato's global YAML configuration file.

## Purpose

Configuration tells Lato which model provider to use, which model to call, and which base agent instructions to apply. Lato stores this data on the local machine.

## File Location

Lato stores configuration in the operating system's user configuration directory:

| Platform | Location |
| -------- | -------- |
| Linux    | `~/.config/lato/config.yaml` |
| macOS    | `~/Library/Application Support/lato/config.yaml` |
| Windows  | `%AppData%\Lato\config.yaml` |

The `LATO_HOME` environment variable overrides the platform location entirely.

On first run, `Load` creates the Lato configuration directory and writes a default configuration file if the file does not exist. A legacy `~/.lato` home from earlier versions is migrated once by copying; the original files are never rewritten or deleted.

## Configuration Shape

```yaml
model:
  provider: ollama
  endpoint: http://localhost:11434
  name: ornith:9b

agent:
  name: default
  system_prompt: |
    You are a helpful coding assistant.
```

### `model`

| Field      | Required | Description                                              |
| ---------- | -------- | -------------------------------------------------------- |
| `provider` | Yes      | One of `ollama`, `lmstudio`, `nvidia`, `openrouter`, `9router`, `omniroute`. |
| `endpoint` | Yes      | Base URL of the provider, for example `http://localhost:11434`. Overridable per provider for cloud installations. |
| `name`     | Yes      | Model name or ID, for example `ornith:9b` or `vendor/model:variant`. Model IDs are opaque and never split on `/` or `:`. |

### API Keys

Hosted providers read their key from one environment variable, declared
per provider in the registry:

| Provider    | Environment variable |
| ----------- | -------------------- |
| `openrouter` | `OPENROUTER_API_KEY` |
| `9router`    | `NINEROUTER_KEY`     |
| `omniroute`  | `OMNIROUTE_KEY`      |
| `nvidia`     | `NVIDIA_API_KEY`     |
| `ollama` / `lmstudio` | none (local) |

Keys exist only in memory for the running session: they are never
written to `config.yaml`, never logged, and never included in error
messages. If a selected provider requires a key that is not set, Lato
fails fast with an error such as `OPENROUTER_API_KEY is not set`.

### User-Level Provider Connections

Providers connected through `/connect` (including custom ones and
imports from OpenCode/Claude Code) are stored separately, under the
operating system's user configuration directory:

```text
Linux/macOS: ~/.config/lato/providers.json   (via os.UserConfigDir)
Windows:     %AppData%\Lato\providers.json
```

This file stores endpoints, API keys, and cached model lists. It is
never inside a project repository and is written with restrictive
permissions (0600). Resolution order for a provider's credentials:

1. Saved `/connect` configuration (explicit or imported)
2. Environment variable from the table above
3. `config.yaml` endpoint / registry defaults

Environment variables remain fully supported as a fallback; `/connect`
is simply not required when they are already set.

### `agent`

| Field           | Required | Description                                      |
| --------------- | -------- | ------------------------------------------------ |
| `name`          | No       | Display name of the default agent.               |
| `system_prompt` | No       | Base system prompt for the agent.                |

### `limits`

All fields are optional. A missing, zero, or negative value selects the
default, so an unset or hand-edited key can never silently mean
"unlimited".

```yaml
limits:
  max_tool_calls: 100
  max_consecutive_failures: 5
  provider_retries: 3
  max_tool_output: 65536
  context_budget: 131072
  max_history_turns: 20
```

| Field                      | Default  | Description                                                                |
| -------------------------- | -------- | -------------------------------------------------------------------------- |
| `max_tool_calls`           | `100`    | Hard cap on tool executions per run.                                        |
| `max_consecutive_failures` | `5`      | Stop after this many consecutive tool failures.                             |
| `provider_retries`         | `3`      | Extra provider attempts for transient, pre-content failures.                |
| `max_tool_output`          | `65536`  | Bytes a single tool result may contribute (Phase 2E).                       |
| `context_budget`           | `131072` | Bytes of history sent to the provider on one model turn (Phase 3A).         |
| `max_history_turns`        | `20`     | User turns of history sent on one model turn (Phase 3A).                    |

#### History budgeting (`context_budget`, `max_history_turns`)

The budget is applied on **every model turn**, not once when a request
starts, because the working set grows with each tool result.

The bound is **bytes, not tokens**. Lato supports providers with
different tokenizers and has no token estimator, so a byte limit is
reported as a byte limit.

Trimming never breaks message structure. History is split into
indivisible units — a plain message, or an assistant message together
with all of its tool results — and whole units are evicted, so a tool
result is never separated from the call that requested it.

Two units are pinned regardless of budget:

- the unit holding the **current user request**, so a budget can never
  silently drop what the user just asked;
- the **newest unit**, which during a tool loop is the assistant message
  and its results, so the model is never asked to act on a tool call
  whose output it has not seen.

When those two pins do not both fit within `context_budget`, the budget
is exceeded rather than either pin broken. Everything between them is
still evicted normally, and a single gap may appear between the pinned
request and the retained recent run.

Leading system messages are pinned separately and are **not** counted
against `context_budget`.

## Functions

### `Dir`

```go
func Dir() (string, error)
```

Returns the Lato home directory (`~/.lato`) and creates it if needed.

### `Path`

```go
func Path() (string, error)
```

Returns the full path to `config.yaml`.

### `Load`

```go
func Load() (*Config, error)
```

Loads the configuration file.

Behavior:

1. Resolve the config path.
2. If the file does not exist, write the default template.
3. Read and parse the YAML file.
4. Validate required model fields.
5. Return a `Config` value or an error.

## Validation

`Load` fails early if required model fields are missing:

- `model.provider` must not be empty.
- `model.endpoint` must not be empty.
- `model.name` must not be empty.

## Design Notes

- Configuration is local. Lato does not send config data to a remote service.
- The default file gives a first-time user a working starting point.
- Runtime model and provider switches (`/model`, `/provider`) are persisted to `config.yaml` once the switch succeeds.
- API keys come only from environment variables and are never round-tripped to disk.

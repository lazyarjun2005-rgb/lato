# Providers

Package: `internal/providers`

The `providers` package defines the model provider interface and its implementations. A provider sends messages to a language model and streams the reply.

## Purpose

Lato talks to models through one interface. The runtime owns the agent loop. Each provider only streams one model turn. This design lets you add a second provider later without changing agent or runtime code.

## Core Types

### `ModelProvider`

```go
type ModelProvider interface {
    StreamChat(
        ctx context.Context,
        messages []Message,
        tools []tools.Definition,
    ) (<-chan StreamEvent, error)
}
```

| Parameter  | Description                                              |
| ---------- | -------------------------------------------------------- |
| `ctx`      | Cancellation and deadline control.                       |
| `messages` | Conversation history, including system and tool messages.|
| `tools`    | Tool definitions available for this model turn.          |

The channel emits `StreamEvent` values until the model turn ends or an error occurs.

### `Message`

A single conversation message.

| Field        | Description                                      |
| ------------ | ------------------------------------------------ |
| `Role`       | `system`, `user`, `assistant`, or `tool`.        |
| `Content`    | Text content of the message.                     |
| `ToolCalls`  | Tool calls requested by an assistant message.    |
| `ToolCallID` | ID that links a tool result to a tool call.      |
| `Name`       | Tool name for tool-result messages.              |

### `ToolCall`

| Field       | Description                          |
| ----------- | ------------------------------------ |
| `ID`        | Unique ID for the tool call.         |
| `Name`      | Name of the tool to run.             |
| `Arguments` | Argument map for the tool.           |

### `Response`

| Field       | Description                                      |
| ----------- | ------------------------------------------------ |
| `Content`   | Full text assembled from streamed chunks.        |
| `ToolCalls` | Tool calls collected during the model turn.      |

### `StreamEvent`

One piece of a streaming model turn.

| Field       | Description                                      |
| ----------- | ------------------------------------------------ |
| `Text`      | Incremental text from the model.                 |
| `Thinking`  | Optional thinking content from the model.        |
| `ToolCalls` | Tool calls reported in this chunk.               |
| `Done`      | True when the provider marks the turn complete.  |
| `Err`       | Non-nil if the stream failed.                    |

## Supported Implementations

Lato has exactly two HTTP implementations. Every provider is one of:

| Class                | Implementation   | Used by                                            |
| -------------------- | ---------------- | -------------------------------------------------- |
| `ollama`             | `OllamaProvider` | Ollama                                             |
| `openai-compatible`  | `NvidiaProvider` | LM Studio, NVIDIA NIM, OpenRouter, 9Router, OmniRoute |

The `openai-compatible` class speaks the standard OpenAI Chat Completions
API: `POST {endpoint}/chat/completions` with SSE streaming and
`GET {endpoint}/models` for discovery, with optional Bearer
authentication. Construct it with `NewOpenAICompatible(endpoint, model,
apiKey, client)`. Model IDs are opaque strings — IDs such as
`vendor/model` or `cc/glm-4:variant` are never split or rewritten.

### Provider Registry

`providers.Registry` is the single catalog of known providers:

| ID           | Name        | Default endpoint                          | API key env          |
| ------------ | ----------- | ----------------------------------------- | -------------------- |
| `ollama`     | Ollama      | `http://localhost:11434`                  | none                 |
| `lmstudio`   | LM Studio   | `http://localhost:1234/v1`                | none                 |
| `nvidia`     | NVIDIA NIM  | `https://integrate.api.nvidia.com/v1`     | `NVIDIA_API_KEY`     |
| `openrouter` | OpenRouter  | `https://openrouter.ai/api/v1`            | `OPENROUTER_API_KEY` |
| `9router`    | 9Router     | `http://localhost:20128/v1`               | `NINEROUTER_KEY`     |
| `omniroute`  | OmniRoute   | `http://localhost:8787/v1` (override via config) | `OMNIROUTE_KEY` |

Adding a new OpenAI-shaped provider means adding one registry entry —
no runtime or TUI changes.

### Local-First

Lato remains local-first. Ollama works fully offline and is the default
provider. Online providers (OpenRouter, NVIDIA NIM) are optional and are
only contacted after you explicitly switch to them with `/provider`.
9Router and OmniRoute default to local endpoints; set `model.endpoint`
in `config.yaml` for a cloud installation. API keys are read only from
environment variables, never stored on disk or printed.

### Connecting Providers Interactively (`/connect`)

Environment variables are never required. `/connect` opens an
interactive wizard: pick a provider, enter its base URL and/or API key
(keys are masked while typing), and Lato validates the connection with a
lightweight `GET {endpoint}/models` call, saves it, and opens the model
picker.

| Provider | Prompts | Notes |
| -------- | ------- | ----- |
| Ollama | Base URL | Works fully offline |
| LM Studio | Base URL | Local, no key |
| OpenRouter | API key | Fixed cloud endpoint |
| NVIDIA NIM | API key | Fixed cloud endpoint |
| 9Router | Base URL + API key | Key optional for unauthenticated local installs |
| OmniRoute | Base URL + API key | Endpoint configurable per installation |
| Other / Custom | Name, Base URL, optional key | Uses the shared OpenAI-compatible implementation |

Failed custom-provider validation offers a manual model-ID fallback so
gateways without `/models` can still be saved; `/model refresh` can
discover their models later once the endpoint responds.

Connections persist in the user-level store (never inside a project):

```text
Linux/macOS: ~/.config/lato/providers.json   (via os.UserConfigDir)
Windows:     %AppData%\Lato\providers.json
```

The file holds API keys in plain text but is written with restrictive
permissions (0600 file, 0700 directory). Keys are redacted as `***`
everywhere they could otherwise surface — transcript messages, errors,
import previews.

### Importing Existing Configurations

`/connect import` (or `/import`) scans for provider configurations you
already have and lets you import them explicitly:

- **OpenCode**: parses `opencode.json` / `opencode.jsonc` (user config
  dir and project root). Providers whose `npm` package is
  `@ai-sdk/openai-compatible` or `@ai-sdk/openai` are converted,
  including base URL, key, and model list.
- **Claude Code**: reads `~/.claude/settings.json` (or environment) for
  `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN`, importing them only when
  the endpoint clearly aims at an OpenAI-compatible router (the
  documented 9Router port or an `/v1` path). Native Anthropic endpoints
  are not supported by Lato providers and are refused with an
  explanation.

Detection only reads files; nothing executes and nothing is saved until
you confirm in the picker.

### Credential Precedence

When building a request, Lato resolves provider settings deterministically:

1. Explicit user configuration (`/connect`)
2. Imported configuration (also a saved connection)
3. Environment variables named by the registry (`OPENROUTER_API_KEY`, ...)
4. Defaults from the registry (`config.yaml` endpoint remains the fallback)

A saved connection always wins over environment variables. Switching to
a hosted provider with no configuration and no environment key shows
"Run /connect" guidance instead of failing mid-request.

### Grouped Model Picker

`/model` lists models grouped per connected provider using cached
discovery results; the active provider's section is fetched live.
`/model refresh` re-runs discovery for every configured provider and
updates the cache. Model IDs are opaque strings and are displayed
exactly as providers report them.

### Custom Models (`/model add`)

Some providers (9Router dashboards, for example) serve models their
`/models` endpoint does not list. `/model add` registers one by hand:
pick a connected provider, type the exact model ID, optionally give it
a display name. The ID is stored verbatim in the user-level connection
store and later sent to the provider unchanged — Lato never parses,
splits, or normalizes it.

- Custom models appear under their provider in the grouped `/model`
  picker and stay selectable even when live discovery does not return
  them.
- `/model refresh` preserves custom models; if the provider starts
  returning the same ID, it is listed once.
- If the provider rejects the model at request time, the real API error
  is shown normally; the registration is never removed automatically.

### Ollama

Type: `OllamaProvider`

| Field      | Description                                            |
| ---------- | ------------------------------------------------------ |
| `Endpoint` | Ollama base URL, for example `http://localhost:11434`. |
| `Model`    | Model name, for example `ornith:9b`.                   |

`StreamChat` posts to the Ollama `/api/chat` endpoint with streaming enabled. It converts Lato messages and tool definitions into the Ollama request shape, then maps each response chunk to a `StreamEvent`.

## How the Runtime Uses a Provider

1. The runtime selects a provider from configuration.
2. The runtime builds the message list, including the system prompt.
3. The runtime calls `StreamChat` with current tool definitions.
4. The runtime assembles text and tool calls from stream events.
5. If the model requests tools, the runtime executes them and starts another model turn.

## Design Notes

- Providers do not execute tools. The runtime executes tools.
- Providers do not manage sessions. The session package stores history.
- Unsupported provider names fail fast with a clear error.
- A provider that requires an API key fails construction immediately when the variable is unset, before any HTTP call.
- Tool calling is never disabled for online providers: streamed tool-call fragments are assembled into normal `ToolCall` values and executed by the runtime exactly as with Ollama.

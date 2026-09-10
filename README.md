# Lato (`lato`)

## Overview

Lato is a local-first command line tool for running AI agents. It uses a model provider, agent instructions, skills, tools, and sessions.

Lato runs as a single binary.

It does not require:

- A user account
- A cloud service
- Remote data processing
- Telemetry

Lato is under active development. Features and interfaces can change.

---

# Install

## Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/lazyarjun2005-rgb/lato/v1.0.9/scripts/install.sh | sh
```

The installer downloads the prebuilt binary for your platform (amd64 or
arm64), verifies its SHA-256 checksum against the release's
`checksums.txt`, and installs it to `~/.local/bin/lato`. No Go toolchain
or sudo is required. If `~/.local/bin` is not on your `PATH`, the
installer prints the exact line to add.

## Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/lazyarjun2005-rgb/lato/v1.0.9/scripts/install.ps1 | iex
```

The installer detects amd64 or arm64, downloads the matching `lato.exe`
from the GitHub release, verifies its checksum, and installs it to
`%LOCALAPPDATA%\Programs\Lato\lato.exe` (per-user, no administrator
rights), adding that directory to your user `PATH` if needed.

## npm

```bash
npm install -g lato-cli@1.0.9
```

The npm package downloads the same official release binary for your
platform during installation; the `lato` command itself is the native
binary and does not need Node at runtime.

## Build from source

Requires Go 1.26 or later:

```bash
git clone https://github.com/lazyarjun2005-rgb/lato
cd lato
go build -o lato .
./lato
```

On Windows PowerShell:

```powershell
go build -o lato.exe .
.\lato.exe
```

To install into your Go bin directory: `go install .`

Verify any installation:

```bash
lato --version   # lato v1.0.9
lato doctor      # environment check
```

---

# Main Features

Lato provides:

- Interactive terminal interface with streaming responses
- Local model execution through Ollama (fully offline)
- OpenAI-compatible providers: LM Studio, NVIDIA NIM, OpenRouter, 9Router, OmniRoute
- Agent tools: file read/write/edit, repository search, command execution
- Agent skills (loaded on demand)
- Session storage, resume, rewind, and branching
- Model provider abstraction with interactive `/connect`
- Effort/reasoning control
- A permission system for every agent action

---

# Providers

| Provider | Endpoint | API key |
| -------- | -------- | ------- |
| Ollama | http://localhost:11434 | none (local) |
| LM Studio | http://localhost:1234/v1 | none (local) |
| NVIDIA NIM | https://integrate.api.nvidia.com/v1 | `NVIDIA_API_KEY` |
| OpenRouter | https://openrouter.ai/api/v1 | `OPENROUTER_API_KEY` |
| 9Router | http://localhost:20128/v1 | `NINEROUTER_KEY` |
| OmniRoute | configurable | `OMNIROUTE_KEY` |

Hosted providers are **bring-your-own-key**: Lato never bundles an API
key. Set the environment variable or use `/connect` to save a connection
locally (keys are stored on your machine only and are redacted in all
output).

---

# Start Lato

```bash
cd ~/some-project
lato
```

Lato uses your current directory as its workspace.

One-shot prompt:

```bash
lato run "summarize the README"
```

---

# Commands

Session and model control:

```text
/model [name|add|refresh]   Show or switch model; register custom IDs; refresh lists
/provider [name]            Show or switch provider
/connect [import]           Connect a provider interactively; import from OpenCode/Claude Code
/effort [level]             Show or switch effort: low, medium, high, ultra, lato-x
/fast                       Toggle conversational small-talk mode
```

Session control:

```text
/sessions                   Open the saved session picker
/resume [id]                Resume a previous session
/rename <title>             Rename the current session
/rewind [n]                 Drop the last n exchanges
/branch [title]             Branch the conversation
/export                     Export the transcript as Markdown
/copy [transcript]          Copy the response (or transcript) to the clipboard
/clear                      Clear the visible transcript
/exit                       End the session
```

Project state:

```text
/memory [add|remove|clear]  Persistent project memory
/task                       List tasks; resume/abandon paused work
/permissions [reset]       Show the permission policy; clear approvals
/workspace                  Describe the current repository
/index                      Show the repository index summary
```

Development:

```text
/search, /explain, /debug, /fix, /test, /build, /run, /review, /refactor, /code
```

Diagnostics:

```text
/doctor                     Environment check
/version                    Show the running Lato version
/help                       List available commands
```

---

# Tools

The agent's built-in tools include:

```text
read_file, write_file, create_file, edit_file
list_files, pwd
search_repo, read_repo_file
run_command
load_skill
remember_project_fact, update_project_memory, forget_project_memory, recall_project_memory
```

`run_command` executes a single program with arguments in the workspace
(`go test ./...`, `npm test`) — directly, without a shell — under a
timeout, with bounded output capture. A nonzero exit code is a normal
result the agent reads and reacts to.

Skills are Markdown files. The model sees only a skill catalog; full
skill bodies are fetched on demand through `load_skill`.

---

# Security and Permissions

Lato checks every agent action before it runs:

- Reads and normal in-workspace edits run automatically.
- Destructive operations (deletions, `git reset --hard`, force pushes,
  anything outside the workspace) pause and ask: allow once, allow for
  task, or deny.
- Commands run without a shell, with working-directory confinement to
  the workspace and bounded output/timeouts.
- API keys are read from environment variables or the local `/connect`
  store, never bundled with Lato, and redacted in any output.
- Sessions, config, and memory stay on your machine.

`/permissions` shows the current policy; `/permissions reset` clears
temporary approvals.

---

# Configuration

Lato creates the configuration file during the first run.

Location:

```text
Linux:    ~/.config/lato/config.yaml
macOS:    ~/Library/Application Support/lato/config.yaml
Windows:  %AppData%\Lato\config.yaml
```

Example:

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

Skills live in the `skills/` subdirectory of the same location.

---

# Sessions

Lato saves chat sessions locally at `.lato/sessions/` in the workspace.

Use `/sessions` to browse, `/resume` to continue, `/rewind` to undo
exchanges, `/branch` to explore alternatives, and `/export` to save a
transcript as Markdown.

---

# Project Structure

```text
lato/
├── main.go
├── cmd/            # CLI entry points (lato, chat, run, doctor)
├── internal/
│   ├── agent/      # Agent identity and system prompt
│   ├── command/    # Slash command framework
│   ├── config/     # Configuration handling
│   ├── providers/  # Model providers
│   ├── runtime/    # Agent execution loop
│   ├── session/    # Session storage
│   ├── skills/     # Skill loading
│   ├── tools/      # Tool system
│   └── tui/        # Terminal interface
├── npm/lato-cli/   # npm distribution wrapper
├── scripts/        # Installers
└── .github/workflows/  # CI and release
```

---

# Design Rules

Lato follows these rules:

- Keep the runtime small.
- Keep components separate.
- Store user data locally.
- Allow replacement of models and tools.
- Avoid unnecessary system requirements.

---

# Purpose

Lato provides a simple runtime for local AI agents.

The model provides intelligence.

The tools provide actions.

The skills provide instructions.

The runtime connects these components.

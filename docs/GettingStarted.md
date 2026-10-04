# Getting Started

This page explains how to install Lato, configure it, and run your first agent session.

## Requirements

Before you start, make sure you have:

- [Ollama](https://ollama.com) installed and running using `ollama serve`
- At least one local model installed
- Go 1.26 or later, only if you build from source

Example:

```bash
ollama pull ornith:9b
ollama serve
```

## Install

Pick one method. All of them install the same official prebuilt release
binary.

### Prebuilt installer (recommended, no Go required)

Linux/macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/lazyarjun2005-rgb/lato/v1.2.1/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/lazyarjun2005-rgb/lato/v1.2.1/scripts/install.ps1 | iex
```

The installer downloads the binary for your platform from the
[v1.2.1 release](https://github.com/lazyarjun2005-rgb/lato/releases/tag/v1.2.1),
verifies its SHA-256 checksum, and installs it per-user:

- Linux/macOS: `~/.local/bin/lato`
- Windows: `%LOCALAPPDATA%\Programs\Lato\lato.exe` (PATH updated automatically)

If `~/.local/bin` is not on your PATH, the installer prints the exact
line to add — it never edits your shell files.

Verify:

```bash
lato --version   # lato v1.2.1
lato doctor
```

### npm

```bash
npm install -g lato-cli@1.0.9
```

### Build from source

1. Clone the repository:

```bash
git clone https://github.com/lazyarjun2005-rgb/lato
cd lato
```

2. Build the binary:

```bash
go build -o lato .
```

3. Run Lato:

```bash
./lato
```

On Windows PowerShell:

```powershell
go build -o lato.exe .
.\lato.exe
```

## First Run

On first run, Lato creates its configuration in the operating system's user
configuration directory:

```text
Linux:    ~/.config/lato/config.yaml
          ~/.config/lato/skills/
macOS:    ~/Library/Application Support/lato/config.yaml
Windows:  %AppData%\Lato\config.yaml
```

The `LATO_HOME` environment variable overrides the location. A legacy
`~/.lato` directory from earlier versions is copied across once.

The default configuration uses Ollama:

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

Edit `config.yaml` if your model name or endpoint is different.

## Start a Chat

```bash
lato
```

Type a prompt:

```text
› explain this repository
```

Lato sends the prompt to the local model, runs any requested tools, and shows the streamed reply in the terminal.

## Useful Slash Commands

| Command              | Action                                      |
| -------------------- | ------------------------------------------- |
| `/help`              | List available commands.                    |
| `/model`             | Show the active model.                      |
| `/model <name>`      | Switch model for the next request.          |
| `/provider`          | Show the active provider.                   |
| `/effort`            | Show the agent effort level and ladder.     |
| `/effort <level>`    | Switch effort: `low`, `medium`, `high`, `ultra`, `lato-x`. |
| `/copy`              | Copy the latest response to the clipboard (`/copy transcript` for everything, `Alt+C` as a shortcut). |
| `/memory`            | List persistent project memory (`add TEXT`, `remove ID`, `clear` to manage). |
| `/task`              | List tasks; `/task resume [id]` continues paused work, `/task abandon <id>` retires it. |
| `/permissions`       | Show the permission policy; `/permissions reset` clears temporary approvals. |
| `/sessions`          | Open the saved session picker.              |
| `/workspace`         | Describe the current repository.            |
| `/index`             | Show the repository index summary.          |
| `/clear`             | Clear the visible transcript.               |
| `/exit`              | End the session.                            |

Typing `/` in the input opens a command autocomplete palette — `↑`/`↓`
to choose, `Enter` to run, `Tab` to fill without running, `Esc` to
dismiss.

### Effort

Effort controls how hard Lato works on a task:

```text
low → medium → high → ultra → lato-X
```

`medium` is the balanced default, `high` is recommended for serious
coding, and `lato-X` is maximum thoroughness inside Lato's safety
bounds. Change it with `/effort <level>`, or press `←`/`→` inside the
`/model` picker (Enter saves model + effort; `s` applies them to this
session only). The header always shows the active level. Providers that
declare no effort parameter simply get more (or less) agent-side
orchestration instead — see [Effort](Effort.md).

## Permissions and Safety

Lato checks every action before it runs. Harmless reads and normal
edits inside your project happen automatically; risky work pauses and
asks you first:

```text
Permission required

Risk: destructive or outside the workspace
Action:
  Delete directory ./build

Reason:
  deletes files

[1] Allow once   [2] Allow for task   [3] Deny
```

- **Allow once** approves exactly that action.
- **Allow for task** approves matching actions for the current task
  only — never globally, and never after a restart.
- **Deny** returns "Permission denied" to the agent, which then chooses
  another approach. Nothing is modified.
- `/permissions` shows the current policy state; `/permissions reset`
  clears temporary approvals immediately.

Deletions, destructive git commands (`reset --hard`, `clean -fd`,
force pushes), and anything targeting paths outside the workspace
always require confirmation. Commands such as `rm -rf .`, `rm -rf /`,
or `go test && rm -rf something` are recognized as dangerous as a
whole — not by matching a fixed list of exact strings.

## Work in Any Project

Lato operates on the project you launch it in, not on its own source
tree. Anywhere Lato discovers a project root — by `git`, `go.mod`,
`package.json`, `Cargo.toml`, and similar markers — it will analyze that
project. Lato can be used without installing it into the project
directory; build it once, put the `lato` binary on your `PATH`, then run
it from any project:

```bash
# Linux / macOS
cd ~/Projects/my-project
lato

# Windows PowerShell
cd C:\Projects\my-project
.\lato.exe
```

Then `lato` treats `~/Projects/my-project` (or the Windows equivalent)
as the workspace: `/workspace`, `/index`, repository context, and the
repository search tools all target that project.

## One-Shot Prompt

## One-Shot Prompt

```bash
lato run "summarize the README"
```

This path does not open the interactive TUI. It prints the final model response and exits.

## Add a Skill

1. Create a Markdown file in `~/.lato/skills/`.
2. Optionally add YAML frontmatter with `id`, `name`, and `description`.
3. Restart Lato so the skill store reloads.
4. Ask the agent a task that needs the skill. The model can load it with `load_skill`.

Example skill:

```md
---
id: clean-code
name: Clean Code
description: Prefer simple, readable code changes.
---

Prefer small functions.
Use clear names.
Avoid unnecessary abstraction.
```

## What Stays Local

Lato is local-first. It does not require:

- a user account
- a cloud service
- remote data processing
- telemetry

Your configuration, skills, and sessions stay on your machine.

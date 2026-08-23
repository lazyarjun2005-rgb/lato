# Lato (`lato`)

## Overview

Lato is a local-first command line tool for running AI agents. It uses a local model provider, agent instructions, skills, tools, and sessions.
Lato runs as a single binary.

It does not require:

- A user account
- A cloud service
- Remote data processing
- Telemetry

Lato is under active development. Features and interfaces can change.

---

# Main Features

Lato provides:

- Local model execution through Ollama
- Interactive terminal interface
- Streaming responses
- Agent skills
- Agent tools
- Session storage
- Session recovery
- Model provider abstraction

---

# Requirements

Before you use Lato, make sure that you have:

- Go version 1.22 or later
- Ollama installed
- A local model installed

Example:

```bash
ollama pull ornith:9b
```

---

# Build

To build Lato, run:

```bash
go build -o lato .
```

The command creates the `lato` executable.

---

# Install Globally

To make plain `lato` work from any terminal and any project directory,
install it into your Go bin directory:

```bash
go install .
```

The binary lands in `$GOBIN` (or `$GOPATH/bin`, or `~/go/bin`). If that
directory is not on your `PATH`, add it — for example:

```bash
export PATH="$PATH:$HOME/go/bin"
```

Or use the installer script, which handles this and prints the exact
`PATH` line when needed (it never edits your shell files):

```bash
./scripts/install.sh                       # Linux/macOS
PREFIX="$HOME/.local/bin" ./scripts/install.sh   # install elsewhere
.\scripts\install.ps1                      # Windows PowerShell
```

Verify with:

```bash
which lato     # should print the installed binary path
lato doctor    # environment check
```

Then start Lato from any project directory:

```bash
cd ~/some-project
lato
```

Lato always uses your current directory as its workspace — never the
directory where the binary or its source code lives.

---

# Start Lato

Run:

```bash
lato.exe
```

Lato starts the interactive terminal interface.

Example:

```text
> explain this repository
```

---

# System Operation

Lato processes a request in the following order:

```text
User Input
    |
    v
Command Handler
    |
    v
Agent Runtime
    |
    +-- Skills
    |
    +-- Memory
    |
    +-- Tools
    |
    v
Model Provider
    |
    v
Response
```

The runtime separates each function.

This allows each part to be changed without changing the complete system.

---

# Commands

Lato supports these commands:

```text
/help
Shows available commands.
```

```text
/sessions
Shows stored sessions.
```

```text
/resume <session-id>
Loads a previous session.
```

```text
/tools
Shows available tools.
```

```text
/memory
Manages agent memory.
```

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

The configuration file defines:

- Model provider
- Model endpoint
- Model name
- Agent instructions

---

# Skills

Skills are Markdown files.

Skills provide additional instructions for the agent.

Location:

```text
Linux:    ~/.config/lato/skills/
macOS:    ~/Library/Application Support/lato/skills/
Windows:  %AppData%\Lato\skills\
```

Example:

```md
# Go Development

Use the Go language standard.

Prefer simple designs.

Use clear error handling.
```

Lato loads all Markdown skill files during agent startup.

---

# Tools

Tools allow the agent to perform actions.

Built-in tools include:

```text
read_file
write_file
list_files
shell
```

A tool can:

- Receive input from the agent
- Perform an operation
- Return a result

---

# Sessions

Lato saves chat sessions locally.

Session files are stored at:

```text
.lato/sessions/
```

Example:

```text
.lato/
└── sessions/
    ├── session-a.json
    └── session-b.json
```

Use `/sessions` to view saved sessions.

Use `/resume` to continue a previous session.

---

# Project Structure

```text
lato/
├── cmd/
│   └── root.go

├── internal/
│   ├── agent/
│   │   Agent runtime
│   │
│   ├── command/
│   │   Command handling
│   │
│   ├── config/
│   │   Configuration handling
│   │
│   ├── providers/
│   │   Model providers
│   │
│   ├── runtime/
│   │   Agent execution
│   │
│   ├── session/
│   │   Session storage
│   │
│   ├── skills/
│   │   Skill loading
│   │
│   ├── tools/
│   │   Tool system
│   │
│   └── tui/
│       Terminal interface
│
└── examples/
    └── skills/
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

# Future Development

Planned features:

- Improved session selection
- Tool permission control
- Improved memory system
- Additional model providers
- Agent profiles
- Plugin support
- Improved agent planning

---

# Purpose

Lato provides a simple runtime for local AI agents.

The model provides intelligence.

The tools provide actions.

The skills provide instructions.

The runtime connects these components.
```
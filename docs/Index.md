# Documentation for Lato

Lato is an open source, lightweight, local-first agent harness for running specialized AI agents. It is written in Go. This is the documentation for Lato.
You can find the index of the contents in this file.

## Guides

| Name                                        | Description                                                         |
| ------------------------------------------- | ------------------------------------------------------------------- |
| [Getting Started](GettingStarted.md) | Install Lato, configure Ollama, and run your first session. |
| [CLI](CLI.md)                        | Command-line entry points: `lato`, `lato chat`, `lato run`, and resume. |

## Packages

| Name                      | Package     | Description                                                                                                     |
| ------------------------- | ----------- | --------------------------------------------------------------------------------------------------------------- |
| [Agent](Agent.md)         | `agent`     | Defines the `Agent` type and builds the final system prompt by combining the agent identity with loaded skills. |
| [Command](Command.md)     | `command`   | Implements slash command parsing and execution for interactive chat.                                            |
| [Config](Config.md)       | `config`    | Loads, validates, and manages Lato's global YAML configuration file.                                      |
| [Commands](Commands.md)   | `process`   | Runs programs inside the target workspace with timeouts and bounded output, reporting exit code and streams. |
| [Context](Context.md)     | `context`   | Builds a structured repository description for injection into model prompts on repository questions.       |
| [Editing](Editing.md)     | `edit`      | Safe, targeted file editing for the target workspace: exact-match replacements, creation, and diffs.        |
| [Effort](Effort.md)       | `effort`    | First-class effort ladder (low → medium → high → ultra → lato-X): provider-aware request configuration plus bounded agent orchestration. |
| [Index](Repository.md)    | `index`     | Indexes the workspace (files, directories, languages, Go symbols) and provides deterministic local search. |
| [Providers](Providers.md) | `providers` | Defines the model provider interface and its implementations for communicating with LLMs.                       |
| [Retrieval](Retrieval.md) | `retrieve`  | Retrieves bounded, evidence-backed source excerpts from the index so code questions are answered from real code. |
| [Runtime](Runtime.md)     | `runtime`   | Coordinates agents, providers, tools, skills, and sessions to execute model interactions.                       |
| [Session](Session.md)     | `session`   | Manages chat sessions, message history, persistence, and provider-specific message conversion.                  |
| [Skills](Skills.md)       | `skills`    | Discovers, indexes, and loads agent skills on demand from the local skill store.                                |
| [Tools](Tools.md)         | `tools`     | Defines the tool framework, including tool registration, execution, and built-in tool implementations.          |
| [TUI](TUI.md)             | `tui`       | Provides the interactive terminal interface built with Bubble Tea for chatting with Lato.                 |
| [Workspace](Workspace.md) | `workspace` | Discovers and describes the repository Lato is running inside, without AI or network access.       |

## Installation

To install Lato globally — so plain `lato` works from any terminal and any
project directory — use the installer script:

```bash
git clone https://github.com/fabledruns/forcefield
cd forcefield
./scripts/install.sh          # Linux/macOS: go install . → ~/go/bin/lato
```

```powershell
git clone https://github.com/fabledruns/forcefield
cd forcefield
.\scripts\install.ps1         # Windows PowerShell equivalent
```

Both scripts are user-local and idempotent: they never require sudo and
never edit shell configuration; if `PATH` needs updating, the exact line to
add is printed. Verify with `lato doctor`.

For a full first-run walkthrough, see [Getting Started](GettingStarted.md).

### Make Your Own Binary

Clone the repository using `git clone https://github.com/fabledruns/forcefield` in a new folder, and run `go build -o lato .`, then run using `./lato`.
Alternatively run `go install .` to place the binary in your Go bin directory.

### Releases Binary

Download the latest binary from the GitHub Releases page for your operating system.

## Architecture

Lato follows a modular architecture where each package has a single responsibility. The runtime coordinates the other packages to process a user request, execute tools, and return a response.

```text
             User
               │
               ▼
        CLI / Interactive TUI
               │
               ▼
            Runtime
     ┌─────────┼─────────┐
     │         │         │
     ▼         ▼         ▼
   Agent    Session    Config
     │
     ▼
   Provider
     │
     ▼
 Local LLM (Ollama)
     │
     ▼
 Tool Calls
     │
     ▼
    Tools
     │
     ▼
  Skill Loading
     │
     ▼
 Final Response
```

### Request Flow

1. The user submits a prompt through the CLI or interactive TUI.
2. The runtime loads the configuration and restores the current session.
3. The runtime builds the agent and its initial system prompt.
4. The selected model provider sends the request to the local language model.
5. If the model requests a tool, the runtime executes it and returns the result to the model.
6. If the model requests a skill, the skill is loaded from disk and provided to the model.
7. The model produces its final response.
8. The runtime saves the updated session and returns the response to the user.
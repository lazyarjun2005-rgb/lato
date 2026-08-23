# Command

Package: `internal/command`

The `command` package implements the slash command system for interactive chat. It parses input, registers commands, looks up commands, and runs them. The package does not import Bubble Tea.

## Purpose

Slash commands control the chat session. Examples:

- `/help` — list available commands
- `/clear` — clear the chat transcript
- `/exit` — end the session
- `/model` — show or switch the active model
- `/provider` — show or switch the active provider
- `/effort` — show or change the agent effort level (M16)
- `/sessions` — open the session picker
- `/workspace` — describe the current repository
- `/index` — show the repository index summary

## Slash Command Palette

Typing `/` in the input opens an autocomplete palette derived from the
same registry that powers `/help`. Filtering is case-insensitive and
prefix-based (`/mo` → `/model`, `/co` → `/connect`, `/copy`).

Keys while the palette is engaged:

| Key | Action |
| --- | ------ |
| `↑` / `↓` | move selection |
| `Tab` | complete the selected command into the input |
| `Enter` | complete and execute through the normal dispatcher |
| `Esc` | dismiss the palette, keep typed text |
| Backspace | continue filtering |

Once a space is typed, argument entry begins and the palette steps
aside. The palette never executes anything itself — acceptance fills
the input with the canonical command line, which flows through the
standard dispatcher.

## Core Interfaces

### `Command`

Each slash command implements this interface:

| Method        | Description                                      |
| ------------- | ------------------------------------------------ |
| `Name()`      | Primary command name, for example `help`.        |
| `Aliases()`   | Optional alternate names, for example `?`.       |
| `Description()` | Short text for `/help`.                        |
| `Usage()`     | Usage string, for example `/model [name]`.       |
| `Execute()`   | Runs the command with a context and arguments.   |

### `Context`

Commands act on the session through a small interface. The TUI is the production implementation. Tests can supply a fake.

| Method               | Description                                      |
| -------------------- | ------------------------------------------------ |
| `Println`            | Write a message to the user.                     |
| `Clear`              | Clear the visible transcript.                    |
| `Quit`               | End the interactive session.                     |
| `Model` / `SetModel` | Read or change the active model.                 |
| `Provider` / `SetProvider` | Read or change the active provider.        |
| `CurrentEffort` / `SetEffort` | Read or change the effort level.        |
| `OpenSessionPicker`  | Open the session selection UI.                   |
| `Workspace`          | Read the workspace description.                  |
| `Index`              | Read the cached repository index.                |
| `SkillsSummary`      | Render the discovered skill catalog.             |
| `TaskList` / `ResumeTask` / `AbandonTask` | Persistent task operations.|
| `MemorySummary` and memory mutation methods | Project memory access.    |
| `PermissionsSummary` / `ResetPermissions`  | Permission policy access.  |
| `SubmitPrompt`       | Submit a prompt as a user turn into the existing agent loop. |

## Main Parts

| Part       | File           | Description                                              |
| ---------- | -------------- | -------------------------------------------------------- |
| Parser     | `parser.go`    | Detects slash commands and splits name and arguments.    |
| Registry   | `registry.go`  | Stores commands by name and alias.                       |
| Dispatch   | `dispatch.go`  | Looks up a command and runs it.                          |
| Suggest    | `suggest.go`   | Suggests similar names for unknown commands.             |
| Builtin    | `builtin/`     | Built-in command implementations.                        |

## Parse and Dispatch Flow

1. The user submits a line in the TUI.
2. `Parse` checks for a leading `/`.
3. If the line is not a command, the TUI treats it as a chat message.
4. If the line is a command, `Dispatch` looks up the name in the registry.
5. If the name is unknown, dispatch returns an error with suggestions.
6. If the name is known, the command runs against the context.

## Development Commands

The development family (`search`, `explain`, `debug`, `fix`, `test`,
`build`, `run`, `review`, `refactor`, `code`) is data-driven: one
`devCommand` table in `internal/command/builtin/devprompt.go` defines
usage, description, and the agent directive. Executing one renders its
arguments into a prompt and submits it through `Context.SubmitPrompt`,
which records it as a genuine user turn and streams the answer through
the ONE existing agent loop. These commands therefore inherit the
current model/provider, the tool system, the permission gate, bounded
turns, automatic tool-failure recovery, and honest completion. They
never call a model themselves; adding or removing one is a single
table entry.

| Command    | Usage              | Required args |
| ---------- | ------------------ | ------------- |
| `search`   | `/search <topic>`  | yes           |
| `explain`  | `/explain <target>`| yes           |
| `debug`    | `/debug <symptom>` | yes           |
| `fix`      | `/fix <problem>`   | yes           |
| `test`     | `/test [target]`   | no            |
| `build`    | `/build [target]`  | no            |
| `run`      | `/run [what]`      | no            |
| `review`   | `/review [target]` | no            |
| `refactor` | `/refactor <goal>` | yes           |
| `code`     | `/code <task>`     | yes           |

## Built-in Commands

| Command      | Aliases | Usage              | Action                                      |
| ------------ | ------- | ------------------ | ------------------------------------------- |
| `help`       | `?`, `commands` | `/help`     | List registered commands.                   |
| `clear`      | —       | `/clear`           | Clear conversation history and transcript.  |
| `exit`       | `quit`  | `/exit`            | End the chat session.                       |
| `model`      | —       | `/model [name]`    | Show or switch the active model.            |
| `provider`   | —       | `/provider [name]` | Show or switch the active provider.         |
| `effort`     | —       | `/effort [level]`  | Show or change the effort level.            |
| `fast`       | —       | `/fast`            | Session-only switch to low effort.          |
| `sessions`   | `s`     | `/sessions`        | Open the saved session picker.              |
| `rename`     | —       | `/rename <title>`  | Rename the current session persistently.    |
| `resume`     | —       | `/resume [<id\|title>]` | Resume a session by ID/prefix or exact title; bare opens the picker. |
| `workspace`  | —       | `/workspace`       | Describe the current repository.            |
| `index`      | —       | `/index`           | Show the repository index summary.          |
| `status`     | —       | `/status`          | Summarize project and agent setup.          |
| `version`    | —       | `/version`         | Show the running Lato version.              |
| `doctor`     | —       | `/doctor`          | Environment check inside the chat.          |
| `skills`     | —       | `/skills`          | List skills the agent can load.             |
| `copy`       | —       | `/copy [target]`   | Copy the last response or transcript.       |
| `export`     | —       | `/export [path]`   | Write the conversation to a Markdown file.  |
| `connect`    | —       | `/connect [import]`| Connect a provider interactively.           |
| `import`     | —       | `/import`          | Import provider config from other tools.    |
| `memory`     | —       | `/memory [sub]`    | Inspect or manage project memory.           |
| `task`       | —       | `/task [sub]`      | Show, resume, or abandon tasks.             |
| `permissions`| —       | `/permissions [reset]` | Show permission policy / reset grants.  |

The version string is shared with `lato --version` through
`internal/version`; release builds override it at link time:

    go build -ldflags "-X lato/internal/version.Version=1.2.3" .

## How to Add a Command

1. Create a type that implements `Command`.
2. Register an instance at TUI startup.
3. Do not change the parser, registry, or dispatch logic.

## Design Notes

- The package is independent of the TUI and the runtime.
- Parsing, lookup, suggestion, and execution are separate and testable.
- Built-in commands live in `command/builtin` so core dispatch stays small.

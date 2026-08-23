# TUI

Package: `internal/tui`

The `tui` package provides the interactive terminal interface for Lato. It is built with Bubble Tea.

## Purpose

The TUI is a presentation layer. It shows the conversation, accepts user input, runs slash commands, and streams runtime events into the transcript. It does not own agent logic, provider logic, or tool logic.

## Start Path

```go
func Start(sess *session.Session) error
```

1. Load configuration for header labels.
2. Create a Bubble Tea program with the chat model.
3. Enable the alternate screen and mouse cell motion.
4. Run until the user exits.

The CLI starts the TUI from:

- `lato` with no subcommand
- `lato chat`
- `lato --resume <session-id>`

## Main UI Parts

| Part              | Role                                                      |
| ----------------- | --------------------------------------------------------- |
| Header            | Shows agent, provider, model, and effort labels.          |
| Transcript        | Scrollable conversation history.                          |
| Input box         | Text input for prompts and slash commands.                |
| Slash palette     | Autocomplete strip above the input while a `/` command is being typed (M16). |
| Spinner / status  | Shows that a model run is in progress.                    |
| Session picker    | Modal list of saved sessions from `/sessions`.            |
| Provider/model pickers | Modal lists for `/provider` and `/model` (grouped per provider). In `/model`, `←`/`→` select the effort level and `s` applies session-only. |
| Connect wizard    | Masked-input `/connect` flow for adding providers.        |
| Permission prompt | Compact modal when a tool call needs explicit approval (M13): `[1] Allow once · [2] Allow for task · [3] Deny`; `esc`/`n` deny. |
| Banner / styles   | Visual presentation helpers.                              |

## Permission Prompts

When the runtime's permission gate requires confirmation (M13), the
prompt takes over the screen like other modals. Keys `1`/`y` allow
once, `2`/`t` allow for the current task, and `3`/`n`/`esc` deny.
The request, the decision, and any refusal are appended to the
transcript as plain-text activity lines, so `/copy last` and
`/copy transcript` capture them like any other output. Prompt text is
secret-redacted before display.

## Copying Output

`/copy` places Lato's output on the system clipboard as plain text
(no terminal styling or ANSI escapes — markdown and code blocks stay
readable as text):

| Command            | What is copied                                            |
| ------------------ | --------------------------------------------------------- |
| `/copy`            | The most recent complete response, including its tool activity lines. |
| `/copy response`   | Same as `/copy`.                                           |
| `/copy last`       | Same as `/copy`.                                           |
| `/copy transcript` | The whole visible conversation, labeled per speaker.       |

Keyboard shortcuts:

- `Alt+C` copies the latest response (works in every terminal).
- `Ctrl+Shift+C` also copies when the terminal reports it as a distinct
  key; many terminal emulators intercept it themselves. On legacy
  terminals that fold it into `Ctrl+C`, use `Alt+C` or `/copy` instead.

Linux clipboard requirements: one of `wl-copy` (Wayland), `xclip`, or
`xsel` must be installed. macOS uses `pbcopy`; Windows uses `clip.exe`.
If no mechanism is available, Lato shows an error naming what to
install — copied content never appears in error messages.

## Input Handling

When the user submits a line:

1. The TUI first tries slash-command dispatch.
2. If the line is a command, the command runs through the command context.
3. If the line is not a command, the TUI treats it as a chat prompt.
4. The prompt is added to the session and sent through the runtime stream.
5. Streamed text, tool activity, and the final response update the transcript.
6. The session is saved locally.

## Command Integration

The TUI implements `command.Context`.

Through that interface, commands can:

- print status messages
- clear the transcript
- quit the program
- read or switch model and provider
- open the session picker

Built-in commands are registered when the chat model is created.

## Streaming

The TUI consumes `runtime.Event` values:

| Event             | TUI behavior                                      |
| ----------------- | ------------------------------------------------- |
| `EventThinking`   | Show waiting or thinking status.                  |
| `EventText`       | Append assistant text to the live buffer.         |
| `EventToolStart`  | Show that a tool started.                         |
| `EventToolFinish` | Show tool completion or tool error detail.        |
| `EventDone`       | Finalize the assistant message and save session.  |
| `EventError`      | Show the error and stop waiting.                  |

## Session Picker

The `/sessions` command loads saved sessions and opens a picker modal.

- Sessions are sorted by last update time.
- Each entry shows a short title from the first user message.
- Selecting a session switches the active conversation in the TUI.

## Design Notes

- The TUI is thin. Business logic stays in runtime, command, session, and tools.
- Markdown rendering improves readability of assistant replies.
- The interface targets a local terminal workflow, not a web UI.

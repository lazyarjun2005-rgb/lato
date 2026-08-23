# CLI

Package: `cmd`

The CLI is the user entry point for Lato. The binary name is `lato`.

## Purpose

The CLI starts interactive chat, resumes a session, or runs a one-shot prompt. It uses the Cobra command framework.

## Commands

### `lato`

Starts the interactive terminal interface with a new session.

```bash
lato
```

### `lato --resume <session-id>`

Starts the interactive terminal interface with an existing session.

```bash
lato --resume a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

### `lato chat`

Starts the same interactive chat as bare `lato`.

```bash
lato chat
```

### `lato run [task]`

Runs one prompt through the runtime and prints the final response.

```bash
lato run explain this repository
```

The command joins all task arguments into one prompt string.

### `lato doctor`

Checks the installation and environment: where the running binary lives,
whether its directory is on `PATH` (so plain `lato` works from any
terminal), where configuration, provider connections, project memory, and
tasks are stored, which workspace you are in, and which clipboard helper is
available. It changes nothing.

## Request Paths

### Interactive path

```text
lato / lato chat
    │
    ▼
session.New or session.Load
    │
    ▼
tui.Start
    │
    ▼
runtime.StreamChat
```

### One-shot path

```text
lato run [task]
    │
    ▼
runtime.Run
    │
    ▼
print response content
```

## Design Notes

- Bare `lato` opens chat so the common path needs no subcommand.
- `lato run` is useful for scripts and quick checks.
- Session resume is a root flag, not a separate subcommand, in the current prototype.

# Session

Package: `internal/session`

The `session` package manages chat sessions, message history, local persistence, and conversion of session messages into provider message format.

## Purpose

A session is one conversation. Lato stores sessions as local JSON files so you can list them later and continue a previous chat.

## Types

### `Message`

| Field     | Type        | Description                          |
| --------- | ----------- | ------------------------------------ |
| `Role`    | `string`    | Message role, for example `user`.    |
| `Content` | `string`    | Message text.                        |
| `Time`    | `time.Time` | Time when the message was added.     |

### `Session`

| Field       | Type        | Description                              |
| ----------- | ----------- | ---------------------------------------- |
| `ID`        | `string`    | Unique session ID.                       |
| `Title`     | `string`    | Optional human-readable title (M3.2).    |
| `CreatedAt` | `time.Time` | Session creation time.                   |
| `UpdatedAt` | `time.Time` | Last update time.                        |
| `Messages`  | `[]Message` | Ordered message history.                 |

Older session files without `title` load normally with an empty
title; set one with `/rename <title>` inside Lato.

`/resume <target>` resolves a session by exact ID, unique ID prefix
(e.g. the 8-character short ID shown by /sessions), or exact Title —
in that order. Ambiguous prefixes or duplicate titles are refused with
the candidates listed; resolution never renames or modifies sessions.

`/rewind [N]` removes the most recent N conversation turns from the
current session and persists the result. A turn is one user request
plus its assistant response when one exists, so an unanswered final
request is removed by itself. It is a conversation-history operation
only: files on disk, memory, tasks, and configuration are untouched.

## Storage Location

Sessions are stored under the current working directory:

```text
.lato/sessions/<session-id>.json
```

Example:

```text
.lato/
└── sessions/
    ├── a1b2c3d4-....json
    └── e5f6g7h8-....json
```

## Functions

### `New`

```go
func New() *Session
```

Creates a new session with a unique ID, current timestamps, and an empty message list.

### `AddMessage`

```go
func (s *Session) AddMessage(role, content string)
```

Appends a message and updates `UpdatedAt`.

### `Save`

```go
func (s *Session) Save() error
```

Writes the session to disk as indented JSON. The function creates the sessions directory if needed and updates `UpdatedAt`.

### `Load`

```go
func Load(id string) (*Session, error)
```

Reads one session file by ID.

### `List`

```go
func List() ([]Session, error)
```

Reads all session JSON files from the sessions directory. If the directory does not exist, the function returns an empty list.

### `ProviderMessages`

```go
func (s *Session) ProviderMessages() []providers.Message
```

Converts session messages into provider messages for a model call. This conversion keeps only role and content for the provider path used by chat history.

## How the CLI Uses Sessions

| Command / flag           | Behavior                                      |
| ------------------------ | --------------------------------------------- |
| `lato` or `lato chat`    | Starts a new session.                         |
| `lato --resume <id>`     | Loads an existing session by ID.              |
| `/sessions` in the TUI   | Lists saved sessions and opens a picker.      |

## Design Notes

- Session data stays on the local machine.
- Each session file is independent JSON.
- The package does not talk to the model. It only stores and converts history.

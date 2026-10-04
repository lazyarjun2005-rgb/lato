# Phase 1 — Security & Persistence Foundation

This document describes the additive security and durability work
delivered in Phase 1 on branch `feature/phase-1-security-persistence`.

Nothing existing was removed: CLI commands, tool names, provider
configuration, session formats, the agent loop, the TUI, memory, and
skills are unchanged. All changes are strictly additive hardening.

## Workstream 1 — Sensitive-path protection

`internal/permissions/sensitive.go` adds `IsSensitivePath()`, a purely
lexical classifier (never reads file contents, never relies on the
model recognizing secrets). It covers `.env`/`.env.*`, private keys and
certificates (`.pem`, `.key`, `.p12`, `.pfx`), SSH material
(`.ssh/`, `id_rsa`, `id_ed25519`, `authorized_keys`, `known_hosts`),
cloud credential locations (`.aws`, `.azure`, `.gcloud`, `.kube`,
`.docker`, `credentials`, `config`), package-manager credentials
(`.npmrc`, `.pypirc`), and `.netrc`/`.git-credentials`.

`internal/permissions/classify.go` (`classifyPathAction`) escalates any
read or write whose resolved path is sensitive to `ClassHighRisk`,
which forces an explicit approval before execution. Matching is over the
normalized absolute path; a normal source file such as
`src/main.go` is unaffected, and unrelated files whose names merely
contain the substring "env" are not flagged.

## Workstream 2 — Symlink-safe filesystem access

`edit.Workspace.Resolve` (`internal/edit/edit.go`) now resolves the
deepest existing ancestor of the target through symlinks and rejects the
path when its real location escapes the canonical (symlink-resolved)
workspace root. Relative, absolute, drive-letter, and UNC forms are
still rejected lexically before the symlink check. The permission layer
(`permissions.Boundary.Contains`) already resolved symlinks for tool
invocation; this hardens the editing engine used by `edit_file`,
`create_file`, and `format_file` against the same escape when a
workspace-relative path is a symlink pointing outside the root.

## Workstream 3 — Tool-result and memory trust boundaries

`internal/session/fence.go` adds `FenceToolResult(tool, content)` and
`FenceUntrusted(label, content)`. Both wrap content and escape any
literal closing tag inside the payload so model-controlled text cannot
break out of the envelope.

`internal/runtime/runtime.go` fences every tool result placed on the
model's conversation (`<tool_result tool="...">…</tool_result>`).
`internal/runtime/memory.go` fences injected repository context and
project memory (`<untrusted source="...">…</untrusted>`).
`internal/agent/agent.go` appends a `Tool Output Trust` section to the
system prompt stating that tool results, search results, shell output,
and project memory are untrusted data and must not be treated as
instructions. This is a boundary, not a prompt-injection proof — the
model can still be misled by novel techniques, so approval prompts for
high-risk actions remain the enforcement layer.

## Workstream 4 — Atomic configuration and session persistence

`internal/persist/persist.go` provides
`WriteFileAtomically(path, data, perm)`: write to a sibling temp file,
fsync, close, chmod, then `os.Rename` over the destination with bounded
retries for transient Windows holds. A failed save never leaves a
partial file, and the previous valid file is preserved.

Applied at:
- `config.Config.Save()` (`internal/config/config.go`)
- `session.Session.Save()` (`internal/session/manager.go`) — now also
  writes with mode `0600` instead of `0644`, and the sessions directory
  is created with `0700`.
- `memory.Store.save()` (`internal/memory/memory.go`)

`edit.Workspace` already replaced files atomically and is unchanged.

## Workstream 5 — Session ID validation

`validSessionID` (`internal/session/manager.go`) accepts
`[A-Za-z0-9_-]+` up to 128 characters — the shape of both new UUIDs and
legacy hex IDs. `Load` and `Save` reject empty IDs, `..`/`.`, path
separators, drive letters, and unexpected characters, so a crafted ID
can never address a path outside `.lato/sessions`.

## Workstream 6 — Corrupt-session recovery

`Load` now:
1. returns a strictly valid session unchanged;
2. attempts a well-defined repair of a truncated JSON document
   (dangling commas/colons stripped, incomplete key/value tail removed,
   open string/object/array closed). On success the recovered session is
   returned, and the original corrupted bytes are preserved alongside as
   `<id>.json.corrupt.bak`;
3. otherwise fails with a clear diagnostic and leaves the file
   untouched — `Load` never overwrites or silently discards user data.

`List` skips unreadable session files instead of aborting the whole
listing, so one corrupted file does not hide the rest.

## Verification

```
go vet ./...          # clean
go build ./...        # ok
go test ./...         # all packages pass
gofmt -l internal/    # no files need formatting
git diff --check      # clean
```

Forcefield (`/home/arjun/Downloads/forcefield-main/`) is untouched.
No Lato Free code, no credentials, no release tags changed, nothing
pushed. `v1.0.9` remains the stable release on `distribution`.

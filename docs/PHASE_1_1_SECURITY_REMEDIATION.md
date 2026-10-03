# Phase 1.1 — Security Gap Remediation

This document records the closure of the two security blockers found in
the Phase 1 post-implementation audit.

## Original audit findings

1. **HIGH — `run_command` bypass.** `classifyCommand`'s routine
   allowlist included `cat`, `head`, `tail`, `grep`, `cp`, etc. The
   command classifier never checked for sensitive targets, so
   `cat .env`, `cp .aws/credentials /tmp`, or `cat ~/.ssh/id_rsa` could
   be auto-allowed.
2. **MEDIUM — repository-tool bypass.** `read_repo_file` and
   `search_repo` fell into the generic read-only branch and were
   auto-allowed; an indexed sensitive file could be read (or surfaced in
   search results) without approval.

## Root causes

- `classifyRunCommand`/`classifyCommand` trusted the safe-program
  table without first inspecting path arguments.
- `pathArg` in `internal/permissions/classify.go` did not include
  `read_repo_file`, and `search_repo` had no path classification at all.
- Search results were returned exactly as the store produced them, with
  no sensitive-path filtering.

## Changes

### `internal/permissions/command.go`

- Added `sensitiveCommandTarget(program, args)`: scans every argument
  token (including the value of `--flag=value` forms) through
  `IsSensitivePath`. It is conservative and fails toward Ask, but is
  **not** a full shell parser.
- `classifyCommand` now runs that check immediately before the routine
  allowlist, returning `ClassHighRisk`/`Ask` (`"command targets a
  sensitive file"`) for any sensitive token. Redirections, pipes, and
  substitutions were already classified as shell features → Ask.

### `internal/permissions/classify.go`

- Added `"read_repo_file": "path"` to `pathArg`, so its path argument
  flows through `classifyPathAction` (same sensitive-path escalation as
  `read_file`).
- `classifyRunCommand` now escalates a `dir` argument that is itself a
  sensitive directory (e.g. `dir: ".aws"` or `dir: ".ssh"`) to
  `ClassHighRisk`/`Ask`.

### `internal/tools/repository/repository.go`

- `SearchRepository.Execute` now filters index matches whose path
  `IsSensitivePath` flags before the result is returned; `Count` is
  recomputed accordingly. Sensitive matches never reach the model.
- `read_repo_file` derives its classification from the runtime
  permission gate (`classifyPathAction`), which now escalates sensitive
  paths, so a direct reader cannot obtain sensitive indexed content
  without explicit approval.

## Regression tests added

- `internal/permissions/command_test.go`:
  `TestSensitiveCommandTargetsRequireApproval` (`cat .env`, `head .env.local`,
  `tail .env`, `grep TOKEN .env`, `cp .aws/credentials ...`, `cat ~/.ssh/id_rsa`,
  `ls .docker/config.json`, `cat certs/server.key`, `cat id_ed25519`, `head .kube/config`
  all → `ClassHighRisk`/`Ask`); `TestSafeCommandsWithNormalPathsStillAllowed`
  (`cat README.md`, `cat environment.txt` (substring must NOT trigger), etc.
  remain `Allow`).
- `internal/permissions/sensitive_test.go`:
  `TestReadRepoFileSensitivePathEscalates` (`read_repo_file .env` → Ask),
  `TestSensitiveDirEscalatesForRunCommand` (`dir=.aws` → Ask).
- `internal/tools/repository/repository_test.go`:
  `TestSearchRepositoryFiltersSensitiveMatches` (`.env`/`.aws/credentials`
  matches removed, normal matches retained).

## Validation results

```
gofmt -l ./internal ./cmd   # no files
go vet ./...                 # clean
go build ./...               # ok
go test ./...                # all packages pass
git diff --check             # clean
```

## Remaining limitations

- `sensitiveCommandTarget` is **not** a shell parser and is not a
  sandbox. It covers the common one-shot forms; anything with pipes,
  redirections, or substitutions falls back to the existing
  shell-feature → Ask branch, and ambiguous forms default to Ask. A
  novel indirection (e.g. embedding the path in expansion or a variable)
  is not detected; the durable enforcement for non-obvious cases remains
  the approval prompt.
- Sensitive-path filtering in `search_repo` is based on the match path;
  a verbatim `.env` name is masked, but content that resembles a credential
  inside a non-sensitive file is out of scope here.
- TOCTOU and the edit-engine resolution are unchanged from Phase 1
  (not race-free); no new symlink or race claim is made.
- This remediation is **not** a secure shell sandbox; it raises the
  approval bar for the common sensitive-access patterns.

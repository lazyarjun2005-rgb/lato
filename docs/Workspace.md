# Workspace

Package: `internal/workspace`

The `workspace` package discovers and describes the repository Lato is running inside. It is pure filesystem inspection — no AI, no network, no external commands. The runtime captures the result once at startup, so later milestones (context building, planning, indexing) can read a workspace description without re-scanning the disk.

## Purpose

Before the agent asks the LLM anything, it should understand the workspace it is running inside. Lato starts in a folder, detects the project around it, and records:

- Repository name and project root
- Current working directory and operating system
- Whether it is a git repository and the current branch
- Primary programming language
- Detected build system and package manager
- Important root files
- A bounded directory tree

This information is captured once at startup and stored on the `Runtime`, so later features never re-scan the disk.

## Core Types

### `Info`

A complete description of the workspace.

| Field           | Description                                       |
| --------------- | ------------------------------------------------- |
| `Repository`    | Name from the git remote, falling back to the module/package name, then the root directory name. |
| `Root`          | Absolute path of the detected project root.       |
| `CWD`           | Absolute path Lato was started from.             |
| `OS`            | Friendly operating-system name (Windows/macOS/Linux). |
| `IsGitRepo`     | Whether the root is a git repository.             |
| `Branch`        | Current git branch, empty if detached or not a repo. |
| `Language`      | Primary language, empty if undetected.            |
| `Framework`     | Detected framework (e.g. Next.js, Django, Vite).  |
| `Module`        | Module/package identifier from the manifest.      |
| `BuildSystem`   | e.g. Go modules, Cargo, Maven, PEP 517.           |
| `PackageManager`| e.g. Go modules, Cargo, npm, pnpm, pip.           |
| `ImportantFiles`| Present root files from the well-known list.      |
| `Tree`          | Bounded directory tree below the root.            |

### `Node`

One entry in the `Tree`.

| Field  | Description                               |
| ------ | ----------------------------------------- |
| `Name` | Directory or file name.                   |
| `Path` | Path relative to the root, using `/` separators. |
| `IsDir`| True for directories.                     |

## Entry Points

| Function       | Description                                                |
| -------------- | ---------------------------------------------------------- |
| `Discover()`   | Discovers from the current working directory.             |
| `DiscoverDir(dir)` | Discovers from a specific directory, walking upward to find a project root. |
| `(Info).Summary()` | Renders a compact multi-line description for the `/workspace` command. |

## Detection Logic

Language detection is purely file-based and deterministic. It checks root manifests first, then falls back to the most common source-file extension counted during the directory walk.

Ignored paths do not participate. The walk skips version-control metadata, dependency and build directories, and everything the root `.gitignore` excludes, so a gitignored virtualenv or generated bundle can never outvote the project's own sources. (See [Repository.md](Repository.md) for the exact rule set, which discovery and the index share.)

| Language       | Primary signals                                      |
| -------------- | ---------------------------------------------------- |
| Go             | `go.mod`, `go.work`, or `.go` files                 |
| Python         | `pyproject.toml`, `requirements.txt`, `setup.py`, `.py` |
| Rust           | `Cargo.toml`, `.rs`                                 |
| JavaScript     | `package.json` without `tsconfig.json`, `.js`       |
| TypeScript     | `package.json` with `tsconfig.json`, `.ts`          |
| Java           | `pom.xml`, `build.gradle`, `.java`               |
| C#             | `.csproj`, `.sln`, `.cs`                          |

Frameworks, build systems, and package managers are inferred from the same root files plus manifest contents (e.g. a `next.config.js` implies Next.js; a `yarn.lock` implies Yarn).

No AI and no network are involved. The only input is the filesystem.

## Important Files

If these exist at the root they are recorded in `ImportantFiles`; missing files are simply ignored and never cause an error:

- Manifests and build entry points — `go.mod`, `go.work`, `package.json`, `Cargo.toml`, `pyproject.toml`, `requirements.txt`, `Makefile`, `Justfile`, `Dockerfile`, `docker-compose.yml`, `compose.yaml`
- Documentation — `README.md`, `README`, `CONTRIBUTING.md`, `CHANGELOG.md`, `LICENSE`, `LICENSE.md`, `LICENSE.txt`, `COPYING`
- Agent instructions — `CLAUDE.md`, `AGENTS.md`, `.cursorrules`
- Editor, linter, and test conventions — `.editorconfig`, `.golangci.yml`, `.golangci.yaml`, `tsconfig.json`, `setup.cfg`, `pytest.ini`

Agent-instruction files are included deliberately: they tell the model how the project expects to be worked on.

## Errors

Discovery never fails and never produces errors. Unreadable directories, missing files, and foreign filesystems are all handled by reporting an empty or partial description.

An unreadable directory is skipped rather than descended into, and the count of such entries is reported as `Info.Unreadable`. `/workspace` shows an `Unreadable` row only when that count is non-zero, so a fully readable workspace prints exactly as before. A non-zero value means the description is partial.

## Design Notes

- Pure `os`/`path/filepath` inspection; no AI, no LLM, no network.
- The initial root search climbs at most 8 levels.
- The directory tree is capped at depth 3 and 200 entries.
- Ignore rules live in one place (`Ignore` in this package) and are shared with the repository index, so discovery and the index can never disagree about which paths belong to the project. This package holds them because the index already depends on it; inverting that would create an import cycle.
- Discovery is kept separate from `agent`, `runtime`, `provider`, and `tools` so later milestones can reuse it.
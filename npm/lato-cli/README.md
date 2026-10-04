# lato-cli

Lato is a local-first, terminal-based AI coding agent. This npm package
installs the official prebuilt `lato` binary for your platform — no Go
toolchain required.

- Project: https://github.com/lazyarjun2005-rgb/lato
- Releases: https://github.com/lazyarjun2005-rgb/lato/releases

## Install

```bash
npm install -g lato-cli
```

The postinstall script downloads the native Lato binary for your
operating system and architecture (Linux/macOS amd64/arm64, Windows
amd64/arm64) from the matching GitHub release, so the installed version
always corresponds to this package's version.

## Usage

```bash
lato          # interactive session in the current directory
lato run "summarize the README"
lato doctor   # verify the installation and environment
lato --version
```

## Requirements

- Node.js >= 18 (only for the install step; the `lato` command itself is
  a native binary and runs without Node)

Lato talks to model providers such as Ollama (fully local), LM Studio,
NVIDIA NIM, and OpenRouter. Hosted providers use your own API key
(Bring Your Own Key); nothing is bundled.

## Alternatives

- Linux/macOS: `curl -fsSL https://raw.githubusercontent.com/lazyarjun2005-rgb/lato/v1.2.0/scripts/install.sh | sh`
- Windows PowerShell: `irm https://raw.githubusercontent.com/lazyarjun2005-rgb/lato/v1.2.0/scripts/install.ps1 | iex`

## License

Apache-2.0. See [LICENSE](LICENSE).

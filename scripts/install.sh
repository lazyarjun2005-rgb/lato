#!/usr/bin/env sh
# install.sh — user-local global installation of Lato (Linux/macOS).
#
# Installs the `lato` executable so it can be started from any terminal
# and any project directory with plain `lato` — no absolute paths.
#
# Default behavior mirrors standard Go tooling: `go install .` places
# the binary in $GOBIN, or $GOPATH/bin, or ~/go/bin. Set PREFIX to
# target another user-local directory instead (no sudo required):
#
#   ./scripts/install.sh                    # go install . → ~/go/bin/lato
#   PREFIX="$HOME/.local/bin" ./scripts/install.sh
#
# The script is idempotent: re-running it simply refreshes the binary.
# It never uses sudo and never edits your shell configuration; if PATH
# needs updating it prints the exact line for you to add yourself.

set -eu

if ! command -v go >/dev/null 2>&1; then
    echo "error: Go is not installed or not on PATH." >&2
    echo "Install Go from https://go.dev/dl/ and re-run this script." >&2
    exit 1
fi

repo="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo"

if [ -n "${PREFIX:-}" ]; then
    # User-chosen location: build directly into PREFIX.
    mkdir -p "$PREFIX"
    echo "Building lato into $PREFIX ..."
    go build -o "$PREFIX/lato" .
    bin_dir="$(CDPATH= cd -- "$PREFIX" && pwd)"
else
    # Standard Go behavior: go install resolves GOBIN/GOPATH itself.
    echo "Installing lato via go install . ..."
    go install .
    bin_dir="$(go env GOBIN)"
    if [ -z "$bin_dir" ]; then
        gopath="$(go env GOPATH | cut -d':' -f1)"
        bin_dir="$gopath/bin"
    fi
fi

echo "Installed: $bin_dir/lato"

case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *)
        echo ""
        echo "NOTE: $bin_dir is not on your PATH."
        echo "To make \`lato\` available in every terminal, add this line to"
        echo "your shell configuration (~/.bashrc, ~/.zshrc, or equivalent):"
        echo ""
        echo "  export PATH=\"\$PATH:$bin_dir\""
        echo ""
        echo "Then restart your shell (or run: source ~/.bashrc)."
        echo "This script deliberately does not edit your shell files."
        ;;
esac

echo ""
echo "Verify with:"
echo "  which lato     # should print $bin_dir/lato"
echo "  lato doctor    # environment check"
echo ""
echo "Then, from any project directory:"
echo "  cd ~/some-project && lato"

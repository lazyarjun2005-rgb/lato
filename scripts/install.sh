#!/bin/sh
# install.sh — prebuilt binary installer for Lato (Linux/macOS).
#
# Downloads the official native Lato binary for this platform from the
# pinned GitHub release, verifies its SHA-256 checksum, and installs it
# to ~/.local/bin/lato. No Go toolchain, no compilation, no sudo.
#
# Usage:
#   curl -fsSL <installer URL> | sh
#   sh scripts/install.sh
#
# Override the install location with LATO_INSTALL_DIR.
# The script never executes the downloaded binary during installation
# and never edits your shell configuration files.

set -eu

VERSION="v1.2.1"
REPO="lazyarjun2005-rgb/lato"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
CHECKSUMS_URL="${BASE_URL}/checksums.txt"

INSTALL_DIR="${LATO_INSTALL_DIR:-$HOME/.local/bin}"

err() {
    echo "error: $*" >&2
    exit 1
}

log() {
    echo "$@"
}

# --- Platform detection -------------------------------------------------

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Linux)  PLATFORM="linux" ;;
    Darwin) PLATFORM="darwin" ;;
    *) err "unsupported operating system: ${OS}. Use npm or a manual download from https://github.com/${REPO}/releases" ;;
esac

case "$ARCH" in
    x86_64|amd64)    ARCH_NAME="amd64" ;;
    aarch64|arm64)   ARCH_NAME="arm64" ;;
    *) err "unsupported architecture: ${ARCH}" ;;
esac

ASSET="lato-${PLATFORM}-${ARCH_NAME}"
ASSET_URL="${BASE_URL}/${ASSET}"

log "Installing Lato ${VERSION} (${PLATFORM}-${ARCH_NAME})..."

# --- Check for an existing Lato installation ---------------------------

TARGET="${INSTALL_DIR}/lato"

if [ -e "$TARGET" ] && [ ! -x "$TARGET" ]; then
    err "${TARGET} exists but is not executable; remove it and re-run the installer"
fi

if [ -e "$TARGET" ]; then
    log "Replacing existing Lato installation at ${TARGET}"
fi

# --- Download ------------------------------------------------------------

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t lato-install)"
trap 'rm -rf "$TMP_DIR"' EXIT

log "Downloading ${ASSET_URL}"

if ! curl -fsSL --retry 3 -o "$TMP_DIR/$ASSET" "$ASSET_URL"; then
    err "could not download ${ASSET_URL}

The release may not exist for this platform. Check:
  https://github.com/${REPO}/releases/tag/${VERSION}"
fi

# --- Checksum verification ----------------------------------------------

log "Verifying SHA-256 checksum..."

if ! curl -fsSL --retry 3 -o "$TMP_DIR/checksums.txt" "$CHECKSUMS_URL"; then
    err "could not download ${CHECKSUMS_URL}; cannot verify the binary — aborting"
fi

EXPECTED="$(grep " ${ASSET}\$" "$TMP_DIR/checksums.txt" | awk '{print $1}')"

if [ -z "$EXPECTED" ]; then
    err "no checksum found for ${ASSET} in checksums.txt"
fi

ACTUAL="$(sha256sum "$TMP_DIR/$ASSET" 2>/dev/null | awk '{print $1}')"

if [ -z "$ACTUAL" ]; then
    # macOS ships shasum instead of sha256sum
    ACTUAL="$(shasum -a 256 "$TMP_DIR/$ASSET" 2>/dev/null | awk '{print $1}')"
fi

if [ -z "$ACTUAL" ]; then
    err "no SHA-256 tool available (need sha256sum or shasum); cannot verify the binary"
fi

if [ "$ACTUAL" != "$EXPECTED" ]; then
    err "checksum mismatch for ${ASSET}

  expected: ${EXPECTED}
  actual:   ${ACTUAL}

The download may be corrupted or tampered with. Aborting."
fi

log "Checksum OK"

# --- Install -------------------------------------------------------------

mkdir -p "$INSTALL_DIR"

mv "$TMP_DIR/$ASSET" "$TARGET"
chmod 755 "$TARGET"

log ""
log "Installed: ${TARGET}"

# --- PATH check ----------------------------------------------------------

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        log ""
        log "NOTE: ${INSTALL_DIR} is not on your PATH."
        log "To make \`lato\` available in every terminal, add this line to"
        log "your shell configuration (~/.bashrc, ~/.zshrc, or equivalent):"
        log ""
        log "  export PATH=\"\$PATH:${INSTALL_DIR}\""
        log ""
        log "Then restart your shell (or run: source ~/.bashrc)."
        ;;
esac

log ""
log "Verify with: lato --version   (should print: lato ${VERSION})"
log "Then start it from any project directory: cd ~/some-project && lato"

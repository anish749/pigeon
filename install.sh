#!/usr/bin/env bash
set -euo pipefail

REPO="anish749/pigeon"
INSTALL_DIR="${PIGEON_INSTALL_DIR:-$HOME/.local/bin}"

# Installs a pre-built binary and skill file, then restarts the daemon.
# Usage: do_install <binary_path> <skill_src>
#   skill_src: local file path (dev) or https:// URL (release)
do_install() {
  local bin="$1"
  local skill_src="$2"

  echo "Stopping daemon (if running)..."
  "${INSTALL_DIR}/pigeon" daemon stop 2>/dev/null || true

  mkdir -p "$INSTALL_DIR"
  # Use install(1) rather than cp: on macOS, cp truncates the existing file
  # in place, preserving the com.apple.provenance xattr that caches the prior
  # binary's code signature. The new bytes then fail signature validation on
  # exec and are killed with SIGKILL. install unlinks the target first, giving
  # a fresh inode with no stale xattrs.
  install -m 755 "$bin" "${INSTALL_DIR}/pigeon"
  echo "Installed pigeon to ${INSTALL_DIR}/pigeon"

  if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
    echo ""
    echo "Note: ${INSTALL_DIR} is not in your PATH."
    echo "Add it with: export PATH=\"${INSTALL_DIR}:\$PATH\""
  fi

  CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
  SKILL_DST="${CLAUDE_DIR}/skills/pigeon"
  mkdir -p "$SKILL_DST"
  if [[ "$skill_src" == http* ]]; then
    curl -fsSL -o "$SKILL_DST/SKILL.md" "$skill_src"
  else
    cp "$skill_src" "$SKILL_DST/SKILL.md"
  fi
  echo "Installed Claude Code skill to ${SKILL_DST}/SKILL.md"

  echo "Starting daemon..."
  "${INSTALL_DIR}/pigeon" daemon start

  echo ""
  echo "Run 'pigeon help' to get started."
}

# --- dev mode: build from local source ---
# Usage: ./install.sh dev
if [ "${1:-}" = "dev" ]; then
  COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
  echo "Building dev-${COMMIT} from local source..."
  go build -ldflags "-X main.version=dev-${COMMIT}" -o /tmp/pigeon.dev ./cmd/pigeon
  do_install /tmp/pigeon.dev .claude/skills/pigeon/SKILL.md
  rm -f /tmp/pigeon.dev
  exit 0
fi

# --- release mode: download latest release from GitHub ---
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
esac

echo "Detected platform: ${OS}/${ARCH}"

LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST" ]; then
  echo "Error: could not determine latest release" >&2
  exit 1
fi
echo "Latest release: ${LATEST}"

ARCHIVE="pigeon_${LATEST#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${ARCHIVE}"

echo "Downloading ${URL}..."
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if ! curl -fsSL -o "${TMP}/${ARCHIVE}" "$URL"; then
  echo "Error: no release found for ${OS}/${ARCH}" >&2
  echo "Available builds may be limited. Check https://github.com/${REPO}/releases" >&2
  exit 1
fi

tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"

SKILL_URL="https://raw.githubusercontent.com/${REPO}/${LATEST}/.claude/skills/pigeon/SKILL.md"
do_install "${TMP}/pigeon" "$SKILL_URL"

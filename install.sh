#!/usr/bin/env bash
set -euo pipefail

REPO="anish749/pigeon"
INSTALL_DIR="${PIGEON_INSTALL_DIR:-$HOME/.local/bin}"

# Detect platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
esac

echo "Detected platform: ${OS}/${ARCH}"

# Get latest release tag
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST" ]; then
  echo "Error: could not determine latest release" >&2
  exit 1
fi
echo "Latest release: ${LATEST}"

# Download archive
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

# Extract and install binary
tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"
mkdir -p "$INSTALL_DIR"
cp "${TMP}/pigeon" "${INSTALL_DIR}/pigeon"
chmod +x "${INSTALL_DIR}/pigeon"
echo "Installed pigeon to ${INSTALL_DIR}/pigeon"

# Check PATH
if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  echo ""
  echo "Note: ${INSTALL_DIR} is not in your PATH."
  echo "Add it with: export PATH=\"${INSTALL_DIR}:\$PATH\""
fi

# Install Claude Code skill
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
SKILL_DST="${CLAUDE_DIR}/skills/pigeon"
SKILL_URL="https://raw.githubusercontent.com/${REPO}/${LATEST}/.claude/skills/pigeon/SKILL.md"
mkdir -p "$SKILL_DST"
if curl -fsSL -o "$SKILL_DST/SKILL.md" "$SKILL_URL"; then
  echo "Installed Claude Code skill to ${SKILL_DST}/SKILL.md"
fi

echo ""
echo "Run 'pigeon help' to get started."

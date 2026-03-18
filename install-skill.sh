#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
CMU_PATH="$REPO_DIR/cmu"
SKILL_SRC="$REPO_DIR/.claude/skills/cmu/SKILL.md.template"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
CLAUDE_DIR="${CLAUDE_DIR%/}"
SKILL_DST="$CLAUDE_DIR/skills/cmu"

mkdir -p "$SKILL_DST"
sed "s|__CMU_PATH__|$CMU_PATH|g" "$SKILL_SRC" > "$SKILL_DST/SKILL.md"
echo "Installed skill to $SKILL_DST/SKILL.md (cmu path: $CMU_PATH)"

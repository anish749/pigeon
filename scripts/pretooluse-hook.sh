#!/usr/bin/env bash
# PreToolUse hook that chains cc-tool-reviewer → pigeon.
# cc-tool-reviewer handles local allow/deny/AI review.
# When it can't decide ("ask"), pigeon routes to Slack/TUI for remote approval.
#
# Requires: jq, nc, pigeon on PATH.

set -euo pipefail

# Read hook input from stdin (Claude Code pipes it).
INPUT=$(cat)

# Try cc-tool-reviewer first.
if RESULT=$(echo "$INPUT" | nc -U /tmp/cc-tool-reviewer.sock 2>/dev/null); then
    # Parse the decision.
    DECISION=$(echo "$RESULT" | jq -r '.hookSpecificOutput.permissionDecision // empty' 2>/dev/null)

    case "$DECISION" in
        allow|deny)
            # cc-tool-reviewer made a definitive decision — pass through.
            echo "$RESULT"
            exit 0
            ;;
        ask)
            # cc-tool-reviewer can't decide — escalate to pigeon.
            echo "$INPUT" | pigeon hook pretooluse
            exit 0
            ;;
    esac
fi

# cc-tool-reviewer unavailable or returned invalid response — try pigeon.
echo "$INPUT" | pigeon hook pretooluse

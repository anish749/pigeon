# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
"""
Parse `pigeon workstream discover` text output into a workstreams.json
fragment. Reads /tmp/ws-<workspace>-<model>.txt files produced by the
discover command, extracts {name, focus} per workstream, and writes
JSON.

Usage:
  uv run experiments/focus-filter/parse_discover.py /tmp/ws-trudy-sonnet.txt
"""

import json
import re
import sys
from pathlib import Path


def parse(text):
    """Pull the JSON envelope from the 'claude cli response' log line and
    extract the LLM's structured workstream list."""
    # The log line has raw="<escaped-json>"; find it and unescape.
    match = re.search(r'msg="claude cli response" raw="(.+)"\s*$',
                      text, re.MULTILINE)
    if not match:
        # Fall back to parsing 'discovery: found workstream' lines
        return parse_log_lines(text)

    # The raw=... is JSON-escaped; unescape it.
    raw = match.group(1)
    raw = raw.encode().decode("unicode_escape")
    try:
        env = json.loads(raw)
    except json.JSONDecodeError:
        return parse_log_lines(text)

    result = env.get("result", "")
    # Strip ```json fences
    result = re.sub(r"^```(?:json)?\s*", "", result.strip())
    result = re.sub(r"\s*```\s*$", "", result)
    try:
        obj = json.loads(result)
    except json.JSONDecodeError:
        return parse_log_lines(text)

    out = []
    for ws in obj.get("workstreams", []):
        out.append({
            "name": ws.get("name", ""),
            "focus": ws.get("focus", ""),
        })
    return out


def parse_log_lines(text):
    out = []
    pattern = re.compile(
        r'discovery: found workstream"\s+name="([^"]+)"\s+focus="((?:[^"\\]|\\.)*)"'
    )
    for m in pattern.finditer(text):
        name = m.group(1)
        focus = m.group(2).encode().decode("unicode_escape")
        out.append({"name": name, "focus": focus})
    return out


def slugify(name):
    """Make a workstream id from the name: lowercase, hyphens, max 40 chars."""
    s = re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")
    return f"ws-{s[:36]}".strip("-")


def main():
    if len(sys.argv) < 2:
        print("usage: parse_discover.py <input>...", file=sys.stderr)
        sys.exit(1)

    for path in sys.argv[1:]:
        text = Path(path).read_text()
        ws_list = parse(text)
        # Slug the names to ids
        ws_with_ids = [
            {"id": slugify(w["name"]), "name": w["name"], "focus": w["focus"]}
            for w in ws_list
        ]
        # Print as a JSON fragment ready to paste into workstreams.json
        print(f"# {path}: {len(ws_with_ids)} workstreams")
        print(json.dumps(ws_with_ids, indent=2))
        print()


if __name__ == "__main__":
    main()

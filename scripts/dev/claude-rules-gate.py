#!/usr/bin/env python3
"""PreToolUse gate: point agents at path-scoped rules on first touch.

Reads the Claude Code PreToolUse hook payload on stdin (Edit/Write/MultiEdit),
matches the target file against the `globs:` frontmatter of docs/agent-rules/*.md,
and blocks the FIRST edit per (session, rule) with a short pointer so the agent
Reads the relevant rule and retries. Subsequent edits pass silently — the rules
are no longer auto-injected into every session, so this is the "필요한 규칙만,
필요한 시점에" enforcement half of CLAUDE.md's context policy.

Fail-open by design: any parsing/matching problem exits 0 (never break editing).
"""

import fnmatch
import json
import os
import re
import sys
import tempfile

RULES_SUBDIR = os.path.join("docs", "agent-rules")


def parse_frontmatter(text):
    """Return (description, [globs]) from a leading --- block. Accepts globs as
    a JSON-ish array, a bare comma-separated string, or a YAML list on the
    following lines ("- pattern")."""
    if not text.startswith("---\n"):
        return "", []
    end = text.find("\n---", 4)
    if end < 0:
        return "", []
    desc, globs = "", []
    in_globs_list = False
    for line in text[4:end].splitlines():
        stripped = line.strip()
        if in_globs_list:
            if stripped.startswith("- "):
                globs.append(stripped[2:].strip().strip("\"'"))
                continue
            in_globs_list = False
        key, _, val = line.partition(":")
        key, val = key.strip(), val.strip()
        if key == "description":
            desc = val.strip("\"'")
        elif key == "globs":
            if not val:
                in_globs_list = True  # YAML list form on following lines
            elif val.startswith("["):
                try:
                    globs = [str(g) for g in json.loads(val)]
                except ValueError:
                    globs = [g.strip().strip("\"'") for g in val.strip("[]").split(",")]
            else:
                globs = [g.strip().strip("\"'") for g in val.split(",")]
    return desc, [g for g in globs if g]


def glob_matches(rel_path, pattern):
    # fnmatch's `*` already crosses `/`, so `**` collapses to `*`.
    pat = pattern.replace("**/", "*").replace("**", "*")
    return fnmatch.fnmatchcase(rel_path, pat)


def main():
    payload = json.load(sys.stdin)
    tool_input = payload.get("tool_input") or {}
    file_path = tool_input.get("file_path") or tool_input.get("notebook_path") or ""
    if not file_path:
        return 0

    root = os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd") or os.getcwd()
    root = os.path.realpath(root)
    rel = os.path.relpath(os.path.realpath(file_path), root)
    if rel.startswith(".."):
        return 0  # outside the repo — not ours to gate

    rules_dir = os.path.join(root, RULES_SUBDIR)
    if not os.path.isdir(rules_dir):
        return 0

    session = re.sub(r"[^A-Za-z0-9_-]", "_", str(payload.get("session_id") or "nosession"))
    seen_dir = os.path.join(tempfile.gettempdir(), f"claude-rules-gate-{session}")
    os.makedirs(seen_dir, exist_ok=True)

    hits = []
    for fname in sorted(os.listdir(rules_dir)):
        if not fname.endswith(".md") or fname == "README.md":
            continue
        marker = os.path.join(seen_dir, fname)
        if os.path.exists(marker):
            continue
        try:
            with open(os.path.join(rules_dir, fname), encoding="utf-8") as f:
                desc, globs = parse_frontmatter(f.read(4096))
        except OSError:
            continue
        if any(glob_matches(rel, g) for g in globs):
            hits.append((fname, desc))
            open(marker, "w").close()  # notify once per session

    if not hits:
        return 0

    lines = [f"[rules-gate] '{rel}' 경로는 다음 룰의 적용 대상입니다 (이번 세션 첫 안내 — 재시도하면 통과):"]
    for fname, desc in hits:
        lines.append(f"  - {RULES_SUBDIR}/{fname} — {desc}")
    lines.append("작업과 관련된 룰을 Read로 확인한 뒤 같은 편집을 재시도하세요.")
    print("\n".join(lines), file=sys.stderr)
    return 2  # block this call once; the retry passes via the markers above


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — gate must never break editing
        sys.exit(0)

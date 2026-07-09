#!/usr/bin/env python3
"""PreToolUse nudge: steer symbol-name text searches to greppy.

Reads the Claude Code PreToolUse payload on stdin (Grep / Bash). When an agent
greps a BARE IDENTIFIER that greppy's symbol graph knows as an actual definition
(exact name match), block that one call with a pointer to the structural
commands (who-calls / brief / impact / semantic-search). The retry — or any
genuine text search (TODO, log strings, regex) — passes silently.

Discriminator: `greppy search-symbols <pat> --json` must return a hit whose
`name` equals the pattern exactly. Common words (TODO, error, handler) fuzzy-match
but never exact-match a definition, so they are never blocked.

Blocks once per (session, pattern): the immediate retry passes via the marker.
Fail-open by design: greppy missing, graph unbuilt, parse error, timeout → exit 0
(never break searching). This is the enforcement half of CLAUDE.md's greppy line.
"""

import hashlib
import json
import os
import re
import shlex
import subprocess
import sys
import tempfile

IDENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{2,}$")
GREP_TOKENS = {"grep", "egrep", "fgrep", "rg", "ripgrep"}


def bare_identifier(pat):
    """A single code-identifier-shaped token (len>=3), no regex/space/path."""
    return bool(pat) and bool(IDENT.match(pat))


def pattern_from_bash(command):
    """Best-effort: extract the search pattern from a `grep/rg PATTERN` command.
    Returns "" when the command is not a simple grep/rg, invokes greppy, or can't
    be parsed — all of which mean "not ours to gate"."""
    if "greppy" in command:
        return ""  # never gate an explicit greppy call
    try:
        tokens = shlex.split(command)
    except ValueError:
        return ""
    # Find a grep-family program token, then the first non-flag argument.
    for i, tok in enumerate(tokens):
        prog = os.path.basename(tok)
        if prog in GREP_TOKENS:
            for arg in tokens[i + 1:]:
                if arg.startswith("-"):
                    continue
                if arg in ("|", "&&", "||", ";"):
                    break  # end of this grep segment, no pattern seen
                return arg
            return ""
    return ""


def is_defined_symbol(pat, root):
    """True iff greppy's graph has a definition whose name == pat (exact)."""
    try:
        out = subprocess.run(
            ["greppy", "search-symbols", pat, "--json", "--root", root],
            capture_output=True, text=True, timeout=6,
        )
    except (OSError, subprocess.SubprocessError):
        return False  # greppy absent / crashed → fail open
    if out.returncode != 0 or not out.stdout:
        return False
    try:
        hits = (json.loads(out.stdout) or {}).get("hits") or []
    except ValueError:
        return False
    return any(h.get("name") == pat for h in hits)


def main():
    payload = json.load(sys.stdin)
    tool = payload.get("tool_name") or ""
    ti = payload.get("tool_input") or {}

    if tool == "Grep":
        pat = (ti.get("pattern") or "").strip()
    elif tool == "Bash":
        pat = pattern_from_bash(ti.get("command") or "")
    else:
        return 0

    if not bare_identifier(pat):
        return 0  # regex, multi-word, or empty → a real text search

    root = os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd") or os.getcwd()
    root = os.path.realpath(root)

    if not is_defined_symbol(pat, root):
        return 0  # not a known symbol → let the text search run

    # Block once per (session, pattern); the retry passes via the marker.
    session = re.sub(r"[^A-Za-z0-9_-]", "_", str(payload.get("session_id") or "nosession"))
    seen_dir = os.path.join(tempfile.gettempdir(), f"greppy-nudge-{session}")
    os.makedirs(seen_dir, exist_ok=True)
    marker = os.path.join(seen_dir, hashlib.sha1(pat.encode()).hexdigest())
    if os.path.exists(marker):
        return 0
    open(marker, "w").close()

    msg = [
        f"[greppy] '{pat}'는 코드 심볼입니다 (greppy 그래프에 정의 존재). 관계·구조 질문이면 텍스트 grep보다:",
        f"  greppy who-calls {pat}   ·  greppy brief {pat}   ·  greppy impact {pat}   ·  greppy callees {pat}",
        f'  이름을 모를 땐: greppy semantic-search "동작 설명"',
        "텍스트 문자열 검색이 목적이면 같은 검색을 그대로 재실행하세요 (세션당 심볼별 1회만 안내).",
    ]
    print("\n".join(msg), file=sys.stderr)
    return 2  # block this one call; retry passes via the marker


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — a nudge must never break searching
        sys.exit(0)

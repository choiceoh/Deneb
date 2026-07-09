#!/usr/bin/env python3
"""PreToolUse reminder: nudge agents to reach for CodeGraph when they start
touching source code — NON-BLOCKING, once per session.

The grep-nudge (codegraph-nudge.py) only catches symbol text-searches; the bigger
win — `codegraph_explore` before understanding/editing unfamiliar code — has no
grep trigger. So on the FIRST Read/Edit/Write of a *source* file in a session,
this injects a one-time system reminder (permissionDecision "allow" +
additionalContext) so the model sees it WITHOUT the tool being blocked.

Output contract (Claude Code hooks): exit 0 with
  {"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow",
   "additionalContext":"..."}}
Fail-open: anything unexpected → exit 0 with no output (never blocks or errors).
"""

import json
import os
import re
import sys
import tempfile

SOURCE_EXT = {
    ".go", ".ts", ".tsx", ".js", ".jsx", ".kt", ".kts", ".rs",
    ".py", ".java", ".c", ".cc", ".cpp", ".h", ".hpp", ".swift", ".scala",
}

REMINDER = (
    "이 레포엔 CodeGraph(MCP 코드 그래프)가 있다. 소스의 구조·관계·흐름과 "
    "'이 심볼 바꾸면 뭐 깨지나(블래스트 반경)'는 파일을 여러 개 Read/grep 하며 "
    "머릿속으로 재구성하지 말고 codegraph_explore(또는 CLI codegraph impact/callers/node)로 "
    "한 번에·정확히 파악하라 — 인터페이스→구현체 같은 동적 디스패치까지 추적된다. "
    "낯선 코드에 손대기 전 codegraph_explore를 먼저 고려. (세션 1회 안내)"
)


def main():
    payload = json.load(sys.stdin)
    ti = payload.get("tool_input") or {}
    path = ti.get("file_path") or ti.get("notebook_path") or ""
    if not path:
        return 0
    if os.path.splitext(path)[1].lower() not in SOURCE_EXT:
        return 0  # not a source file — no nudge

    root = os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd") or os.getcwd()
    root = os.path.realpath(root)
    rel = os.path.relpath(os.path.realpath(path), root)
    if rel.startswith(".."):
        return 0  # outside the repo

    # Once per session (total, not per file).
    session = re.sub(r"[^A-Za-z0-9_-]", "_", str(payload.get("session_id") or "nosession"))
    marker = os.path.join(tempfile.gettempdir(), f"codegraph-remind-{session}")
    if os.path.exists(marker):
        return 0
    open(marker, "w").close()

    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "allow",
            "additionalContext": REMINDER,
        }
    }, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — a reminder must never break tool use
        sys.exit(0)

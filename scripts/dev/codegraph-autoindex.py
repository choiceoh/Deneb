#!/usr/bin/env python3
"""SessionStart hook: ensure this worktree has a CodeGraph index, cheaply.

CodeGraph's index (.codegraph/) is per-project-path and NOT auto-built, so a
fresh worktree has no data and codegraph_explore just says "not available". This
hook seeds one from a sibling/ancestor index (+sync) or, failing that, runs a
full init — all backgrounded, so session start is never delayed.

The build logic lives in codegraph_worktree_index.ensure_index (shared with the
per-tool-use hooks, which cover worktrees entered AFTER SessionStart). Fail-open:
codegraph missing, not a git repo, or already indexed → exit 0.
"""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from codegraph_worktree_index import ensure_index  # noqa: E402


def main():
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        payload = {}
    root = os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd") or os.getcwd()
    ensure_index(root)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — a convenience hook must never break sessions
        sys.exit(0)

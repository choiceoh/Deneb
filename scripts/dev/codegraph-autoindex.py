#!/usr/bin/env python3
"""SessionStart hook: ensure this worktree has a CodeGraph index, cheaply.

CodeGraph's index (.codegraph/) is per-project-path and NOT auto-built, so a
fresh worktree has no data and codegraph_explore just says "not available". This
hook fixes that without a full 2-minute reindex per worktree:

  - If a sibling worktree already has an index, COPY it (sub-second on local
    disk) and run `codegraph sync` to reconcile this branch's drift (~0.6s).
  - Otherwise (first worktree), run a full `codegraph init`.

All work is backgrounded, so session start is never delayed — the index simply
becomes queryable a moment later. A time-boxed /tmp lock stops concurrent
sessions from double-indexing the same worktree (and self-heals after 15 min if
an attempt dies). Fail-open: codegraph missing, not a git repo, or already
indexed → exit 0.
"""

import glob
import hashlib
import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import time


def codegraph_bin():
    """Resolve the codegraph binary even under a minimal hook PATH (login profile
    not sourced). Same spots as codegraph-serve.sh. None if not installed."""
    for c in ("codegraph",
              os.path.expanduser("~/.local/bin/codegraph"),
              os.path.expanduser("~/.npm-global/bin/codegraph")):
        p = shutil.which(c)
        if p:
            return p
    return None


def main():
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        payload = {}
    root = os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd") or os.getcwd()
    root = os.path.realpath(root)

    cg = codegraph_bin()
    if cg is None:
        return 0                                        # tool not installed
    if not os.path.exists(os.path.join(root, ".git")):
        return 0                                        # not a repo/worktree
    if os.path.isdir(os.path.join(root, ".codegraph")):
        return 0                                        # already indexed

    key = hashlib.sha1(root.encode()).hexdigest()
    lock = os.path.join(tempfile.gettempdir(), f"codegraph-autoindex-{key}")
    now = time.time()
    if os.path.exists(lock) and now - os.path.getmtime(lock) < 900:
        return 0                                        # another session on it
    open(lock, "w").close()

    # Freshest sibling worktree that already carries an index → donor.
    parent = os.path.dirname(root)
    donors = [d for d in glob.glob(os.path.join(parent, "*", ".codegraph"))
              if os.path.realpath(os.path.dirname(d)) != root and os.path.isdir(d)]
    donor = max(donors, key=os.path.getmtime) if donors else None

    dst = os.path.join(root, ".codegraph")
    q = shlex.quote
    if donor:
        # Cheap path: seed from a sibling, then reconcile branch drift. If the
        # seed is unusable (partial/locked copy, sync error), self-heal to a
        # full init so the worktree still ends up correctly indexed.
        cmd = (f"cp -r {q(donor)} {q(dst)} && cd {q(root)} && codegraph sync"
               f" || {{ rm -rf {q(dst)}; cd {q(root)} && codegraph init; }}")
    else:
        cmd = f"cd {q(root)} && codegraph init"         # first-time full build

    log = os.path.join(tempfile.gettempdir(), f"codegraph-autoindex-{key}.log")
    try:
        # LOGIN shell: restores the profile PATH (node + codegraph) that a
        # minimal detached hook env lacks. Detached so it never blocks startup.
        subprocess.Popen(
            ["bash", "-lc", cmd],
            stdout=open(log, "w"), stderr=subprocess.STDOUT,
            start_new_session=True,
        )
    except OSError:
        pass                                            # fail-open
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — a convenience hook must never break sessions
        sys.exit(0)

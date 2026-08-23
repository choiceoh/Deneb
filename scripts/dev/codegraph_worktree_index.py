"""Shared helper: ensure a git worktree has its OWN CodeGraph index.

CodeGraph's index (.codegraph/) is per-project-path and NOT auto-built. A
worktree without one makes codegraph walk UP to the parent checkout's index and
serve that tree's (often a different branch's) symbols — the "results reflect a
different git working tree" warning, i.e. stale/wrong search results.

`ensure_index(root)` fixes that cheaply and idempotently:
  - already has a local .codegraph  → no-op (one stat, the common case)
  - a donor index exists (sibling worktree, or the ancestor checkout codegraph
    would otherwise fall back to) → copy it (sub-second) + `codegraph sync` to
    reconcile this branch's drift
  - otherwise → full `codegraph init`
All work is backgrounded (never blocks the caller) and per-root locked (two
hooks firing at once don't double-build). Fail-open throughout: codegraph
missing, not a worktree, or any error → silently do nothing.

Imported by codegraph-autoindex.py (SessionStart) and the per-tool-use hooks
(codegraph-remind.py / codegraph-nudge.py) so a worktree entered MID-session —
e.g. Claude Code's EnterWorktree, which never ran the SessionStart hook — still
self-heals on first source access instead of serving the parent index.
"""

import glob
import hashlib
import os
import shlex
import shutil
import subprocess
import tempfile
import time

_LOCK_TTL = 900  # seconds; a dead build attempt self-heals after this


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


def _nearest_ancestor_index(root):
    """The .codegraph codegraph would fall back to by walking up from root — the
    ideal donor for a nested worktree (same repo, sync reconciles branch drift).
    Skips root itself. None if no ancestor carries an index."""
    anc = root
    while True:
        parent = os.path.dirname(anc)
        if parent == anc:  # filesystem root
            return None
        anc = parent
        cand = os.path.join(anc, ".codegraph")
        if os.path.isdir(cand):
            return cand


def ensure_index(root):
    """Ensure `root` (a git worktree/checkout) has its own .codegraph. Cheap,
    idempotent, backgrounded, fail-open. Returns nothing; never raises."""
    try:
        root = os.path.realpath(root)
        # Cheapest + most common check first: a local index already exists.
        if os.path.isdir(os.path.join(root, ".codegraph")):
            return
        # .git is a DIR in the main checkout, a FILE in a linked worktree — both
        # satisfy exists(); a plain directory (no repo) does not.
        if not os.path.exists(os.path.join(root, ".git")):
            return
        if codegraph_bin() is None:
            return

        key = hashlib.sha1(root.encode()).hexdigest()
        lock = os.path.join(tempfile.gettempdir(), f"codegraph-autoindex-{key}")
        if os.path.exists(lock) and time.time() - os.path.getmtime(lock) < _LOCK_TTL:
            return  # another hook/session is already building this root
        open(lock, "w").close()

        # Freshest donor: a sibling worktree's index, or the ancestor checkout's.
        parent = os.path.dirname(root)
        donors = [d for d in glob.glob(os.path.join(parent, "*", ".codegraph"))
                  if os.path.realpath(os.path.dirname(d)) != root and os.path.isdir(d)]
        anc = _nearest_ancestor_index(root)
        if anc:
            donors.append(anc)
        donor = max(donors, key=os.path.getmtime) if donors else None

        dst = os.path.join(root, ".codegraph")
        q = shlex.quote
        here = os.path.dirname(os.path.abspath(__file__))
        seed = os.path.join(here, "codegraph-seed-index.sh")
        rpc = os.path.join(here, "rpcmap_codegraph_sync.py")
        if donor:
            # Seed from the donor (skip daemon runtime), then sync + rpcmap.
            # If the seed is unusable, self-heal to a full init + rpcmap inject.
            cmd = (
                f"bash {q(seed)} {q(donor)} {q(root)}"
                f" || {{ rm -rf {q(dst)}; cd {q(root)} && codegraph init"
                f" && python3 {q(rpc)} {q(root)}; }}"
            )
        else:
            cmd = f"cd {q(root)} && codegraph init && python3 {q(rpc)} {q(root)}"

        log = os.path.join(tempfile.gettempdir(), f"codegraph-autoindex-{key}.log")
        # LOGIN shell restores the profile PATH (node + codegraph) a minimal hook
        # env lacks; detached so it never blocks the caller.
        with open(log, "w") as log_file:
            subprocess.Popen(
                ["bash", "-lc", cmd],
                stdout=log_file, stderr=subprocess.STDOUT,
                start_new_session=True,
            )
    except Exception:  # noqa: BLE001 — a convenience helper must never break a hook
        pass

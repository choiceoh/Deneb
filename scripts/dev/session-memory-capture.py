#!/usr/bin/env python3
"""SessionEnd hook: capture a deterministic skeleton of the coding session.

Reads the hook payload from stdin (session_id, transcript_path, cwd), gathers
git facts about the branch's work, and records one JSON "episode" per session.
No LLM, no network -- pure local git, so it can never fail or slow a session.

It does NOT fire once per session: SessionEnd runs again on resume and after
compaction. So an episode REPLACES the earlier row for its session rather than
appending, and the whole log is compacted on each write (see `compact`).

Two layers of memory come out of this hook:

- the outcome skeleton written here -- branch, commits, files, opening ask;
- the decision rationale mined from git commit bodies into decisions.jsonl by
  `decision_mine`, refreshed at the end of this hook.

`card.md` remains the hand-written digest an agent may keep, and the surfacing
hook still shows it. It is no longer the only semantic layer, because relying
on an agent to write one left it empty in every session on record.

Storage is a single shared dir (~/.claude/deneb-session-memory), NOT the repo
and NOT per-worktree, so every worktree/session writes one timeline -- which is
why writes here take an interprocess lock.

Fail-safe contract: ANY error exits 0 with no output. A memory hook must never
disrupt a coding session.
"""

import contextlib
import json
import os
import subprocess
import sys
import time

MEM_DIR = os.path.expanduser("~/.claude/deneb-session-memory")
EPISODES = os.path.join(MEM_DIR, "episodes.jsonl")

MAX_COMMITS = 20
MAX_FILES_SAMPLE = 12
FIRST_MSG_CAP = 240

# One row per session, newest state wins. SessionEnd is not once-per-session:
# it fires again on resume and after compaction, and an unconditional append
# made the log mostly duplicates (10 rows, 4 distinct -- one session accounted
# for 9 of them). Since the surfacing hook shows only the last few rows, a
# single chatty session could evict every other session's memory. A later
# episode for a session strictly supersedes its earlier one: same branch and
# session, further along.
MAX_EPISODES = 200


def normalize_cwd(p):
    """MSYS/git-bash paths (/c/Users/...) -> Windows (C:/Users/...) so a native
    git invoked from Python resolves them. Native paths pass through unchanged."""
    if len(p) >= 3 and p[0] == "/" and p[2] == "/" and p[1].isalpha():
        return p[1].upper() + ":/" + p[3:]
    return p


def git(cwd, *args):
    """Run a git command, returning stdout (stripped) or '' on any failure."""
    try:
        out = subprocess.run(
            ["git", "-C", cwd, *args],
            capture_output=True,
            text=True,
            timeout=8,
        )
        return out.stdout.strip() if out.returncode == 0 else ""
    except Exception:
        return ""


def first_user_message(transcript_path):
    """Best-effort: the session's opening ask, as a human-readable topic.

    The transcript is JSONL (one message object per line). We take the first
    genuine user text turn, skipping tool-result / system-injected user turns
    (which start with '<' or embed tool_result). Truncated so we never store a
    large or sensitive blob -- it is the operator's own prompt, capped short.
    """
    if not transcript_path:
        return ""
    try:
        with open(transcript_path, "r", encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                msg = obj.get("message") or {}
                if obj.get("type") != "user" and msg.get("role") != "user":
                    continue
                content = msg.get("content")
                text = ""
                if isinstance(content, str):
                    text = content
                elif isinstance(content, list):
                    for block in content:
                        if isinstance(block, dict) and block.get("type") == "text":
                            text = block.get("text", "")
                            break
                text = text.strip()
                # Skip tool-result / system-injected user turns -- not real asks.
                if not text or text.startswith("<") or "tool_result" in text[:40]:
                    continue
                return text[:FIRST_MSG_CAP]
    except Exception:
        return ""
    return ""


def mine_decisions(cwd):
    """Refresh the mined decision log from git, incrementally.

    Runs here rather than at SessionStart because it is the same kind of work
    this hook already does (local git, no network) and because SessionEnd has
    no latency budget to protect. Cost is ~0.4s for a cold 400-commit build and
    ~0.05s once the cursor is set.

    Attached to a hook on purpose: the semantic memory layer this feeds
    (``card.md``) was left to an agent's goodwill and stayed empty across every
    session on record. A memory source nobody is obliged to write does not get
    written, so this one refreshes itself.
    """
    try:
        _load_miner().mine(cwd=cwd)
    except Exception:
        # Same contract as the rest of this hook: memory never breaks a session.
        pass


def _load_miner():
    """Import the decision miner from this script's own directory, or None."""
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    import decision_mine

    return decision_mine


@contextlib.contextmanager
def _memory_lock(name, directory):
    """The miner's interprocess lock, degrading to a no-op if it cannot load.

    Read-modify-write on a dir shared by every worktree needs more than an
    atomic rename: two SessionEnds can each read the same snapshot and replace
    it in turn, and the later write just drops the earlier episode without
    raising. Borrowed rather than duplicated so both writers use one lock.
    """
    try:
        miner = _load_miner()
    except Exception:
        yield False
        return
    with miner.memory_lock(name, mem_dir=directory) as held:
        yield held


def compact(rows):
    """Keep the last row per session, in place; rows with no id are kept as-is.

    Applied to the whole file rather than only the incoming session, so a log
    that already accumulated duplicates heals on the next write instead of
    carrying them forever -- a session that has ended for good never gets
    another chance to replace its own rows.
    """
    last_index = {}
    for i, row in enumerate(rows):
        sid = row.get("session_id")
        if sid:
            last_index[sid] = i
    out = []
    for i, row in enumerate(rows):
        sid = row.get("session_id")
        if sid and last_index.get(sid) != i:
            continue
        out.append(row)
    return out


def append_episode(episode, path=EPISODES):
    """Record the episode, replacing any earlier row for the same session.

    Rewrite-and-replace rather than append, so a session that ends repeatedly
    leaves one row instead of nine. The rewrite is atomic (temp + os.replace)
    because the memory dir is shared across worktrees and two sessions can end
    at once. If anything about the rewrite fails we fall back to a plain
    append: a duplicate row is a much smaller loss than a dropped episode, and
    the whole point of this hook is that it cannot fail loudly.
    """
    try:
        os.makedirs(MEM_DIR, exist_ok=True)
        with _memory_lock("episodes", os.path.dirname(path) or MEM_DIR):
            rows = []
            try:
                with open(path, "r", encoding="utf-8", errors="replace") as f:
                    for line in f:
                        if not line.strip():
                            continue
                        try:
                            rows.append(json.loads(line))
                        except Exception:
                            continue
            except FileNotFoundError:
                pass
            rows.append(episode)
            rows = compact(rows)[-MAX_EPISODES:]
            tmp = f"{path}.{os.getpid()}.tmp"
            with open(tmp, "w", encoding="utf-8") as f:
                for row in rows:
                    f.write(json.dumps(row, ensure_ascii=False) + "\n")
            os.replace(tmp, path)
            return
    except Exception:
        pass

    try:
        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(episode, ensure_ascii=False) + "\n")
    except Exception:
        pass


def main():
    try:
        payload = json.load(sys.stdin)
    except Exception:
        payload = {}

    cwd = normalize_cwd(payload.get("cwd") or os.getcwd())
    session_id = payload.get("session_id", "")
    transcript_path = payload.get("transcript_path", "")

    branch = git(cwd, "rev-parse", "--abbrev-ref", "HEAD")
    head = git(cwd, "rev-parse", "--short", "HEAD")
    # Commits this branch carries over origin/main -- the branch's deliverable.
    commits = [
        ln
        for ln in git(
            cwd, "log", "--oneline", "-n", str(MAX_COMMITS), "origin/main..HEAD"
        ).splitlines()
        if ln
    ]
    files = [ln for ln in git(cwd, "diff", "--name-only", "origin/main...HEAD").splitlines() if ln]
    dirty = len([ln for ln in git(cwd, "status", "--porcelain").splitlines() if ln])

    episode = {
        "ts": int(time.time()),
        "date": time.strftime("%Y-%m-%d %H:%M", time.localtime()),
        "session_id": session_id,
        "cwd": cwd,
        "branch": branch,
        "head": head,
        "topic": first_user_message(transcript_path),
        "commits": commits[:MAX_COMMITS],
        "files_changed": len(files),
        "files_sample": files[:MAX_FILES_SAMPLE],
        "dirty_files": dirty,
    }

    append_episode(episode)
    mine_decisions(cwd)
    sys.exit(0)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never let a memory hook disrupt a session.
        sys.exit(0)

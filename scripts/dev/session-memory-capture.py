#!/usr/bin/env python3
"""SessionEnd hook: capture a deterministic skeleton of the coding session.

Fires once when a Claude Code session ends. Reads the hook payload from stdin
(session_id, transcript_path, cwd), gathers git facts about the branch's work,
and appends one JSON "episode" to the shared session-memory log. No LLM, no
network -- pure local git, so it can never fail or slow a session. The semantic
layer (decisions, lessons, self-corrections) is written separately by the agent
into card.md; this hook only captures the outcome skeleton that is tedious to
reconstruct from memory next time.

Storage is a single shared dir (~/.claude/deneb-session-memory), NOT the repo and
NOT per-worktree, so every worktree/session appends to one timeline.

Fail-safe contract: ANY error exits 0 with no output. A memory hook must never
disrupt a coding session.
"""

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

    try:
        os.makedirs(MEM_DIR, exist_ok=True)
        with open(EPISODES, "a", encoding="utf-8") as f:
            f.write(json.dumps(episode, ensure_ascii=False) + "\n")
    except Exception:
        pass

    sys.exit(0)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never let a memory hook disrupt a session.
        sys.exit(0)

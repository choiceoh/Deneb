#!/usr/bin/env python3
"""Cross-session file claims: who is editing what, across every coding harness.

Worktree isolation stops parallel agents corrupting each other's checkout; it
does NOT stop them editing the same file on two branches and discovering it at
merge time. Four harnesses (Claude, Cursor, ZCode, Codex) run against this repo
concurrently, and CLAUDE.md's multi-agent rules are discipline, not enforcement.

This module is the enforcement half, shaped by what an exploratory coding
session actually is:

  - Claims are OBSERVED, not declared. A ticket system can demand a `touches`
    list up front; an agent cannot know what it will edit until it edits it.
    The first write registers the claim; later writers are the ones told.
  - Delivery is BLOCK-ONCE, not ask-forever. The first write to a contested
    file is stopped with the full story (who, where, how long ago); the warned
    session's retry passes. The agent — not an operator dialog — decides
    whether "same file, different function" applies, which mirrors how the
    rules-gate and the codegraph nudge already talk to agents here, and it is
    the one delivery channel all three hook protocols share (Claude
    permissionDecision, Cursor permission JSON, ZCode exit-2).
  - The ledger key is "<repo identity>:<repo-relative path>". The relative half
    makes two worktrees' copies of one file collide (same eventual merge); the
    identity half keeps Deneb's README from colliding with SolarFlow's. Identity
    is the normalized origin URL — the literal definition of "meets at the same
    merge" — with the resolved common .git path as the local-only fallback.

Consumed by deneb-concurrency-guard.py (check 4/4b). Also a CLI:

    python3 deneb_file_claims.py status   # live claims, newest first
    python3 deneb_file_claims.py reset    # drop the ledger

Fail-open everywhere: a claims subsystem that can break an edit would be a
worse concurrency hazard than the collisions it prevents.
"""

from __future__ import annotations

import fcntl
import json
import os
import time

# How long one session's claim keeps warning other sessions. Longer than the
# live-test lock (10 min) because an editing session spans hours. The asymmetry
# picks the number: a stale warning costs one blocked-then-retried write, a
# missed collision costs a merge conflict or silently undone work.
CLAIM_TTL = int(os.environ.get("DENEB_FILE_CLAIM_TTL", "21600"))  # 6h
# Bounded so one busy session cannot grow the ledger without limit.
CLAIM_MAX = 400


def ledger_path():
    return os.path.join(os.path.expanduser("~"), ".claude", "deneb-file-claims.json")


# ---------------------------------------------------------------------------
# Keying


def claim_key(real_path):
    """(ledger key, display-relative path) for a file, or None outside a repo.

    Walks the filesystem rather than shelling out to git — this runs inside a
    hook that fires on every edit.
    """
    directory = os.path.dirname(real_path)
    while True:
        if _is_checkout_root(directory):
            rel = os.path.relpath(real_path, directory)
            return _repo_identity(directory) + ":" + rel, rel
        parent = os.path.dirname(directory)
        if parent == directory:
            return None
        directory = parent


def _is_checkout_root(directory):
    """Whether .git here is a REAL checkout marker.

    A bare exists(".git") is too weak: an empty .git directory satisfies it,
    and one exists at /tmp/.git on this fleet (2026-08-11, empty) — that would
    put every temp file in a repo rooted at /tmp. A checkout has .git/HEAD; a
    linked worktree has .git as a file holding a gitdir: pointer.
    """
    marker = os.path.join(directory, ".git")
    if os.path.isfile(marker):
        return True
    return os.path.isfile(os.path.join(marker, "HEAD"))


def _repo_identity(checkout_root):
    common = _common_git_dir(checkout_root)
    url = _origin_url(os.path.join(common, "config"))
    if url:
        return _normalize_origin(url)
    return common


def _common_git_dir(checkout_root):
    """The shared .git directory behind a checkout (a linked worktree resolves
    to its parent repository's)."""
    marker = os.path.join(checkout_root, ".git")
    if os.path.isdir(marker):
        return os.path.realpath(marker)
    try:
        with open(marker, encoding="utf-8") as fh:
            first = fh.readline().strip()
    except OSError:
        return os.path.realpath(marker)
    if not first.startswith("gitdir:"):
        return os.path.realpath(marker)
    gitdir = first[len("gitdir:"):].strip()
    if not os.path.isabs(gitdir):
        gitdir = os.path.join(checkout_root, gitdir)
    gitdir = os.path.realpath(gitdir)
    head, _ = os.path.split(gitdir)
    if os.path.basename(head) == "worktrees":
        return os.path.dirname(head)
    return gitdir


def _origin_url(config_path):
    """[remote "origin"] url by line scan — no git subprocess in a hot hook."""
    try:
        with open(config_path, encoding="utf-8") as fh:
            in_origin = False
            for line in fh:
                stripped = line.strip()
                if stripped.startswith("["):
                    in_origin = stripped.replace(" ", "") == '[remote"origin"]'
                    continue
                if in_origin and stripped.startswith("url"):
                    _, _, value = stripped.partition("=")
                    return value.strip() or None
    except OSError:
        pass
    return None


def _normalize_origin(url):
    """One spelling per origin.

    Clones of one repo routinely disagree about the URL — the fleet's prod
    checkout says …/deneb while ~/deneb-dev says …/Deneb.git. Same repo; a
    byte-compared identity split them (caught live 2026-08-27). Scheme, ssh
    form, case, .git suffix and trailing slash all fold. A rare case-sensitive
    host would only warn slightly too often — the cheap direction to be wrong.
    """
    u = url.strip().lower().rstrip("/")
    for prefix in ("https://", "http://", "ssh://", "git://"):
        if u.startswith(prefix):
            u = u[len(prefix):]
            break
    if u.startswith("git@"):
        u = u[len("git@"):].replace(":", "/", 1)
    if u.endswith(".git"):
        u = u[: -len(".git")]
    return u


# ---------------------------------------------------------------------------
# Ledger


def observe_write(real_path, cwd, session_id):
    """Record that session_id is writing real_path; return a warning dict the
    first time it collides with another live session's claim, else None.

    The warning is returned exactly once per (file, session): the caller
    delivers it by blocking the write, and the session's retry — or any later
    write — passes. The holder keeps the claim, so a THIRD session is still
    told. Everything about this is best-effort; no failure here may block an
    edit.
    """
    hook_state = os.path.join(os.path.expanduser("~"), ".claude") + os.sep
    if real_path.startswith(hook_state):
        return None
    keyed = claim_key(real_path)
    if keyed is None:
        # Being inside a checkout IS the scratch filter: anything outside a
        # repo has no merge to collide at.
        return None
    key, rel = keyed

    try:
        return _with_locked_ledger(
            lambda claims, now: _observe_locked(claims, now, key, rel, cwd, session_id)
        )
    except Exception:  # noqa: BLE001 — fail-open, like every check around it
        return None


def _observe_locked(claims, now, key, rel, cwd, session_id):
    held = claims.get(key)
    holder = str(held.get("session_id") or "") if held else ""
    if held and holder not in ("", session_id):
        warned = held.get("warned")
        warned = list(warned) if isinstance(warned, list) else []
        if session_id in warned:
            return None  # already told this session about this file
        warned.append(session_id)
        held["warned"] = warned
        return {
            "rel": rel,
            "holder": holder,
            "holder_cwd": str(held.get("cwd") or ""),
            "age_seconds": int(now - float(held.get("ts") or 0)),
        }
    claims[key] = {
        "session_id": session_id, "ts": now, "cwd": cwd or "",
        # Carry the warned list across the holder's own refreshes, so a session
        # that was told once is not told again every time the holder works.
        "warned": (held or {}).get("warned") or [],
    }
    return None


def _with_locked_ledger(fn):
    """Read-modify-write under an exclusive flock.

    Two harness hooks firing at once are not hypothetical — that is the
    premise of the whole feature. Without the lock the slower writer clobbers
    the faster one's claim, which is precisely a lost warning.
    """
    path = ledger_path()
    os.makedirs(os.path.dirname(path), exist_ok=True)
    now = time.time()
    with open(path, "a+", encoding="utf-8") as fh:
        fcntl.flock(fh, fcntl.LOCK_EX)
        fh.seek(0)
        try:
            raw = json.load(fh)
        except ValueError:
            raw = {}
        claims = {
            key: value
            for key, value in (raw.items() if isinstance(raw, dict) else [])
            if isinstance(value, dict) and now - float(value.get("ts") or 0) < CLAIM_TTL
        }
        result = fn(claims, now)
        if len(claims) > CLAIM_MAX:
            oldest = sorted(claims.items(), key=lambda kv: float(kv[1].get("ts") or 0))
            for key, _ in oldest[: len(claims) - CLAIM_MAX]:
                claims.pop(key, None)
        fh.seek(0)
        fh.truncate()
        json.dump(claims, fh)
        return result


def warning_text(warning):
    """The one message every harness shows, in its own channel."""
    lines = [
        f"다른 세션({warning['holder'][:12]}…)이 {_ago(warning['age_seconds'])} 전 "
        f"같은 파일을 편집했습니다: {warning['rel']}",
    ]
    if warning["holder_cwd"]:
        lines.append(f"그쪽 작업 위치: {warning['holder_cwd']}")
    lines.append(
        "워크트리가 달라 덮어쓰기는 안 나지만, 머지 때 충돌하거나 서로의 수정을 "
        "무효화할 수 있습니다. 의도한 것이면 같은 편집을 그대로 다시 시도하세요 — "
        "이 알림은 세션당 한 번만 막습니다."
    )
    return "\n".join(lines)


def _ago(seconds):
    if seconds < 90:
        return f"{seconds}초"
    if seconds < 5400:
        return f"{seconds // 60}분"
    return f"{seconds // 3600}시간"


# ---------------------------------------------------------------------------
# CLI


def main(argv):
    command = argv[1] if len(argv) > 1 else "status"
    if command == "reset":
        try:
            os.remove(ledger_path())
        except OSError:
            pass
        print("클레임 원장을 비웠습니다.")
        return 0
    now = time.time()
    try:
        with open(ledger_path(), encoding="utf-8") as fh:
            raw = json.load(fh)
    except (OSError, ValueError):
        raw = {}
    live = [
        (key, value) for key, value in (raw.items() if isinstance(raw, dict) else [])
        if isinstance(value, dict) and now - float(value.get("ts") or 0) < CLAIM_TTL
    ]
    if not live:
        print("살아있는 클레임 없음.")
        return 0
    live.sort(key=lambda kv: -float(kv[1].get("ts") or 0))
    for key, value in live:
        age = _ago(int(now - float(value.get("ts") or 0)))
        warned = ",".join(value.get("warned") or []) or "-"
        print(f"{age:>6} 전  {str(value.get('session_id'))[:14]:14}  {key}  (알림: {warned})")
    return 0


if __name__ == "__main__":
    import sys
    sys.exit(main(sys.argv))

#!/usr/bin/env python3
"""PreToolUse concurrency guard: stop parallel Deneb sessions clobbering each other.

Canonical source. ~/.claude/hooks/deneb-concurrency-guard.py must stay a stable
shim (installed by scripts/dev/install-user-hooks.sh) that execs the production
checkout's copy of THIS file — so the guard's logic ships through the normal
merge → auto-deploy pipeline instead of living as an unversioned home-dir file
(the 2026-07-18 incident: the only copy was overwritten with a stub and lost).

Three checks, per the original 2026-07-09 spec:
  1. prod tree   — Write/Edit/MultiEdit under ~/deneb/ (main-only, auto-deploy)
                   → deny. Worktrees (~/deneb/.claude/worktrees/*) and
                   ~/deneb-dev pass.
  2. add scoping — `git add -A/./--all` and `git commit -a/-am` → ask, steering
                   to scripts/committer (keeps other sessions' files out).
                   shlex-tokenized, so "-a" inside a commit message never trips.
  3. live-test   — `live-test.sh restart` while another session holds a fresh
                   (<10 min) ~/.claude/deneb-livetest.lock → ask (shared dev
                   gateway / genesis / wiki state). Own lock or stale → pass.

Self-gate: only acts when the touched path / cwd / command is Deneb-related, so
the global wiring is a no-op in unrelated projects. Fail-open by design: any
internal error allows the tool. Known ceiling (unchanged from the original):
check 1 sees tool file_path only — Bash `sed -i`/redirects into prod are not
parsed (rare; CLAUDE.md already forbids them).
"""

import json
import os
import shlex
import sys
import time

LIVETEST_LOCK_TTL = 600  # seconds; matches the original 10-minute lock


def prod_root():
    """The production checkout (main-only, auto-deploy owns it)."""
    return os.path.realpath(os.path.expanduser("~/deneb"))


def livetest_lock_path():
    return os.path.expanduser("~/.claude/deneb-livetest.lock")


def decide(decision, reason):
    """Emit a PreToolUse permission decision (deny/ask) and allow-exit."""
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": decision,
            "permissionDecisionReason": reason,
        }
    }, ensure_ascii=False))
    return 0


def is_deneb_context(cwd, command):
    """Self-gate: Deneb checkouts/worktrees all carry 'deneb' in their path."""
    haystack = f"{cwd} {command}".lower()
    return "deneb" in haystack


def check_prod_edit(tool, tool_input, cwd):
    """Check 1: deny direct edits inside the prod tree (worktrees pass)."""
    if tool not in ("Write", "Edit", "MultiEdit"):
        return None
    file_path = (tool_input.get("file_path") or "").strip()
    if not file_path:
        return None
    if not os.path.isabs(file_path):
        file_path = os.path.join(cwd or os.getcwd(), file_path)
    real = os.path.realpath(file_path)
    prod = prod_root()
    if real != prod and not real.startswith(prod + os.sep):
        return None
    if real.startswith(os.path.join(prod, ".claude", "worktrees") + os.sep):
        return None  # isolated worktrees under the prod checkout are fine
    return decide("deny", (
        f"프로드 전용 트리({prod}) 직접 편집은 금지 — main만, auto-deploy가 관리 "
        "(더럽히면 배포 동결). 개발은 ~/deneb-dev 또는 워크트리에서 하세요."
    ))


def command_segments(command):
    """shlex-tokenize and split on shell separators; unparseable → no segments."""
    try:
        tokens = shlex.split(command)
    except ValueError:
        return []
    segments, current = [], []
    for tok in tokens:
        if tok in (";", "&&", "||", "|", "&"):
            if current:
                segments.append(current)
            current = []
        else:
            current.append(tok)
    if current:
        segments.append(current)
    return segments


def git_subcommand(segment):
    """(subcommand, args-after-it) for a `git …` segment, else (None, [])."""
    if not segment or os.path.basename(segment[0]) != "git":
        return None, []
    i = 1
    while i < len(segment):
        tok = segment[i]
        if tok in ("-C", "-c", "--git-dir", "--work-tree"):
            i += 2  # global flag with a value
            continue
        if tok.startswith("-"):
            i += 1
            continue
        return tok, segment[i + 1:]
    return None, []


def check_git_scoping(command):
    """Check 2: ask on repo-wide staging so parallel sessions' files stay out."""
    for segment in command_segments(command):
        sub, args = git_subcommand(segment)
        if sub == "add":
            if any(a in ("-A", "--all", ".") for a in args):
                return decide("ask", (
                    "git add -A/. 는 다른 세션의 파일까지 스테이징합니다. "
                    "scripts/committer \"<msg>\" <files…> 로 내 변경만 커밋하세요."
                ))
        elif sub == "commit":
            for a in args:
                if a.startswith("--"):
                    continue  # --amend, --allow-empty, … are not -a
                if a.startswith("-") and "a" in a[1:]:
                    return decide("ask", (
                        "git commit -a 는 다른 세션의 변경까지 쓸어 담습니다. "
                        "scripts/committer \"<msg>\" <files…> 를 쓰세요."
                    ))
    return None


def check_livetest(command, session_id):
    """Check 3: ask when another session ran live-test restart <10 min ago."""
    if "live-test.sh" not in command or "restart" not in command:
        return None
    lock = livetest_lock_path()
    now = time.time()
    try:
        with open(lock, encoding="utf-8") as fh:
            held = json.load(fh)
        holder, ts = str(held.get("session_id") or ""), float(held.get("ts") or 0)
    except (OSError, ValueError):
        holder, ts = "", 0.0
    age = now - ts
    if holder and holder != session_id and age < LIVETEST_LOCK_TTL:
        return decide("ask", (
            f"다른 세션({holder[:12]}…)이 {int(age)}초 전 live-test restart를 실행했습니다 "
            "— dev 게이트웨이·genesis/wiki 상태 공유로 충돌 가능. 계속할까요? "
            f"(락 리셋: rm {lock})"
        ))
    try:  # take/refresh the lock for this session; best-effort
        os.makedirs(os.path.dirname(lock), exist_ok=True)
        with open(lock, "w", encoding="utf-8") as fh:
            json.dump({"session_id": session_id, "ts": now}, fh)
    except OSError:
        pass
    return None


def main():
    payload = json.load(sys.stdin)
    tool = payload.get("tool_name") or ""
    tool_input = payload.get("tool_input") or {}
    cwd = payload.get("cwd") or os.getcwd()
    session_id = str(payload.get("session_id") or "nosession")

    result = check_prod_edit(tool, tool_input, cwd)
    if result is not None:
        return result

    if tool == "Bash":
        command = tool_input.get("command") or ""
        if not is_deneb_context(cwd, command):
            return 0
        result = check_git_scoping(command)
        if result is not None:
            return result
        result = check_livetest(command, session_id)
        if result is not None:
            return result
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — the guard must never break tooling
        sys.exit(0)

#!/usr/bin/env python3
"""PreToolUse concurrency guard: stop parallel Deneb sessions clobbering each other.

Canonical source. ~/.claude/hooks/deneb-concurrency-guard.py must stay a stable
shim (installed by scripts/dev/install-user-hooks.sh) that execs the production
checkout's copy of THIS file — so the guard's logic ships through the normal
merge → auto-deploy pipeline instead of living as an unversioned home-dir file
(the 2026-07-18 incident: the only copy was overwritten with a stub and lost).

Checks 1-3 are the original 2026-07-09 spec; 1b and 4 were added later:
  1. prod tree   — Write/Edit/MultiEdit under ~/deneb/ (main-only, auto-deploy)
                   → deny. Worktrees (~/deneb/.claude/worktrees/*) and
                   ~/deneb-dev pass.
  2. add scoping — `git add -A/./--all` and `git commit -a/-am` → ask, steering
                   to scripts/committer (keeps other sessions' files out).
                   shlex-tokenized, so "-a" inside a commit message never trips.
  3. live-test   — `live-test.sh restart` while another session holds a fresh
                   (<10 min) ~/.claude/deneb-livetest.lock → ask (shared dev
                   gateway / genesis / wiki state). Own lock or stale → pass.

  4. file claim  — a write to a repo file another LIVE session wrote within
                   DENEB_FILE_CLAIM_TTL (6h) → ask, ONCE per (session, file).
                   Keyed by REPO-RELATIVE path, so two worktrees editing the
                   same file collide even though their absolute paths differ.
                   Worktree isolation already prevents corruption; this covers
                   the part it cannot — the two branches meeting at merge time.
  4b. same, for writes made from the SHELL rather than the edit tools: sed -i,
                   redirects, heredocs, tee/cp destinations, and writes inside
                   a heredoc-fed interpreter (`python3 - <<PY … open(p,"w")`).
                   Not an edge case: on a real session nearly every edit took
                   one of these forms, so a tool-only check would have watched
                   the minority path.

Self-gate: only acts when the touched path / cwd / command is Deneb-related, so
the global wiring is a no-op in unrelated projects. Fail-open by design: any
internal error allows the tool.

Check 1b closes the original's known ceiling: Bash commands whose WRITE TARGET
is an absolute/~ path in the prod tree (cp/mv/rm dest, tee, sed -i, >/>>
redirects) now ask — the exact hole the 2026-07-18 stale fix script walked
through (`cp … ~/deneb/scripts/dev/`). Reading prod (cat/grep/ls, cp FROM prod)
stays silent; relative-path writes with cwd inside prod are out of scope.
"""

import json
import os
import re
import shlex
import sys
import time

LIVETEST_LOCK_TTL = 600  # seconds; matches the original 10-minute lock

# How long one session's claim on a file keeps warning other sessions.
#
# Longer than the live-test lock because an editing session spans hours, not
# minutes. The asymmetry decides the number: a false ask costs one keystroke, a
# missed collision costs a merge conflict or lost work in another worktree.
# Tunable so a quiet week can shorten it without a redeploy.
FILE_CLAIM_TTL = int(os.environ.get("DENEB_FILE_CLAIM_TTL", "21600"))  # 6h
# Bounded so one busy session cannot grow the ledger without limit.
FILE_CLAIM_MAX = 400


def prod_root():
    """The production checkout (main-only, auto-deploy owns it)."""
    return os.path.realpath(os.path.expanduser("~/deneb"))


def file_claims_path():
    return os.path.join(os.path.expanduser("~"), ".claude", "deneb-file-claims.json")


def repo_relative(real_path):
    """Path relative to its git checkout root, or None when outside one.

    Keying on the REPO-RELATIVE path is the whole mechanism: two agents editing
    gateway-go/internal/foo.go in two different worktrees are editing the same
    file as far as the eventual merge is concerned, and absolute paths would
    make those two look unrelated.

    Walks up for .git (a directory in the main checkout, a file in a worktree)
    rather than shelling out to git — this runs before every edit.
    """
    directory = os.path.dirname(real_path)
    while True:
        if _is_checkout_root(directory):
            return os.path.relpath(real_path, directory)
        parent = os.path.dirname(directory)
        if parent == directory:
            return None
        directory = parent


def _is_checkout_root(directory):
    """Whether .git here is a REAL checkout marker.

    A bare os.path.exists(".git") is too weak: an empty .git directory is
    enough to satisfy it, and one exists at /tmp/.git on this fleet (created
    2026-08-11, empty). That would make every temp file look like it lived in a
    repo rooted at /tmp and share a key space with real work.

    A checkout has .git/HEAD; a linked worktree has .git as a file holding a
    gitdir: pointer.
    """
    marker = os.path.join(directory, ".git")
    if os.path.isfile(marker):
        return True
    return os.path.isfile(os.path.join(marker, "HEAD"))


def load_claims(now):
    """Read the claim ledger, dropping expired entries."""
    try:
        with open(file_claims_path(), encoding="utf-8") as fh:
            raw = json.load(fh)
    except (OSError, ValueError):
        return {}
    if not isinstance(raw, dict):
        return {}
    return {
        key: value
        for key, value in raw.items()
        if isinstance(value, dict) and now - float(value.get("ts") or 0) < FILE_CLAIM_TTL
    }


def check_file_claim(tool, tool_input, cwd, session_id):
    """Check 6: ask when another live session is editing the same repo file.

    Worktree isolation stops two agents from corrupting each other's checkout;
    it does NOT stop them from editing the same file on two branches and
    discovering it at merge time. That gap is real and routine here — four
    coding harnesses (ZCode, Cursor, Codex, Claude) run against this repo
    concurrently, and CLAUDE.md's multi-agent rules are discipline, not
    enforcement.

    Claims are OBSERVED, not declared. A ticket system can ask for a `touches`
    list up front; an exploratory coding session cannot know what it will edit
    until it edits it. So the first write registers the claim and later writers
    are the ones warned.

    ask, never deny: two agents touching one file is often legitimate (different
    functions, different sections), and only the operator can tell that apart
    from a real collision.
    """
    if tool not in ("Write", "Edit", "MultiEdit"):
        return None
    file_path = (tool_input.get("file_path") or "").strip()
    if not file_path:
        return None
    if not os.path.isabs(file_path):
        file_path = os.path.join(cwd or os.getcwd(), file_path)
    return claim_for_path(os.path.realpath(file_path), cwd, session_id)


def claim_or_warn(rel, cwd, session_id):
    """Register this session's claim on rel, or warn once if another holds it.

    Warning ONCE is the point of the warned list. A held file that re-asked on
    every write would fire twenty times while one function is being edited, and
    a guard that cries that often gets approved reflexively — which is worse
    than not having it. The holder keeps the claim (so a THIRD session is still
    warned); only the asking pair is recorded as settled.
    """
    now = time.time()
    claims = load_claims(now)
    held = claims.get(rel)
    holder = str(held.get("session_id") or "") if held else ""
    if held and holder not in ("", session_id):
        warned = held.get("warned")
        warned = list(warned) if isinstance(warned, list) else []
        if session_id in warned:
            return None  # already told this session about this file
        warned.append(session_id)
        held["warned"] = warned
        save_claims(claims)
        age = int(now - float(held.get("ts") or 0))
        where = str(held.get("cwd") or "")
        return decide("ask", (
            f"다른 세션({str(held.get('session_id'))[:12]}…)이 {_ago(age)} 전 같은 파일을 "
            f"편집했습니다: {rel}"
            + (f"\n  그쪽 작업 위치: {where}" if where else "")
            + "\n  워크트리가 달라 덮어쓰기는 안 나지만, 머지 때 충돌하거나 서로의 수정을 "
              "무효화할 수 있습니다. 같은 파일의 다른 부분이면 계속하세요."
            + f"\n  (클레임 리셋: rm {file_claims_path()})"
        ))

    claims[rel] = {
        "session_id": session_id, "ts": now, "cwd": cwd or "",
        # Carry the warned list across refreshes so a holder's own edits do not
        # reset who has already been told.
        "warned": (held or {}).get("warned") or [],
    }
    save_claims(claims)
    return None


def save_claims(claims):
    """Persist the ledger, bounded. Best-effort: a ledger that cannot be
    written must never block an edit."""
    if len(claims) > FILE_CLAIM_MAX:
        oldest = sorted(claims.items(), key=lambda kv: float(kv[1].get("ts") or 0))
        for key, _ in oldest[: len(claims) - FILE_CLAIM_MAX]:
            claims.pop(key, None)
    try:
        os.makedirs(os.path.dirname(file_claims_path()), exist_ok=True)
        with open(file_claims_path(), "w", encoding="utf-8") as fh:
            json.dump(claims, fh)
    except OSError:
        pass


# Writes performed INSIDE a heredoc-fed interpreter, which the shell parser
# cannot see: `python3 - <<PY` hands the script to stdin, so there is no
# redirect and no write-command argument to find. This is not an exotic case —
# it is how at least one coding agent here performs nearly every edit.
#
# Keyed on the write CALL, not on path-shaped tokens: a read-only `grep
# internal/x.go` must never register a claim, and requiring open(..., "w") is
# what keeps mentions from counting as edits.
INTERPRETER_WRITE_RE = re.compile(
    # open("p", "w"/"a")
    r"""open\(\s*["']([^"']+)["']\s*,\s*["'][wa]"""
    # Path("p").write_text( / .write_bytes(
    r"""|["']([^"']+)["']\s*\)\s*\.write_(?:text|bytes)\(""",
)


def interpreter_write_targets(command):
    """Literal paths a heredoc-fed script writes to."""
    found = []
    for match in INTERPRETER_WRITE_RE.finditer(command):
        target = next((g for g in match.groups() if g), None)
        if target:
            found.append(target)
    return found


def check_bash_file_claim(command, cwd, session_id):
    """Check 4b: the same claim question for files written from the shell.

    Not an edge case — it is the majority path. An agent editing with
    `python3 - <<PY … open(p, "w")`, a heredoc, `sed -i`, or a redirect never
    goes through Write/Edit, so a claim check that only watches those tools
    misses most of what actually gets written. Measured on this very session:
    nearly every edit was a shell heredoc.

    Uses the same target parser as check 1b, so the two cannot disagree about
    what counts as a write.
    """
    for target in bash_write_targets(command) + interpreter_write_targets(command):
        path = os.path.expanduser(target)
        if not os.path.isabs(path):
            path = os.path.join(cwd or os.getcwd(), path)
        result = claim_for_path(os.path.realpath(path), cwd, session_id)
        if result is not None:
            return result
    return None


def claim_for_path(real, cwd, session_id):
    """Shared body of checks 4 and 4b: filter, key, then claim-or-warn."""
    # Hook-local state is not shared work. Matched against ~/.claude
    # specifically, NOT any path containing ".claude" — Claude Code's own
    # worktrees live at <repo>/.claude/worktrees/, so a substring test would
    # exclude the very sessions this check exists for.
    hook_state = os.path.join(os.path.expanduser("~"), ".claude") + os.sep
    if real.startswith(hook_state):
        return None
    # Being inside a git checkout IS the scratch filter: a temp file, a
    # generated artifact, anything outside a repo has no merge to collide at.
    rel = repo_relative(real)
    if rel is None:
        return None
    return claim_or_warn(rel, cwd, session_id)


def _ago(seconds):
    if seconds < 90:
        return f"{seconds}초"
    if seconds < 5400:
        return f"{seconds // 60}분"
    return f"{seconds // 3600}시간"


def livetest_lock_path():
    return os.path.expanduser("~/.claude/deneb-livetest.lock")


def decide(decision, reason):
    """Emit a PreToolUse permission decision (deny/ask) and allow-exit.

    The reason is prefixed so it is never mistaken for a user cancel: the
    harness renders a hook deny with a generic "user declined" wrapper, which
    (2026-07-19) made a correct guard block look like the operator pressed no.
    The marker travels inside permissionDecisionReason AND is mirrored to
    stderr, so whichever stream the harness surfaces, the model sees that a
    guard — not the user — stopped the call, and why.
    """
    marker = (
        "⚠️ Deneb 동시작업 가드가 차단했습니다 (사용자 취소 아님) — "
        if decision == "deny"
        else "❓ Deneb 동시작업 가드가 확인을 요청합니다 (사용자 취소 아님) — "
    )
    full = marker + reason
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": decision,
            "permissionDecisionReason": full,
        }
    }, ensure_ascii=False))
    # Mirror to stderr — surfaced to the model even when the JSON drives the
    # decision, matching how the rules-gate's message reliably shows up.
    print(f"[deneb-concurrency-guard] {full}", file=sys.stderr)
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


def prod_target(token):
    """Realpath when an (expanded) absolute token lands in prod outside worktrees."""
    path = os.path.expanduser(token)
    if not os.path.isabs(path):
        return None
    real = os.path.realpath(path)
    prod = prod_root()
    if real != prod and not real.startswith(prod + os.sep):
        return None
    if real.startswith(os.path.join(prod, ".claude", "worktrees") + os.sep):
        return None
    return real


# Commands where only the last path argument (the destination) mutates vs. ones
# that mutate every path argument they are given.
WRITE_DEST_LAST = {"cp", "install", "rsync"}
WRITE_ANY_ARG = {"rm", "mv", "tee", "touch", "mkdir", "truncate", "ln", "chmod", "chown"}


def bash_write_targets(command):
    """Every path this shell command writes to, across all its segments.

    One parser, two consumers: check 1b (is it the prod tree?) and check 4 (is
    another session holding it?). Written once because a target the two
    disagree about is a hole in whichever check has the narrower list.
    """
    found = []
    for segment in command_segments(command):
        if not segment:
            continue
        name = os.path.basename(segment[0])
        args = [t for t in segment[1:] if not t.startswith("-")]
        targets = []
        if name in WRITE_DEST_LAST:
            if args:
                targets.append(args[-1])
        elif name in WRITE_ANY_ARG:
            targets.extend(args)
        elif name == "sed" and any(t == "-i" or t.startswith("-i") for t in segment[1:]):
            targets.extend(args)
        for i, tok in enumerate(segment):
            bare = tok.lstrip("0123456789&")  # 2>, &>, 2>>… down to the > core
            if bare in (">", ">>"):
                if i + 1 < len(segment):
                    targets.append(segment[i + 1])
            elif bare.startswith(">") and bare.lstrip(">"):
                targets.append(bare.lstrip(">"))
        found.extend(targets)
    return found


def check_prod_write_bash(command):
    """Check 1b: ask on Bash writes into the prod tree (check 1 sees only tool
    file_path). Targets: write-command args, sed -i files, >/>> redirects."""
    for target in bash_write_targets(command):
        hit = prod_target(target)
        if hit:
            return decide("ask", (
                f"이 명령은 프로드 전용 트리에 씁니다: {hit} — main만, auto-deploy가 관리 "
                "(더럽히면 배포 동결·다음 pull 충돌). 개발은 ~/deneb-dev 또는 워크트리에서. "
                "정말 프로드에 써야 하는 부트스트랩이면 계속하세요."
            ))
    return None


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

    result = check_file_claim(tool, tool_input, cwd, session_id)
    if result is not None:
        return result

    if tool == "Bash":
        command = tool_input.get("command") or ""
        if not is_deneb_context(cwd, command):
            return 0
        result = check_prod_write_bash(command)
        if result is not None:
            return result
        result = check_bash_file_claim(command, cwd, session_id)
        if result is not None:
            return result
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

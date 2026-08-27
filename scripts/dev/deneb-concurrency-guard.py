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
                   DENEB_FILE_CLAIM_TTL (6h) → BLOCK ONCE with the full story
                   (who, where, how long ago); the same session's retry passes.
                   Engine (keying, TTL, warn-once, flock) lives in
                   deneb_file_claims.py — one implementation shared with the
                   Cursor and ZCode bridges and the `status` CLI. Keyed by
                   "<origin identity>:<repo-relative path>", so two worktrees
                   (or clones) of one repo collide while different repos never
                   do. Worktree isolation already prevents corruption; this
                   covers the part it cannot — two branches meeting at merge.
  4b. same, for writes made from the SHELL rather than the edit tools: sed -i,
                   redirects, heredocs, tee/cp destinations, and writes inside
                   a heredoc-fed interpreter (`python3 - <<PY … open(p,"w")`).
                   Not an edge case: on a real session nearly every edit took
                   one of these forms, so a tool-only check would have watched
                   the minority path.

Cross-harness: Claude runs this guard natively; Cursor via cursor-hook-bridge
(claim mode, StrReplace→Edit normalized); ZCode via zcode-hook-bridge (claim
mode, deny → exit 2 + stderr — its one feedback channel). Codex has no hook
mechanism and stays uncovered. Live claims: `python3 deneb_file_claims.py status`.

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

# The claims engine lives in its own module so every consumer — this guard, the
# harness bridges, the status CLI — shares one implementation of keying, TTL,
# warn-once, and locking.
sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import deneb_file_claims  # noqa: E402

LIVETEST_LOCK_TTL = 600  # seconds; matches the original 10-minute lock


def prod_root():
    """The production checkout (main-only, auto-deploy owns it)."""
    return os.path.realpath(os.path.expanduser("~/deneb"))


def check_file_claim(tool, tool_input, cwd, session_id):
    """Check 4: block-once when another live session is editing the same repo
    file. Semantics, keying, TTL, and locking live in deneb_file_claims; this
    is only the tool-payload adapter."""
    if tool not in ("Write", "Edit", "MultiEdit"):
        return None
    file_path = (tool_input.get("file_path") or "").strip()
    if not file_path:
        return None
    if not os.path.isabs(file_path):
        file_path = os.path.join(cwd or os.getcwd(), file_path)
    return claim_for_path(os.path.realpath(file_path), cwd, session_id)


def claim_for_path(real, cwd, session_id):
    warning = deneb_file_claims.observe_write(real, cwd, session_id)
    if warning is None:
        return None
    # deny, once: the message tells the agent to simply retry if intentional.
    # This is the one delivery channel every harness shares (Claude
    # permissionDecision, Cursor permission JSON via the bridge, ZCode exit-2
    # via its bridge), and it keeps the decision with the AGENT instead of
    # raising an operator dialog for something only the agent can judge.
    return decide("deny", deneb_file_claims.warning_text(warning))


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
    for target in bash_write_targets(command, cwd, precise=True) + interpreter_write_targets(command):
        path = os.path.expanduser(target)
        if not os.path.isabs(path):
            path = os.path.join(cwd or os.getcwd(), path)
        result = claim_for_path(os.path.realpath(path), cwd, session_id)
        if result is not None:
            return result
    return None


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


def _exists_from(cwd, token):
    path = os.path.expanduser(token)
    if not os.path.isabs(path):
        path = os.path.join(cwd or os.getcwd(), path)
    return os.path.exists(path)


def bash_write_targets(command, cwd=None, precise=False):
    """Paths this shell command writes to. One parser, two consumers, so a
    target they disagree about cannot become a hole in the narrower one.

    precise=False (check 1b, the prod guard): keep every candidate. Over-asking
    about a prod write is safe; missing one is not, so a sed operand that might
    be a path stays in.

    precise=True (check 4, file claims): drop sed operands that do not exist.
    sed -i can only edit existing files, and treating `s/a/b/` as a path filled
    the live ledger with junk. Here a false positive is a spurious BLOCK, so
    precision is what matters.
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
            # args is [expression…, file…] and the two are not distinguishable
            # by shape — `s/a/b/` joined to cwd resolves to a plausible path
            # inside the repo and was polluting the ledger. sed -i can only
            # edit files that already exist, so existence separates them
            # exactly, with no expression-parsing heuristic to get wrong.
            targets.extend(a for a in args if not precise or _exists_from(cwd, a))
        for i, tok in enumerate(segment):
            bare = tok.lstrip("0123456789&")  # 2>, &>, 2>>… down to the > core
            if bare in (">", ">>"):
                if i + 1 < len(segment):
                    targets.append(segment[i + 1])
            elif bare.startswith(">") and bare.lstrip(">"):
                targets.append(bare.lstrip(">"))
        found.extend(t for t in targets if _is_real_write_target(t))
    return found


def _is_real_write_target(token):
    """Whether a parsed token names a FILE this command writes.

    The redirect scan is deliberately loose (it has to catch `2>`, `&>`, `>>`,
    and glued `>file`), and that looseness produced live garbage on 2026-08-27
    — the ledger filled with entries like `&1`, `$SP/q38conc.py`, and `1`:

      - `2>&1` is fd duplication, not a file. It appears in nearly every
        command run here, so two sessions in one directory both claimed `&1`
        and BLOCKED EACH OTHER over a redirect. That is the worst possible
        failure for this feature: a guard that fires on everything gets
        approved reflexively and stops meaning anything.
      - `$SP/x.py` is an unexpanded shell variable. The hook sees the command
        text, not the shell's expansion, so the path is simply unknown — and
        an unknown path must not be guessed at.
      - /dev/null and friends are writes, but nothing merges there.

    Anything that survives is a plain path; the claim layer still filters it
    down to files that live inside a git checkout.
    """
    token = token.strip()
    if not token:
        return False
    if token.startswith("&"):          # 2>&1, >&2 — fd duplication
        return False
    if "$" in token or "`" in token:   # unexpanded expansion; path unknown
        return False
    if any(ch in token for ch in "|<>"):  # metacharacter leaked through
        return False
    if token.startswith("/dev/") or token.startswith("/proc/"):
        return False
    return True


def check_prod_write_bash(command, cwd=None):
    """Check 1b: ask on Bash writes into the prod tree (check 1 sees only tool
    file_path). Targets: write-command args, sed -i files, >/>> redirects."""
    for target in bash_write_targets(command, cwd):
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
        result = check_prod_write_bash(command, cwd)
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

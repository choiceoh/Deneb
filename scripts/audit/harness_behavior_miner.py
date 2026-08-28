"""Cross-harness behavior miner over numbat-normalized coding-agent sessions.

Deneb's fleet runs several coding harnesses against the same repository
(Claude Code, Codex CLI, Cursor). Their on-disk session artifacts are
heterogeneous; the numbat CLI (github.com/perplexityai/numbat, pinned v0.2.0,
record schema 0.3.0) normalizes them into one event vocabulary. This miner
shells out to ``numbat timeline --format json`` per artifact (read-only, no
rules, no hooks), merges events per session, derives deterministic behavioral
metrics (tool mix, verification discipline, retry patterns, scripting habits),
joins Claude Code sessions to the session-memory episodes ledger for a
landed-commit outcome signal, and renders a per-harness contrast report.

Doctrine (same stance as rsi-bench): no LLM scoring — deterministic
aggregation only. The report surfaces contrasts; interpretation happens in
research docs and operator review. The miner reads artifacts and shells out to
numbat, and writes only to the explicit ``--out`` directory.

Fidelity notes baked into the report rather than hidden:
- Cursor transcripts carry no per-event timestamps and no session ids; the
  artifact filename stem is used as the session key and durations are n/a.
- Exit codes are lifted from structured ``exit_code`` when present, else from
  the Codex result preview text ("Process exited with code N"); per-harness
  exit-code coverage is reported so a missing signal reads as unmeasured,
  not as zero failures.
- Command classification is a deterministic shell approximation (operator
  split, wrapper/env-prefix stripping), good for distribution stats only.
"""

from __future__ import annotations

import argparse
import collections
import json
import os
import re
import shutil
import statistics
import subprocess
import sys
from pathlib import Path

NUMBAT_VERSION_PIN = "v0.2.0"
NUMBAT_SCHEMA_EXPECTED = "0.3.0"
NUMBAT_INSTALL_HINT = (
    "install with: GOBIN=~/go/bin go install "
    f"github.com/perplexityai/numbat/cmd/numbat@{NUMBAT_VERSION_PIN}"
)

DEFAULT_AGENTS = ("claude", "codex", "cursor")

# Artifact discovery globs, relative to the home directory. Mirrors the
# numbat agent-coverage matrix for the harnesses this fleet actually runs.
# The coding_dispatch_sessions dir is where the RSI L4 executor
# (scripts/dev/coding_dispatch_executor.py) archives dispatch rollouts out of
# .codex/sessions — the fleet's main Codex volume source. Its nested
# .codex/sessions suffix preserves the vendor layout numbat's path-based
# agent detection requires.
ARTIFACT_GLOBS: dict[str, tuple[str, ...]] = {
    "claude": (".claude/projects/**/*.jsonl",),
    "codex": (
        ".codex/sessions/**/*.jsonl",
        ".codex/sessions/**/*.jsonl.zst",
        ".codex/archived_sessions/*.jsonl",
        ".codex/archived_sessions/*.jsonl.zst",
        ".deneb/data/coding_dispatch_sessions/.codex/sessions/*.jsonl",
    ),
    "cursor": (".cursor/projects/*/agent-transcripts/**/*.jsonl",),
}

DEFAULT_EPISODES = ".claude/deneb-session-memory/episodes.jsonl"

# --- command-string analysis (deterministic approximations) -----------------

_ENV_ASSIGN_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=")
# Prefix words that wrap the real command.
_WRAPPERS = {
    "sudo",
    "nohup",
    "time",
    "exec",
    "env",
    "command",
    "builtin",
    "nice",
    "stdbuf",
    "do",
    "then",
    "else",
}
# Shell control constructs — the segment is flow control, not a command.
_CONTROL = {"for", "while", "if", "until", "case", "esac", "fi", "done", "elif"}

VERIFY_RE = re.compile(
    r"\b(?:make\s+(?:check|ci|test|go(?:-dev)?|python-test|kotlin-models-check)"
    r"|go\s+(?:test|build|vet)"
    r"|pytest"
    r"|python3?\s+-m\s+unittest"
    r"|pnpm\s+(?:test|verify|build)"
    r"|npm\s+test"
    r"|\./gradlew"
    r"|cargo\s+(?:test|check|build)"
    r"|live-test\.sh)\b"
)
COMMIT_RE = re.compile(r"(?:\bgit\s+commit\b|\bcommitter\b|zcode-commit\.sh)")
PUSH_RE = re.compile(r"(?:\bgit\s+push\b|zcode-push\.sh)")
PR_RE = re.compile(r"(?:\bgh\s+pr\b|pr\.sh\s+(?:land|watch|create))")
INLINE_SCRIPT_RE = re.compile(
    r"(?:\b(?:python3?|bash|sh|node)\s+-c\b|<<\s*['\"]?[A-Za-z]{2,}|\bpython3?\s+-\s)"
)
LIMIT_PIPE_RE = re.compile(r"\|\s*(?:head|tail)\b")
EXIT_CODE_TEXT_RE = re.compile(r"exited with code (-?\d+)")
_HEREDOC_RE = re.compile(r"<<-?\s*['\"]?([A-Za-z_][A-Za-z0-9_]*)['\"]?")


def _split_outside_quotes(text: str) -> list[str]:
    """Split on newlines and shell connectors (&&, ||, ;, |) outside quotes."""
    segments: list[str] = []
    buf: list[str] = []
    quote: str | None = None
    i = 0
    while i < len(text):
        ch = text[i]
        if quote:
            buf.append(ch)
            if ch == "\\" and quote == '"' and i + 1 < len(text):
                buf.append(text[i + 1])
                i += 1
            elif ch == quote:
                quote = None
        elif ch in ("'", '"'):
            quote = ch
            buf.append(ch)
        elif ch == "\\" and i + 1 < len(text):
            buf.append(ch)
            buf.append(text[i + 1])
            i += 1
        elif ch == "\n" or ch in (";", "|"):
            segments.append("".join(buf))
            buf = []
            if ch == "|" and i + 1 < len(text) and text[i + 1] == "|":
                i += 1
        elif ch == "&" and i + 1 < len(text) and text[i + 1] == "&":
            segments.append("".join(buf))
            buf = []
            i += 1
        else:
            buf.append(ch)
        i += 1
    segments.append("".join(buf))
    return [seg.strip() for seg in segments if seg.strip()]


def command_segments(command: str) -> list[str]:
    """Split a (possibly multiline) shell command into per-command segments.

    Heredoc bodies and comment lines are dropped, and connector splitting is
    quote-aware — without both, embedded Python/JS blocks (heredocs and
    multiline ``-c "…"`` strings) pollute the argv0 distribution with their
    own keywords (``import``, ``return``, ``}``), which the first corpus run
    surfaced.
    """
    kept: list[str] = []
    terminator: str | None = None
    for line in command.splitlines():
        if terminator is not None:
            if line.strip() == terminator:
                terminator = None
            continue
        if line.strip().startswith("#"):
            continue
        heredoc = _HEREDOC_RE.search(line)
        if heredoc:
            terminator = heredoc.group(1)
        kept.append(line)
    return _split_outside_quotes("\n".join(kept))


def segment_argv0(segment: str) -> str | None:
    """Best-effort program name for one shell segment (basename, unwrapped)."""
    for tok in segment.split():
        if _ENV_ASSIGN_RE.match(tok):
            continue
        base = tok.rsplit("/", 1)[-1].lstrip("({!")
        if not base:
            continue
        if not re.search(r"[A-Za-z0-9]", base):
            return None
        if base in _WRAPPERS:
            continue
        if base in _CONTROL:
            return None
        if base == "cd":
            return None
        return base
    return None


def result_exit_code(event: dict) -> int | None:
    """Exit code from a result event: structured field, else preview text."""
    code = event.get("exit_code")
    if isinstance(code, int):
        return code
    if isinstance(code, str) and code.lstrip("-").isdigit():
        return int(code)
    match = EXIT_CODE_TEXT_RE.search(event.get("content_preview") or "")
    if match:
        return int(match.group(1))
    return None


def _iso_seconds(ts: str | None) -> float | None:
    if not ts:
        return None
    try:
        import datetime as dt

        return dt.datetime.fromisoformat(ts.replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


# --- numbat integration ------------------------------------------------------


def resolve_numbat(explicit: str | None = None) -> str | None:
    """Locate the numbat binary: flag, then $NUMBAT_BIN, PATH, ~/go/bin."""
    candidates = [explicit, os.environ.get("NUMBAT_BIN"), shutil.which("numbat")]
    candidates.append(str(Path.home() / "go" / "bin" / "numbat"))
    for cand in candidates:
        if cand and Path(cand).is_file():
            return cand
    return None


def numbat_schema_note(numbat_bin: str) -> str | None:
    """Warning string when the binary's record schema drifts off the pin."""
    try:
        proc = subprocess.run(
            [numbat_bin, "--version"], capture_output=True, text=True, timeout=30
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return f"numbat --version failed: {exc}"
    banner = (proc.stdout + proc.stderr).strip()
    if NUMBAT_SCHEMA_EXPECTED not in banner:
        return (
            f"numbat schema drift: expected {NUMBAT_SCHEMA_EXPECTED}, got {banner!r}"
            " — metrics may silently change meaning; re-pin before trusting output"
        )
    return None


def run_numbat_timeline(
    numbat_bin: str, artifact: Path
) -> tuple[list[dict], str | None]:
    """Normalize one artifact into numbat session groups (read-only)."""
    cmd = [numbat_bin, "timeline", "--path", str(artifact), "--format", "json"]
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=300)
    except (OSError, subprocess.TimeoutExpired) as exc:
        return [], f"{artifact}: {exc}"
    if proc.returncode != 0:
        tail = proc.stderr.strip().splitlines()[-1:] or ["(no stderr)"]
        return [], f"{artifact}: exit {proc.returncode}: {tail[0][:200]}"
    try:
        doc = json.loads(proc.stdout or "{}")
    except json.JSONDecodeError as exc:
        return [], f"{artifact}: bad JSON: {exc}"
    sessions = doc.get("sessions")
    if not isinstance(sessions, list):
        return [], f"{artifact}: no sessions array"
    return sessions, None


# --- discovery / merge -------------------------------------------------------


def discover_artifacts(
    home: Path,
    agents: tuple[str, ...] = DEFAULT_AGENTS,
    since_days: int = 0,
    now_s: float | None = None,
) -> dict[str, list[Path]]:
    """Sorted artifact paths per agent; optional mtime cutoff in days."""
    cutoff = None
    if since_days > 0:
        base = now_s if now_s is not None else __import__("time").time()
        cutoff = base - since_days * 86400
    found: dict[str, list[Path]] = {}
    for agent in agents:
        paths: set[Path] = set()
        for pattern in ARTIFACT_GLOBS.get(agent, ()):
            for path in home.glob(pattern):
                if not path.is_file():
                    continue
                # Workflow journals sit next to Claude Code subagent
                # transcripts but are not session artifacts.
                if path.name == "journal.jsonl":
                    continue
                if cutoff is not None and path.stat().st_mtime < cutoff:
                    continue
                paths.add(path)
        found[agent] = sorted(paths)
    return found


def _session_key(group: dict, artifact: Path) -> str:
    sid = group.get("session_id")
    if sid:
        return str(sid)
    stem = artifact.name
    for suffix in (".jsonl.zst", ".jsonl"):
        if stem.endswith(suffix):
            stem = stem[: -len(suffix)]
            break
    return stem


def merge_sessions(
    per_artifact: list[tuple[str, Path, list[dict]]],
) -> list[dict]:
    """Merge numbat session groups into one record per (agent, session key).

    Claude Code subagent transcripts live in sibling artifacts but share the
    parent session_id, so merging reunites a session with its subagents.
    Events are ordered by timestamp when present, keeping artifact order as a
    stable tiebreak (Cursor has no timestamps at all).
    """
    merged: dict[tuple[str, str], dict] = {}
    seq = 0
    for agent, artifact, groups in per_artifact:
        for group in groups:
            key = (agent, _session_key(group, artifact))
            rec = merged.setdefault(
                key,
                {
                    "agent": agent,
                    "session_id": key[1],
                    "artifacts": [],
                    "events": [],
                    "start": None,
                    "end": None,
                    "project_path": group.get("project_path"),
                },
            )
            rec["artifacts"].append(str(artifact))
            for ev in group.get("events", []):
                rec["events"].append((ev.get("timestamp") or "", seq, ev))
                seq += 1
            for bound, pick in (("start", min), ("end", max)):
                val = group.get(bound)
                if val:
                    candidates = [v for v in (rec[bound], val) if v]
                    rec[bound] = pick(candidates)
    out = []
    for key in sorted(merged):
        rec = merged[key]
        rec["events"] = [
            ev for _, _, ev in sorted(rec["events"], key=lambda t: (t[0], t[1]))
        ]
        rec["artifacts"] = sorted(set(rec["artifacts"]))
        out.append(rec)
    return out


# --- metrics -----------------------------------------------------------------


def session_metrics(record: dict, episode: dict | None = None) -> dict:
    """Deterministic behavioral metrics for one merged session record."""
    events = record["events"]
    counts = collections.Counter(ev.get("event_type", "?") for ev in events)
    argv0: collections.Counter = collections.Counter()
    execs: list[str] = []
    verify_idx: list[int] = []
    write_idx: list[int] = []
    n_commit = n_push = n_pr = n_inline = n_limit = n_verify = 0
    retries = 0
    prev_cmd = None
    results_with_code = 0
    results_fail = 0
    timestamps: list[float] = []

    for idx, ev in enumerate(events):
        ts = _iso_seconds(ev.get("timestamp"))
        if ts is not None:
            timestamps.append(ts)
        etype = ev.get("event_type")
        if etype == "command.exec":
            cmd = ev.get("command") or ""
            execs.append(cmd)
            for seg in command_segments(cmd):
                name = segment_argv0(seg)
                if name:
                    argv0[name] += 1
            if VERIFY_RE.search(cmd):
                n_verify += 1
                verify_idx.append(idx)
            if COMMIT_RE.search(cmd):
                n_commit += 1
            if PUSH_RE.search(cmd):
                n_push += 1
            if PR_RE.search(cmd):
                n_pr += 1
            if INLINE_SCRIPT_RE.search(cmd):
                n_inline += 1
            if LIMIT_PIPE_RE.search(cmd):
                n_limit += 1
            stripped = cmd.strip()
            if prev_cmd is not None and stripped == prev_cmd:
                retries += 1
            prev_cmd = stripped
        elif etype == "file.write":
            write_idx.append(idx)
        elif etype in ("command.result", "tool.result"):
            code = result_exit_code(ev)
            if code is not None:
                results_with_code += 1
                if code != 0:
                    results_fail += 1

    duration_s = None
    start_s = _iso_seconds(record.get("start"))
    end_s = _iso_seconds(record.get("end"))
    if start_s is not None and end_s is not None and end_s >= start_s:
        duration_s = end_s - start_s
    elif len(timestamps) >= 2:
        duration_s = max(timestamps) - min(timestamps)

    subagents = {ev.get("sub_agent") for ev in events if ev.get("sub_agent")}
    verify_after_last_write = bool(write_idx) and any(
        v > max(write_idx) for v in verify_idx
    )

    metrics = {
        "agent": record["agent"],
        "session_id": record["session_id"],
        "n_artifacts": len(record["artifacts"]),
        "project_path": record.get("project_path"),
        "events": len(events),
        "event_counts": dict(sorted(counts.items())),
        "user_prompts": counts.get("prompt.user", 0),
        "assistant_msgs": counts.get("message.assistant", 0),
        "execs": len(execs),
        "file_reads": counts.get("file.read", 0),
        "file_writes": counts.get("file.write", 0) + counts.get("file.delete", 0),
        "tool_calls": counts.get("tool.call", 0),
        "verify_cmds": n_verify,
        "verify_after_last_write": verify_after_last_write,
        "commit_cmds": n_commit,
        "push_cmds": n_push,
        "pr_cmds": n_pr,
        "inline_scripts": n_inline,
        "limit_pipes": n_limit,
        "retries": retries,
        "results_with_code": results_with_code,
        "results_fail": results_fail,
        "subagents": len(subagents),
        "duration_s": round(duration_s, 1) if duration_s is not None else None,
        "top_cmds": dict(argv0.most_common(12)),
    }
    if episode is not None:
        commits = episode.get("commits")
        metrics["episode_commits"] = len(commits) if isinstance(commits, list) else 0
        metrics["episode_branch"] = episode.get("branch")
    return metrics


def load_episodes(path: Path) -> dict[str, dict]:
    """Episodes ledger indexed by session_id (last write wins)."""
    episodes: dict[str, dict] = {}
    if not path.is_file():
        return episodes
    with path.open(encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError:
                continue
            sid = rec.get("session_id")
            if sid:
                episodes[str(sid)] = rec
    return episodes


# --- aggregation -------------------------------------------------------------


def _median(values: list[float]) -> float | None:
    return round(statistics.median(values), 1) if values else None


def _pct(num: int, den: int) -> float | None:
    return round(100.0 * num / den, 1) if den else None


def aggregate(metrics: list[dict]) -> dict:
    """Per-harness aggregate profile from per-session metrics."""
    if not metrics:
        return {"sessions": 0}
    n = len(metrics)
    sum_execs = sum(m["execs"] for m in metrics)
    sum_writes = sum(m["file_writes"] for m in metrics)
    with_write = [m for m in metrics if m["file_writes"] > 0]
    with_code = sum(m["results_with_code"] for m in metrics)
    durations = [m["duration_s"] for m in metrics if m["duration_s"] is not None]
    top: collections.Counter = collections.Counter()
    for m in metrics:
        top.update(m["top_cmds"])
    total_cmds = sum(top.values())
    return {
        "sessions": n,
        "events": sum(m["events"] for m in metrics),
        "med_events": _median([m["events"] for m in metrics]),
        "med_execs": _median([m["execs"] for m in metrics]),
        "med_user_prompts": _median([m["user_prompts"] for m in metrics]),
        "med_duration_min": _median([d / 60 for d in durations]),
        "duration_coverage": _pct(len(durations), n),
        "pct_verify": _pct(sum(1 for m in metrics if m["verify_cmds"] > 0), n),
        "pct_verify_after_last_write": _pct(
            sum(1 for m in with_write if m["verify_after_last_write"]),
            len(with_write),
        ),
        "pct_commit": _pct(sum(1 for m in metrics if m["commit_cmds"] > 0), n),
        "pct_push": _pct(sum(1 for m in metrics if m["push_cmds"] > 0), n),
        "pct_subagent": _pct(sum(1 for m in metrics if m["subagents"] > 0), n),
        "reads_per_write": (
            round(sum(m["file_reads"] for m in metrics) / sum_writes, 2)
            if sum_writes
            else None
        ),
        "inline_per_100": _pct(sum(m["inline_scripts"] for m in metrics), sum_execs),
        "limit_per_100": _pct(sum(m["limit_pipes"] for m in metrics), sum_execs),
        "retry_per_100": _pct(sum(m["retries"] for m in metrics), sum_execs),
        "exit_coverage": _pct(with_code, sum_execs),
        "fail_rate": _pct(sum(m["results_fail"] for m in metrics), with_code),
        "top_cmds": (
            [
                (name, count, round(100.0 * count / total_cmds, 1))
                for name, count in top.most_common(15)
            ]
            if total_cmds
            else []
        ),
    }


def outcome_contrast(metrics: list[dict]) -> dict:
    """Claude Code committed-vs-not contrast via the episodes ledger join."""
    joined = [m for m in metrics if m["agent"] == "claude" and "episode_commits" in m]
    committed = [m for m in joined if m["episode_commits"] > 0]
    uncommitted = [m for m in joined if m["episode_commits"] == 0]
    return {
        "joined": len(joined),
        "unjoined": sum(1 for m in metrics if m["agent"] == "claude") - len(joined),
        "committed": aggregate(committed),
        "uncommitted": aggregate(uncommitted),
    }


# --- report ------------------------------------------------------------------

PROFILE_ROWS = (
    ("sessions with a verify/build cmd %", "pct_verify"),
    ("verify after last write % (of writing sessions)", "pct_verify_after_last_write"),
    ("sessions with commit cmd %", "pct_commit"),
    ("sessions with push cmd %", "pct_push"),
    ("sessions using subagents %", "pct_subagent"),
    ("file reads per write", "reads_per_write"),
    ("inline scripts per 100 execs", "inline_per_100"),
    ("head/tail limit pipes per 100 execs", "limit_per_100"),
    ("identical-retry per 100 execs", "retry_per_100"),
    ("exit-code coverage % of execs", "exit_coverage"),
    ("nonzero-exit rate % (where covered)", "fail_rate"),
)


def _fmt(value: object) -> str:
    return "n/a" if value is None else str(value)


def render_report(
    aggs: dict[str, dict],
    contrast: dict,
    corpus: dict[str, dict],
    generated_at: str | None = None,
    notes: list[str] | None = None,
) -> str:
    """Deterministic markdown contrast report (aggregation only, no scoring)."""
    lines: list[str] = ["# Cross-harness behavior mining report", ""]
    if generated_at:
        lines += [
            f"Generated: {generated_at} — deterministic aggregation, no LLM scoring.",
            "",
        ]
    lines += [
        "## Corpus",
        "",
        "| harness | artifacts | parse failures | sessions | events |",
        "|---|---|---|---|---|",
    ]
    for agent in sorted(corpus):
        c = corpus[agent]
        agg = aggs.get(agent, {})
        lines.append(
            f"| {agent} | {c['artifacts']} | {c['failures']} | "
            f"{agg.get('sessions', 0)} | {agg.get('events', 0)} |"
        )
    lines += ["", "## Session shape (medians)", ""]
    lines += [
        "| harness | events | execs | user prompts | duration (min) | duration coverage % |",
        "|---|---|---|---|---|---|",
    ]
    agents_with_data = [a for a in sorted(aggs) if aggs[a].get("sessions")]
    for agent in agents_with_data:
        a = aggs[agent]
        lines.append(
            f"| {agent} | {_fmt(a['med_events'])} | {_fmt(a['med_execs'])} | "
            f"{_fmt(a['med_user_prompts'])} | {_fmt(a['med_duration_min'])} | "
            f"{_fmt(a['duration_coverage'])} |"
        )
    lines += ["", "## Behavior profile", ""]
    lines.append("| metric | " + " | ".join(agents_with_data) + " |")
    lines.append("|---|" + "---|" * len(agents_with_data))
    for label, key in PROFILE_ROWS:
        row = [_fmt(aggs[a].get(key)) for a in agents_with_data]
        lines.append(f"| {label} | " + " | ".join(row) + " |")
    lines += ["", "## Top commands per harness", ""]
    for agent in agents_with_data:
        tops = aggs[agent].get("top_cmds") or []
        rendered = ", ".join(
            f"{name} ×{count} ({share}%)" for name, count, share in tops
        )
        lines.append(f"- **{agent}**: {rendered or 'n/a'}")
    lines += ["", "## Claude Code: landed-commit contrast (episodes ledger join)", ""]
    lines.append(
        f"Joined sessions: {contrast['joined']} (no episode record: {contrast['unjoined']})"
    )
    committed, uncommitted = contrast["committed"], contrast["uncommitted"]
    if committed.get("sessions") and uncommitted.get("sessions"):
        lines += [
            "",
            "| metric | committed | no-commit |",
            "|---|---|---|",
            f"| sessions | {committed['sessions']} | {uncommitted['sessions']} |",
        ]
        for label, key in PROFILE_ROWS:
            lines.append(
                f"| {label} | {_fmt(committed.get(key))} | {_fmt(uncommitted.get(key))} |"
            )
        for label, key in (
            ("median execs", "med_execs"),
            ("median events", "med_events"),
            ("median user prompts", "med_user_prompts"),
            ("median duration (min)", "med_duration_min"),
        ):
            lines.append(
                f"| {label} | {_fmt(committed.get(key))} | {_fmt(uncommitted.get(key))} |"
            )
    else:
        lines.append("Not enough joined sessions on both sides for a contrast.")
    lines += ["", "## Caveats", ""]
    default_notes = [
        "Cursor: no per-event timestamps or session ids — artifact-stem session keys; durations n/a.",
        "Exit codes come from structured fields or Codex preview text; read the exit-code coverage row before the fail-rate row.",
        "Command classification is a deterministic shell approximation (split on operators, wrappers stripped).",
        "Parse failures above are dropped artifacts, not silent truncation.",
    ]
    for note in default_notes + (notes or []):
        lines.append(f"- {note}")
    lines.append("")
    return "\n".join(lines)


# --- orchestration -----------------------------------------------------------


def mine(
    home: Path,
    agents: tuple[str, ...],
    numbat_bin: str,
    episodes_path: Path | None = None,
    since_days: int = 0,
    max_artifacts: int = 0,
    runner=run_numbat_timeline,
    log=lambda msg: print(msg, file=sys.stderr),
) -> dict:
    """End-to-end mining pass; returns metrics, aggregates, contrast, report data."""
    discovered = discover_artifacts(home, agents, since_days=since_days)
    episodes = load_episodes(episodes_path) if episodes_path else {}
    per_artifact: list[tuple[str, Path, list[dict]]] = []
    corpus: dict[str, dict] = {}
    failures: list[str] = []
    for agent in agents:
        paths = discovered.get(agent, [])
        if max_artifacts > 0:
            paths = paths[:max_artifacts]
        n_fail = 0
        for i, path in enumerate(paths):
            groups, err = runner(numbat_bin, path)
            if err:
                n_fail += 1
                failures.append(err)
                continue
            per_artifact.append((agent, path, groups))
            if (i + 1) % 50 == 0:
                log(f"[{agent}] {i + 1}/{len(paths)} artifacts")
        corpus[agent] = {"artifacts": len(paths), "failures": n_fail}
    records = merge_sessions(per_artifact)
    metrics = [
        session_metrics(rec, episodes.get(rec["session_id"])) for rec in records
    ]
    aggs = {
        agent: aggregate([m for m in metrics if m["agent"] == agent])
        for agent in agents
    }
    contrast = outcome_contrast(metrics)
    return {
        "metrics": metrics,
        "aggregates": aggs,
        "contrast": contrast,
        "corpus": corpus,
        "failures": failures,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Mine cross-harness coding-agent behavior via numbat timeline."
    )
    parser.add_argument(
        "--out", required=True, help="output directory (report.md, sessions.jsonl)"
    )
    parser.add_argument(
        "--agent", action="append", choices=DEFAULT_AGENTS, help="repeatable; default: all"
    )
    parser.add_argument(
        "--numbat-bin",
        default=None,
        help="numbat binary (default: $NUMBAT_BIN, PATH, ~/go/bin)",
    )
    parser.add_argument("--home", default=None, help="home dir override (tests)")
    parser.add_argument(
        "--episodes",
        default=None,
        help="episodes ledger path (default: ~/" + DEFAULT_EPISODES + ")",
    )
    parser.add_argument(
        "--since-days",
        type=int,
        default=0,
        help="only artifacts modified in the last N days (0=all)",
    )
    parser.add_argument(
        "--max-artifacts", type=int, default=0, help="cap artifacts per agent (0=all)"
    )
    args = parser.parse_args(argv)

    home = Path(args.home).expanduser() if args.home else Path.home()
    agents = tuple(args.agent) if args.agent else DEFAULT_AGENTS
    numbat_bin = resolve_numbat(args.numbat_bin)
    if not numbat_bin:
        print(f"numbat binary not found — {NUMBAT_INSTALL_HINT}", file=sys.stderr)
        return 2
    notes: list[str] = []
    schema_note = numbat_schema_note(numbat_bin)
    if schema_note:
        print(f"warning: {schema_note}", file=sys.stderr)
        notes.append(schema_note)

    episodes_path = (
        Path(args.episodes).expanduser() if args.episodes else home / DEFAULT_EPISODES
    )
    result = mine(
        home,
        agents,
        numbat_bin,
        episodes_path=episodes_path,
        since_days=args.since_days,
        max_artifacts=args.max_artifacts,
    )

    import datetime as dt

    generated_at = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    report = render_report(
        result["aggregates"],
        result["contrast"],
        result["corpus"],
        generated_at=generated_at,
        notes=notes,
    )
    out_dir = Path(args.out).expanduser()
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "report.md").write_text(report, encoding="utf-8")
    with (out_dir / "sessions.jsonl").open("w", encoding="utf-8") as fh:
        for m in sorted(result["metrics"], key=lambda m: (m["agent"], m["session_id"])):
            fh.write(json.dumps(m, ensure_ascii=False, sort_keys=True) + "\n")
    if result["failures"]:
        (out_dir / "parse-failures.txt").write_text(
            "\n".join(result["failures"]) + "\n", encoding="utf-8"
        )
    print(f"report: {out_dir / 'report.md'}")
    print(f"sessions: {out_dir / 'sessions.jsonl'} ({len(result['metrics'])} sessions)")
    if result["failures"]:
        print(f"parse failures: {len(result['failures'])} (see parse-failures.txt)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Deterministic CodeGraph blast-radius brief for a branch's diff.

Renders "what this change can break" from the symbol graph, so a reviewer (human
or agent) sees the dependency reach the diff itself does not show: a one-line
edit inside a widely-called helper and a one-line edit inside a leaf look
identical in `git diff`.

Pipeline, all deterministic, no LLM:

1. ``git diff --name-only <range>``      → changed source files
2. ``git diff -U0 <range> -- <file>``    → changed line numbers (new side)
3. ``codegraph node --file F --symbols-only`` → the file's symbol map
4. changed line → the symbol whose definition starts at or above it
5. ``codegraph impact SYM --json``       → affected symbols, depth 2

The report separates affected symbols that are already IN the diff from those
OUTSIDE it. The outside set is the review-relevant half — callers this change
reaches but does not touch.

Honesty rule: every degradation is stated, never silently dropped. No codegraph
binary, no index, a symbol the index does not know — each renders as an explicit
line in the brief. A brief that cannot see the graph must not read like one that
did.

Usage:
    python3 scripts/dev/impact_brief.py [--repo DIR] [--range origin/main...HEAD]
    python3 scripts/dev/impact_brief.py --format json
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

# Marker pair so an existing brief in a PR body is REPLACED, not duplicated,
# when the branch is re-pushed and the attach step runs again.
MARK_START = "<!-- deneb:impact-brief:start -->"
MARK_END = "<!-- deneb:impact-brief:end -->"

# Languages the CodeGraph index parses. A changed file outside this set (docs,
# YAML, JSON fixtures) has no symbols and is skipped without comment.
SOURCE_SUFFIXES = frozenset(
    {
        ".go", ".kt", ".kts", ".java", ".swift",
        ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
        ".py", ".rs", ".c", ".h", ".cc", ".cpp", ".hpp", ".m", ".mm",
    }
)

# Paths whose symbols are noise in a review brief: vendored trees and generated
# output (the source of truth is regenerated, not reviewed line by line).
SKIP_PATH_PARTS = ("node_modules/", "/vendor/", "vendor/", "/build/", "/.gradle/")

# A test file's symbols are a different kind of blast radius than production
# ones: "these tests cover it" versus "this reaches code you did not touch".
# Mixing them buries the second signal under the first.
TEST_PATH_MARKERS = ("_test.", ".test.", ".spec.", "/test/", "/tests/", "Test.kt", "Tests.kt")

DEFAULT_RANGE = "origin/main...HEAD"
DEFAULT_MAX_SYMBOLS = 12
DEFAULT_MAX_AFFECTED = 8
DEFAULT_DEPTH = 2
# codegraph reads a 300MB SQLite index; a cold call is seconds, not minutes.
# The cap keeps a wedged binary from stalling a PR landing.
CALL_TIMEOUT_S = 90

# `- `Name` (kind) = value — :123`  — the trailing ` — :<line>` is the anchor;
# the value half may itself contain dashes and colons, so split from the right.
_SYMBOL_LINE = re.compile(r"^- `([^`]+)` \(([^)]+)\)(?P<rest>.*) — :(\d+)\s*$")
# `@@ -a,b +c,d @@` — only the new side matters (deleted lines have no symbol
# to analyze in the post-change tree).
_HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")


def is_test_path(path: str) -> bool:
    return any(marker in path for marker in TEST_PATH_MARKERS)


@dataclass
class SymbolImpact:
    """One changed symbol and the symbols a change to it reaches."""

    name: str
    kind: str
    file: str
    line: int
    affected_inside: list[str] = field(default_factory=list)
    affected_outside: list[dict] = field(default_factory=list)
    affected_tests: list[str] = field(default_factory=list)
    total_affected: int = 0
    note: str = ""

    def to_dict(self) -> dict:
        return {
            "symbol": self.name,
            "kind": self.kind,
            "file": self.file,
            "line": self.line,
            "totalAffected": self.total_affected,
            "affectedInsideDiff": self.affected_inside,
            "affectedOutsideDiff": self.affected_outside,
            "affectedTests": self.affected_tests,
            "note": self.note,
        }


@dataclass
class Brief:
    range: str
    changed_files: list[str]
    analyzed_files: list[str]
    symbols: list[SymbolImpact]
    degradations: list[str]
    truncated_symbols: int = 0

    @property
    def measured(self) -> bool:
        return bool(self.symbols)

    def to_dict(self) -> dict:
        return {
            "range": self.range,
            "changedFiles": self.changed_files,
            "analyzedFiles": self.analyzed_files,
            "symbols": [s.to_dict() for s in self.symbols],
            "degradations": self.degradations,
            "truncatedSymbols": self.truncated_symbols,
        }


def _run(cmd: list[str], cwd: Path) -> tuple[int, str]:
    try:
        proc = subprocess.run(
            cmd,
            cwd=str(cwd),
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=CALL_TIMEOUT_S,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return 1, f"{type(exc).__name__}: {exc}"
    return proc.returncode, proc.stdout


def changed_source_files(repo: Path, rev_range: str) -> tuple[list[str], list[str]]:
    """(all changed files, source files worth analyzing) for the range."""
    rc, out = _run(["git", "diff", "--name-only", rev_range], repo)
    if rc != 0:
        return [], []
    changed = [ln.strip() for ln in out.splitlines() if ln.strip()]
    source = [
        f
        for f in changed
        if Path(f).suffix in SOURCE_SUFFIXES
        and not any(part in f for part in SKIP_PATH_PARTS)
        and (repo / f).is_file()
    ]
    return changed, source


def changed_lines(repo: Path, rev_range: str, path: str) -> list[int]:
    """New-side line numbers touched in `path`, from a zero-context diff."""
    rc, out = _run(["git", "diff", "-U0", rev_range, "--", path], repo)
    if rc != 0:
        return []
    lines: list[int] = []
    for raw in out.splitlines():
        m = _HUNK.match(raw)
        if not m:
            continue
        start = int(m.group(1))
        count = int(m.group(2)) if m.group(2) is not None else 1
        # count==0 marks a pure deletion; anchor it to the surrounding symbol
        # by probing the line the deletion sat before.
        lines.extend(range(start, start + count) if count else [max(start, 1)])
    return lines


def file_symbols(codegraph: str, repo: Path, path: str) -> list[tuple[int, str, str]]:
    """(start_line, name, kind) for a file, ascending. Empty when unindexed."""
    rc, out = _run([codegraph, "node", "--file", path, "--symbols-only", "-p", str(repo)], repo)
    if rc != 0:
        return []
    found: list[tuple[int, str, str]] = []
    for raw in out.splitlines():
        m = _SYMBOL_LINE.match(raw.strip())
        if m:
            found.append((int(m.group(4)), m.group(1), m.group(2)))
    found.sort()
    return found


def symbols_for_lines(
    symbols: list[tuple[int, str, str]], lines: list[int]
) -> list[tuple[str, str, int]]:
    """Map changed lines onto the enclosing definitions.

    A symbol map carries start lines only, so a definition is treated as
    spanning up to the next definition's start. That over-attributes a line in
    the gap between two top-level declarations to the earlier one — acceptable
    for a review brief, and it never invents a symbol the file does not have.
    """
    if not symbols:
        return []
    picked: dict[str, tuple[str, str, int]] = {}
    for line in lines:
        chosen = None
        for start, name, kind in symbols:
            if start <= line:
                chosen = (name, kind, start)
            else:
                break
        if chosen and chosen[0] not in picked:
            picked[chosen[0]] = chosen
    return sorted(picked.values(), key=lambda t: t[2])


def symbol_impact(
    codegraph: str, repo: Path, name: str, kind: str, path: str, line: int, depth: int
) -> SymbolImpact:
    out = SymbolImpact(name=name, kind=kind, file=path, line=line)
    rc, raw = _run(
        [codegraph, "impact", name, "--json", "-d", str(depth), "-p", str(repo)], repo
    )
    if rc != 0 or not raw.strip():
        out.note = "impact 조회 실패 (인덱스에 없거나 이름 모호)"
        return out
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        out.note = "impact 출력 파싱 실패"
        return out
    affected = data.get("affected")
    if not isinstance(affected, list):
        out.note = "impact 출력에 affected 없음"
        return out
    for item in affected:
        if not isinstance(item, dict):
            continue
        a_name = str(item.get("name") or "")
        a_file = str(item.get("filePath") or "")
        if not a_name or a_name == name:
            continue
        out.total_affected += 1
        if is_test_path(a_file):
            out.affected_tests.append(a_name)
        elif a_file == path:
            out.affected_inside.append(a_name)
        else:
            out.affected_outside.append({"name": a_name, "file": a_file})
    return out


def sync_index(codegraph: str, repo: Path) -> str:
    """Bring the index up to the working tree. Returns a degradation note or ""..

    A stale index does not fail loudly — it silently attributes changed lines to
    whatever symbol sat there before the edit, producing a brief that looks
    authoritative and points at the wrong code. Syncing first is what makes the
    line→symbol mapping trustworthy; it is an incremental AST pass, seconds.
    """
    rc, _ = _run([codegraph, "sync", str(repo)], repo)
    return "" if rc == 0 else "codegraph sync 실패 — 인덱스가 최신이 아닐 수 있다 (심볼 귀속 주의)"


def build_brief(
    repo: Path,
    rev_range: str,
    *,
    codegraph: str | None = None,
    max_symbols: int = DEFAULT_MAX_SYMBOLS,
    depth: int = DEFAULT_DEPTH,
    sync: bool = True,
) -> Brief:
    degradations: list[str] = []
    changed, source = changed_source_files(repo, rev_range)
    if not changed:
        degradations.append(f"`{rev_range}` 범위에 변경 파일이 없다")
        return Brief(rev_range, [], [], [], degradations)

    binary = codegraph or shutil.which("codegraph")
    if not binary:
        degradations.append("codegraph 바이너리 없음 — 파급 범위 미확인")
        return Brief(rev_range, changed, [], [], degradations)
    if not (repo / ".codegraph" / "codegraph.db").is_file():
        degradations.append(f"`{repo}`에 CodeGraph 인덱스 없음 — 파급 범위 미확인")
        return Brief(rev_range, changed, [], [], degradations)
    if not source:
        degradations.append("변경 파일 중 인덱싱 대상 소스가 없다 (문서·설정만)")
        return Brief(rev_range, changed, [], [], degradations)
    if sync:
        if note := sync_index(binary, repo):
            degradations.append(note)

    candidates: list[tuple[str, str, str, int]] = []
    analyzed: list[str] = []
    for path in source:
        symbols = file_symbols(binary, repo, path)
        if not symbols:
            degradations.append(f"`{path}` 심볼 맵 없음 (신규 파일이거나 미인덱싱)")
            continue
        analyzed.append(path)
        for name, kind, start in symbols_for_lines(symbols, changed_lines(repo, rev_range, path)):
            candidates.append((name, kind, path, start))

    truncated = max(0, len(candidates) - max_symbols)
    impacts = [
        symbol_impact(binary, repo, name, kind, path, line, depth)
        for name, kind, path, line in candidates[:max_symbols]
    ]
    # Loudest blast radius first — that is what a reviewer should read first.
    impacts.sort(key=lambda s: (-len(s.affected_outside), -s.total_affected, s.name))
    return Brief(rev_range, changed, analyzed, impacts, degradations, truncated)


def render_markdown(brief: Brief, *, max_affected: int = DEFAULT_MAX_AFFECTED) -> str:
    out = [MARK_START, "## 변경 파급 범위 (CodeGraph)", ""]
    out.append(
        f"`{brief.range}` · 변경 파일 {len(brief.changed_files)}개 "
        f"(분석 {len(brief.analyzed_files)}개) · 편집 심볼 {len(brief.symbols)}개"
    )
    out.append("")
    if brief.symbols:
        out.append("| 편집한 심볼 | 위치 | diff 밖 프로덕션 심볼 | 테스트 |")
        out.append("|---|---|---|---:|")
        for s in brief.symbols:
            outside = [f"`{a['name']}`" for a in s.affected_outside[:max_affected]]
            rest = len(s.affected_outside) - len(outside)
            if rest > 0:
                outside.append(f"외 {rest}개")
            cell = ", ".join(outside) if outside else "—"
            if s.note:
                cell = s.note
            out.append(
                f"| `{s.name}` ({s.kind}) | `{s.file}:{s.line}` | {cell} | {len(s.affected_tests)} |"
            )
        out.append("")
        widest = brief.symbols[0]
        if widest.affected_outside:
            out.append(
                f"가장 넓은 파급: `{widest.name}` — 이 diff가 건드리지 않은 프로덕션 심볼 "
                f"{len(widest.affected_outside)}개에 닿는다."
            )
        uncovered = [s.name for s in brief.symbols if not s.affected_tests and not s.note]
        if uncovered:
            out.append(
                "테스트가 닿지 않는 편집 심볼: "
                + ", ".join(f"`{n}`" for n in uncovered[:max_affected])
                + (f" 외 {len(uncovered) - max_affected}개" if len(uncovered) > max_affected else "")
            )
        out.append("")
    if brief.truncated_symbols:
        out.append(f"편집 심볼 {brief.truncated_symbols}개는 상한을 넘어 생략됐다.")
        out.append("")
    for note in brief.degradations:
        out.append(f"- ⚠️ {note}")
    if brief.degradations:
        out.append("")
    out.append("<sub>`scripts/dev/impact_brief.py` 자동 생성 — 결정적 그래프 조회, LLM 판단 아님.</sub>")
    out.append(MARK_END)
    return "\n".join(out)


def splice_into_body(body: str, brief_md: str) -> str:
    """Replace an existing brief block in a PR body, or append a new one."""
    start = body.find(MARK_START)
    end = body.find(MARK_END)
    if start != -1 and end != -1 and end > start:
        return body[:start] + brief_md + body[end + len(MARK_END) :]
    separator = "\n\n" if body.strip() else ""
    return body.rstrip() + separator + brief_md + "\n"


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--repo", default=".", help="repository root (default: cwd)")
    ap.add_argument("--range", dest="rev_range", default=DEFAULT_RANGE)
    ap.add_argument("--max-symbols", type=int, default=DEFAULT_MAX_SYMBOLS)
    ap.add_argument("--max-affected", type=int, default=DEFAULT_MAX_AFFECTED)
    ap.add_argument("--depth", type=int, default=DEFAULT_DEPTH)
    ap.add_argument("--format", choices=("markdown", "json"), default="markdown")
    ap.add_argument(
        "--no-sync",
        action="store_true",
        help="skip the pre-analysis `codegraph sync` (faster; risks stale attribution)",
    )
    args = ap.parse_args(argv)

    repo = Path(args.repo).resolve()
    if not (repo / ".git").exists():
        print(f"{repo} is not a git repository", file=sys.stderr)
        return 2
    brief = build_brief(
        repo,
        args.rev_range,
        max_symbols=args.max_symbols,
        depth=args.depth,
        sync=not args.no_sync,
    )
    if args.format == "json":
        print(json.dumps(brief.to_dict(), ensure_ascii=False, indent=2))
    else:
        print(render_markdown(brief, max_affected=args.max_affected))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

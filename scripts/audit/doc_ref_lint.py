#!/usr/bin/env python3
"""doc-ref-lint — validate-or-freeze for doc-embedded code references.

Adapted from the Harness Handbook paper (arXiv:2607.13285): every *active*
reference in agent-facing docs must resolve against the current repository, or
be surfaced as stale ("frozen") instead of silently misleading the next agent.

Deneb's agent docs (CLAUDE.md subtree maps, docs/agent-rules/*) embed repo
paths, `file.go:123` anchors, and symbol names. Code moves; the docs don't.
This linter extracts those references and validates them against:
  - the git file index          (path exists? unique basename?)
  - the file's current length   (line anchor still inside the file?)
  - the CodeGraph index         (symbol still defined? file:line drift?)

Tiers:
  BROKEN  — path missing / line anchor past EOF     (strict mode fails on these)
  WARN    — symbol unknown to CodeGraph              (advisory only; symbols in
            prose are heuristic extractions, so this tier never fails the run)

Escapes: `<!-- docref:off -->` ... `<!-- docref:on -->` fences a block out of
linting; a line containing `docref:ignore` is skipped.
"""

from __future__ import annotations

import argparse
import json
import re
import sqlite3
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

# Docs whose references must stay live — the agent-facing surfaces.
DEFAULT_GLOBS = [
    "CLAUDE.md",
    "**/CLAUDE.md",
    "docs/agent-rules/*.md",
    "docs/tools/*.md",
    "docs/operations/*.md",
]

CODE_EXTS = (
    ".go .kt .kts .ts .tsx .py .sh .md .json .rs .toml .yml .yaml .sql .css "
    ".mjs .cjs .js .gradle .xml .service .socket .timer .conf"
).split()

BACKTICK_RE = re.compile(r"`([^`\n]+)`")
MDLINK_RE = re.compile(r"\]\(([^)#\s]+)(?:#[^)]*)?\)")
LINE_ANCHOR_RE = re.compile(r"^(?P<path>.+?):(?P<line>\d+)(?:-\d+)?$")
# `pipeline.go:AnalyzeEmailPipeline` — path + symbol anchor convention.
SYM_ANCHOR_RE = re.compile(r"^(?P<path>[\w./-]+\.\w+):(?P<sym>[A-Za-z_]\w*)$")
# pkg.Func / Type.Method / bare Func() — the only symbol shapes safe enough to
# check without drowning in prose false-positives.
SYMBOL_RE = re.compile(r"^(?:[a-z][\w]*\.[A-Z]\w+|[A-Z]\w+\.[a-z]\w+|\w{3,}\(\))$")

SKIP_PREFIXES = ("http://", "https://", "~", "/", "$", "<", "100.", "127.", "0.")

# Symbol heads that live outside this repo (Go stdlib, Compose/Material, ktor…):
# CodeGraph rightly doesn't index them, so "unknown symbol" would be noise.
EXTERNAL_SYMBOL_HEADS = frozenset(
    "slog fmt http context json os time sync errors strings sql sqlite3 "
    "MaterialTheme Icons Modifier Text LazyColumn Compose ktor tauri React".split()
)


@dataclass
class Finding:
    doc: str
    line_no: int
    ref: str
    tier: str  # broken-path | broken-line | warn-symbol
    detail: str


@dataclass
class Report:
    findings: list[Finding] = field(default_factory=list)
    checked_refs: int = 0
    checked_docs: int = 0

    def broken(self) -> list[Finding]:
        return [f for f in self.findings if f.tier.startswith("broken")]

    def warns(self) -> list[Finding]:
        return [f for f in self.findings if f.tier.startswith("warn")]


def git_files(repo: Path) -> list[str]:
    out = subprocess.run(["git", "ls-files"], cwd=repo, capture_output=True, text=True, check=True)
    return out.stdout.splitlines()


def load_symbol_index(repo: Path) -> set[str] | None:
    """Names known to CodeGraph, or None when the index is unavailable."""
    db = repo / ".codegraph" / "codegraph.db"
    if not db.exists():
        return None
    try:
        con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
        names = {row[0] for row in con.execute("SELECT DISTINCT name FROM nodes")}
        con.close()
        return names
    except sqlite3.Error:
        return None


MIME_RE = re.compile(r"^(text|application|image|audio|video|multipart)/[\w.+-]+$")

# Skill-plugin layout convention names: every skill directory carries one, so a
# doc mention deliberately refers to the CLASS of files, not a specific one.
GENERIC_CONVENTION_NAMES = {"SKILL.md", "DESCRIPTION.md", "evals/trigger_cases.json"}


def looks_like_path(token: str) -> bool:
    if token.startswith(SKIP_PREFIXES) or " " in token:
        return False
    if any(ch in token for ch in "*{<>$|") or "..." in token:
        return False  # globs / placeholders / `a/.../b` deliberate abbreviations
    if token.startswith("@"):  # npm scope (`@scope/pkg`) — external by definition
        return False
    if any(ord(ch) > 127 for ch in token):
        # Non-ASCII (한글 위키 페이지명, `file.go:라인` placeholders) — the repo
        # tracks zero non-ASCII filenames, so these are never repo paths.
        return False
    if token in CODE_EXTS:
        return False  # a bare extension mention (`.md`), not a file
    if token.startswith("origin/"):
        return False  # git ref, not a path
    if MIME_RE.match(token):
        return False  # `text/plain` — a MIME type, not a path
    base = token.split(":", 1)[0]
    if "/" in base:
        segs = base.split("/")
        # `chat.send/history/abort` — a dotted RPC/method enumeration, not a path
        # (paths never dot their leading directory segments).
        if any("." in s for s in segs[:-1]) and not any(s.endswith(tuple(CODE_EXTS)) for s in segs):
            return False
        return bool(re.match(r"^[\w.@-]+(/[\w.@-]+)+$", base))
    return any(base.endswith(ext) for ext in CODE_EXTS) and base.count(".") >= 1


def extract_refs(text: str) -> list[tuple[int, str, str]]:
    """Yield (line_no, ref, kind[path|symbol]) honoring docref escapes."""
    refs: list[tuple[int, str, str]] = []
    active = True
    for i, line in enumerate(text.splitlines(), 1):
        if "docref:off" in line:
            active = False
            continue
        if "docref:on" in line:
            active = True
            continue
        if not active or "docref:ignore" in line:
            continue
        # Strikethrough marks retired symbols/paths on purpose — not claims.
        line = re.sub(r"~~[^~]*~~", "", line)
        tokens = BACKTICK_RE.findall(line) + MDLINK_RE.findall(line)
        for tok in tokens:
            tok = tok.strip()
            if looks_like_path(tok):
                refs.append((i, tok, "path"))
            elif SYMBOL_RE.match(tok):
                refs.append((i, tok, "symbol"))
    return refs


def file_line_count(repo: Path, rel: str) -> int:
    try:
        with open(repo / rel, "rb") as fh:
            return sum(1 for _ in fh)
    except OSError:
        return 0


def lint(
    repo: Path,
    docs: list[Path],
    symbols: set[str] | None,
    sym_locs: dict[str, dict[str, tuple[int, int]]] | None = None,
) -> Report:
    tracked = git_files(repo)
    tracked_set = set(tracked)
    tracked_dirs: set[str] = set()
    for p in tracked:
        parts = p.split("/")
        for i in range(1, len(parts)):
            tracked_dirs.add("/".join(parts[:i]))
    by_basename: dict[str, list[str]] = {}
    by_suffix: dict[str, list[str]] = {}
    for p in tracked:
        by_basename.setdefault(p.rsplit("/", 1)[-1], []).append(p)
        segs = p.split("/")
        for i in range(len(segs) - 1):
            by_suffix.setdefault("/".join(segs[i + 1 :]), []).append(p)
    # Directory suffixes get the same treatment as file suffixes: module docs
    # write `genesis/lifecycle` meaning .../domain/skills/genesis/lifecycle.
    by_dir_suffix: dict[str, list[str]] = {}
    for d in tracked_dirs:
        segs = d.split("/")
        for i in range(len(segs)):
            by_dir_suffix.setdefault("/".join(segs[i:]), []).append(d)

    report = Report()
    for doc in docs:
        try:
            rel_doc = str(doc.relative_to(repo))
        except ValueError:
            rel_doc = str(doc)  # --extra-docs: 레포 밖 문서는 절대경로로 표기
        try:
            text = doc.read_text(encoding="utf-8")
        except OSError:
            continue
        report.checked_docs += 1
        doc_dir = doc.parent
        for line_no, ref, kind in extract_refs(text):
            report.checked_refs += 1
            if kind == "symbol":
                name = ref.rstrip("()").split(".")[-1]
                head = ref.split(".")[0].rstrip("()")
                # stdlib / framework symbols are legitimately absent from the
                # repo's CodeGraph — verifying them is pure noise.
                if head in EXTERNAL_SYMBOL_HEADS:
                    continue
                if symbols is not None and name not in symbols and head not in symbols:
                    report.findings.append(
                        Finding(rel_doc, line_no, ref, "warn-symbol", "CodeGraph에 없는 심볼")
                    )
                continue

            m = LINE_ANCHOR_RE.match(ref)
            sym_anchor: str | None = None
            if m:
                path_part, anchor = m.group("path"), int(m.group("line"))
            else:
                ms = SYM_ANCHOR_RE.match(ref)
                if ms:
                    path_part, anchor, sym_anchor = ms.group("path"), None, ms.group("sym")
                else:
                    path_part, anchor = ref, None

            # Resolution order mirrors how the docs actually write paths:
            # repo-relative → doc-relative → any-ancestor-relative (module docs
            # write `runtime/rpc` meaning gateway-go/internal/runtime/rpc) →
            # unique-ish path suffix (`chat/skill_hints.go`) → bare basename.
            # Directories count: subtree maps reference packages, not files.
            resolved: str | None = None
            if path_part in tracked_set or path_part.rstrip("/") in tracked_dirs:
                resolved = path_part
            else:
                base = doc_dir
                while resolved is None:
                    try:
                        cand = str((base / path_part).resolve().relative_to(repo))
                    except ValueError:
                        cand = None
                    if cand and (cand in tracked_set or cand in tracked_dirs):
                        resolved = cand
                    if base == repo or base == base.parent:
                        break  # repo 루트 또는 파일시스템 루트(레포 밖 문서) 도달
                    base = base.parent
                if resolved is None:
                    # Well-known module roots: docs shorthand like `pkg/safego`
                    # or `internal/runtime/server` omits the module prefix.
                    for root in (
                        "gateway-go",
                        "gateway-go/internal",
                        "gateway-go/pkg",
                        "andromeda",
                        "andromeda/src",
                        "client-android/app",
                    ):
                        cand = f"{root}/{path_part}"
                        if cand in tracked_set or cand in tracked_dirs:
                            resolved = cand
                            break
                if resolved is None and path_part in by_suffix:
                    candidates = by_suffix[path_part]
                elif resolved is None and path_part.rstrip("/") in by_dir_suffix:
                    candidates = sorted(set(by_dir_suffix[path_part.rstrip("/")]))
                elif resolved is None and "/" not in path_part and path_part in by_basename:
                    candidates = by_basename[path_part]
                else:
                    candidates = []
                if resolved is None and candidates:
                    resolved = candidates[0]
                    if len(candidates) > 1 and path_part in GENERIC_CONVENTION_NAMES:
                        # `SKILL.md` in prose means "any skill's SKILL.md" —
                        # the ambiguity IS the meaning for these plugin-layout
                        # convention names; warning on each mention is noise.
                        resolved = candidates[0]
                    elif len(candidates) > 1:
                        # Rescued only by an AMBIGUOUS short name — this ref is
                        # effectively unverifiable (any same-named file keeps it
                        # green forever). Surface it instead of silently passing.
                        report.findings.append(
                            Finding(
                                rel_doc,
                                line_no,
                                ref,
                                "warn-ambiguous",
                                f"동명 파일 {len(candidates)}개 — 경로를 더 구체화해야 검증됨",
                            )
                        )
                        resolved = ("*ambiguous*", candidates)

            if resolved is None:
                # Tiering by what a miss most likely MEANS:
                #  - source-file exts (.go/.kt/.ts/…): sources live in the repo,
                #    so a miss is rot → BROKEN.
                #  - bare data/doc names (`deneb.json`, `로그.md`): usually
                #    runtime files under ~/.deneb or wiki examples → warn.
                #  - extension-less multi-segment: concepts, API routes,
                #    external repos → warn.
                SOURCE_EXTS = (
                    ".go",
                    ".kt",
                    ".kts",
                    ".ts",
                    ".tsx",
                    ".py",
                    ".sh",
                    ".rs",
                    ".mjs",
                    ".css",
                )
                has_src_ext = any(path_part.endswith(ext) for ext in SOURCE_EXTS)
                bare = "/" not in path_part
                has_ext = any(path_part.endswith(ext) for ext in CODE_EXTS)
                if has_src_ext:
                    tier = "broken-path"
                elif bare or not has_ext:
                    tier = "warn-path"
                else:
                    tier = "broken-path"
                report.findings.append(Finding(rel_doc, line_no, ref, tier, "레포에 없는 경로"))
                continue
            if sym_anchor is not None and symbols is not None and sym_anchor not in symbols:
                # CodeGraph doesn't index every language (shell functions, SQL…).
                # The anchor names a SPECIFIC file, so fall back to checking the
                # file's own text before warning.
                target = resolved[1][0] if isinstance(resolved, tuple) else resolved
                try:
                    body = (repo / target).read_text(encoding="utf-8", errors="ignore")
                except OSError:
                    body = ""
                if sym_anchor not in body:
                    report.findings.append(
                        Finding(
                            rel_doc,
                            line_no,
                            ref,
                            "warn-symbol",
                            "파일·CodeGraph 모두에 없는 심볼 앵커",
                        )
                    )
            if anchor is not None:
                # Ambiguous rescue: accept the anchor if ANY candidate is long
                # enough (under-alarming beats mis-attributing a wrong file).
                if isinstance(resolved, tuple):
                    lengths = [file_line_count(repo, c) for c in resolved[1]]
                    ok = any(anchor <= n for n in lengths)
                    longest = max(lengths, default=0)
                else:
                    longest = file_line_count(repo, resolved)
                    ok = anchor <= longest
                if not ok:
                    report.findings.append(
                        Finding(
                            rel_doc,
                            line_no,
                            ref,
                            "broken-line",
                            f"라인 앵커 {anchor} > 파일 길이 {longest}",
                        )
                    )
                elif sym_locs is not None and not isinstance(resolved, tuple):
                    # Drift check: an in-bounds anchor can still point at the
                    # wrong place after code moves. When the same doc line names
                    # a symbol CodeGraph locates in the anchored file, the
                    # anchor must land inside that symbol's span — else warn
                    # (advisory: prose may legitimately anchor elsewhere) and
                    # let --fix snap it to the symbol's start line.
                    file_syms = sym_locs.get(resolved, {})
                    doc_line = (
                        text.splitlines()[line_no - 1] if line_no <= len(text.splitlines()) else ""
                    )
                    hinted = []
                    for tok in BACKTICK_RE.findall(doc_line):
                        tok = tok.strip().rstrip("()")
                        if tok == ref:
                            continue
                        name = tok.split(".")[-1]
                        if name in file_syms:
                            hinted.append((name, file_syms[name]))
                    # ANY hinted symbol whose span contains the anchor vouches
                    # for it (a doc line often names several symbols; matching
                    # only the first would false-alarm on the rest).
                    if hinted and not any(s <= anchor <= e for _, (s, e) in hinted):
                        names = ", ".join(f"`{n}`({s}–{e})" for n, (s, e) in hinted[:3])
                        report.findings.append(
                            Finding(
                                rel_doc,
                                line_no,
                                ref,
                                "warn-drift",
                                f"앵커 {anchor}이 같은 줄 심볼 {names} 범위 밖 — --fix로 수리 가능",
                            )
                        )
    return report


# Nested agent worktree checkouts copy CLAUDE.md at the snapshot they were
# created. Those copies rot as main moves and are not the docs we maintain —
# weekly-ref-audit was filing hundreds of BROKEN hits from them.
_WORKTREE_PARENTS = frozenset({".claude", ".zcode", ".cursor", ".codex", ".trae"})
_SKIP_DIR_PARTS = frozenset({"node_modules", ".git"})


def is_skipped_doc(path: Path, repo: Path | None = None) -> bool:
    # Judge the path relative to the repo root. Absolute parts would skip
    # every file when the checkout itself lives under ~/.cursor/worktrees/…
    if repo is not None:
        try:
            parts = path.resolve().relative_to(repo.resolve()).parts
        except ValueError:
            parts = path.parts
    else:
        parts = path.parts
    if any(part in _SKIP_DIR_PARTS for part in parts):
        return True
    if ".worktrees" in parts:
        return True
    for i, part in enumerate(parts):
        if part == "worktrees" and i > 0 and parts[i - 1] in _WORKTREE_PARENTS:
            return True
    return False


def collect_docs(repo: Path, globs: list[str]) -> list[Path]:
    seen: set[Path] = set()
    for g in globs:
        for p in sorted(repo.glob(g)):
            if p.is_file() and not is_skipped_doc(p, repo):
                seen.add(p)
    return sorted(seen)


def fix_broken_lines(repo: Path, report: Report, symbols_by_file: dict[str, dict[str, int]]) -> int:
    """--fix: repair broken line anchors deterministically.

    Rule 1 — a backtick symbol on the same doc line that CodeGraph locates in
    the anchored file: rewrite `f.go:N` → `f.go:<symbol start line>`.
    Rule 2 — otherwise drop the stale line (`f.go:N` → `f.go`): losing a number
    beats shipping a lie. Anything else is left for a human."""
    fixed = 0
    targets = [f for f in report.findings if f.tier in ("broken-line", "warn-drift")]
    for f in targets:
        doc = repo / f.doc
        lines = doc.read_text(encoding="utf-8").splitlines(keepends=True)
        line = lines[f.line_no - 1]
        path_part = f.ref.rsplit(":", 1)[0]
        target = None
        file_syms = {}
        for cand, syms in symbols_by_file.items():
            if cand.endswith(path_part) or path_part.endswith(cand):
                file_syms = syms
                break
        for tok in BACKTICK_RE.findall(line):
            tok = tok.strip().rstrip("()")
            name = tok.split(".")[-1]
            if name in file_syms:
                target = f"{path_part}:{file_syms[name][0]}"
                break
        replacement = target or path_part
        lines[f.line_no - 1] = line.replace(f.ref, replacement, 1)
        doc.write_text("".join(lines), encoding="utf-8")
        fixed += 1
    return fixed


def load_symbols_by_file(repo: Path) -> dict[str, dict[str, int]]:
    db = repo / ".codegraph" / "codegraph.db"
    if not db.exists():
        return {}
    try:
        con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
        out: dict[str, dict[str, tuple[int, int]]] = {}
        for name, fp, start, end in con.execute(
            "SELECT name, file_path, start_line, end_line FROM nodes"
            " WHERE kind IN ('function','method','struct','class','type','constant','variable')"
        ):
            out.setdefault(fp, {}).setdefault(name, (start, end))
        con.close()
        return out
    except sqlite3.Error:
        return {}


def unmentioned(repo: Path, src_dir: str, doc_rel: str) -> list[str]:
    """Curation aid: source files under src_dir the doc never mentions.
    Advisory by design — module docs curate, they don't inventory."""
    doc_text = (repo / doc_rel).read_text(encoding="utf-8", errors="ignore")
    out = []
    for p in sorted((repo / src_dir).glob("*.go")):
        if p.name.endswith("_test.go"):
            continue
        if p.name not in doc_text:
            out.append(p.name)
    return out


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--repo", default=".", type=Path)
    ap.add_argument("--glob", action="append", help="override default doc globs")
    ap.add_argument(
        "--extra-docs",
        action="append",
        type=Path,
        help="레포 밖 문서 디렉토리(메모리 등) — refs는 레포 기준 검증",
    )
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--strict", action="store_true", help="broken 참조가 있으면 exit 1")
    ap.add_argument("--fix", action="store_true", help="broken 라인 앵커를 결정적 규칙으로 수리")
    ap.add_argument(
        "--unmentioned",
        nargs=2,
        metavar=("SRC_DIR", "DOC"),
        help="큐레이션 감사: SRC_DIR의 소스 중 DOC에 미언급 파일 나열",
    )
    args = ap.parse_args(argv)

    repo = args.repo.resolve()
    if args.unmentioned:
        for name in unmentioned(repo, *args.unmentioned):
            print(name)
        return 0
    docs = collect_docs(repo, args.glob or DEFAULT_GLOBS)
    for extra in args.extra_docs or []:
        docs += sorted(p for p in extra.resolve().glob("*.md") if p.is_file())
    symbols = load_symbol_index(repo)
    sym_locs = load_symbols_by_file(repo)
    report = lint(repo, docs, symbols, sym_locs)
    fixables = report.broken() + [f for f in report.findings if f.tier == "warn-drift"]
    if args.fix and fixables:
        n = fix_broken_lines(repo, report, sym_locs)
        print(f"--fix: {n}건 수리 — 재검증 필요")
        report = lint(repo, docs, symbols, sym_locs)

    if args.json:
        print(
            json.dumps(
                {
                    "checkedDocs": report.checked_docs,
                    "checkedRefs": report.checked_refs,
                    "broken": [vars(f) for f in report.broken()],
                    "warnings": [vars(f) for f in report.warns()],
                },
                ensure_ascii=False,
                indent=2,
            )
        )
    else:
        for f in report.broken():
            print(f"BROKEN {f.doc}:{f.line_no}  `{f.ref}`  — {f.detail}")
        for f in report.warns():
            print(f"warn   {f.doc}:{f.line_no}  `{f.ref}`  — {f.detail}")
        print(
            f"\ndoc-ref-lint: {report.checked_docs} docs, {report.checked_refs} refs — "
            f"{len(report.broken())} broken, {len(report.warns())} symbol warnings"
            + ("" if symbols is not None else " (CodeGraph 인덱스 없음 — 심볼 검증 생략)")
        )
    return 1 if (args.strict and report.broken()) else 0


if __name__ == "__main__":
    sys.exit(main())

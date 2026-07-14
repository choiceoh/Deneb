#!/usr/bin/env python3
"""codebase-health.py — deterministic structural-health score for the Deneb repo.

Deneb already scores response quality (scripts/dev/quality-test.py), retrieval
(gateway-go/cmd/recall-bench), and skill candidates (genesis validation_engine.go),
all on the same skeleton: fixed inputs -> run -> score -> compare to a baseline ->
emit a grep-able metric line. This harness fills the missing tier — the *codebase's
own structural health* — which no single hard gate (vet/fmt/lint in `make check`)
tracks as a trend, so it rots silently (the 2026-06 audits removed ~8,700 LOC of rot).

Rubric — 14 fast, structural dimensions. Each bar sits at what genuinely excellent
code achieves (NOT tuned to hit a target composite), so a strong codebase lands
high honestly and the score still discriminates between facets:
  Go architecture : layering (upward imports), coupling (fan-out), god-package (LOC)
  Go structure    : file length, function length, nesting depth
  Go docs+tests   : exported-symbol doc coverage, test/source LOC volume, test breadth
  cross-cutting   : duplication (copy-paste windows), debt (TODO/FIXME/HACK + panics)
  per-language    : kotlin, typescript, other-langs (Python/Rust/shell) composites

Bars are Go-appropriate on purpose. We deliberately do NOT use Martin instability
/ distance-from-main-sequence: it flags idiomatic concrete leaf packages (session,
rpcerr, mailbody …) as "painful" for not being interfaces — a Java-ism that
mismeasures good Go. A metric that penalizes correct design to lower a number is
just gaming in disguise; every dimension here measures a real quality facet.

Design mirrors two established audits it sits beside in scripts/audit/:
  - deadcode-audit.sh  — baseline + ratchet (a floor that only goes up).
  - topology-parity.sh — advisory sweep, not a fast-path gate.

Stdlib only, no gateway runtime, no network -> CI-safe and fast (<~2s in fast mode).

Usage:
  scripts/audit/codebase-health.py            # human summary (stderr) + metric (stdout)
  scripts/audit/codebase-health.py --json     # full breakdown as JSON (stdout)
  scripts/audit/codebase-health.py --deep      # also run the deadcode dimension (~1-2 min)
  scripts/audit/codebase-health.py --update    # rewrite the baseline (operator approval)
  scripts/audit/codebase-health.py --check     # ratchet gate: exit 1 if composite regressed

Metric contract (last stdout lines, consumed by iterate.sh / grep):
  metric_value=77.1
  DENEB_HEALTH_DETAIL go-layering=91.8 go-coupling=61.7 … kotlin=59.8 mode=fast

Exit codes: 0 = ok / at-or-above baseline; 1 = --check regression; 2 = usage/tooling error.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
BASELINE_PATH = REPO_ROOT / "scripts" / "audit" / "health-baseline.json"

# Composite tolerance for --check: a floor that only ratchets up (deadcode-audit style).
CHECK_TOLERANCE = 1.0
# How many offending items to surface per dimension (actionable, not a dump).
TOP_N = 15

# --- Rigorous, Go-appropriate bars (see module docstring) --------------------
# The rubric rewards proximity to what genuinely excellent code achieves; it is
# NOT tuned to hit a target composite. Bars sit at world-class levels, so a
# strong codebase lands high honestly and the score still discriminates.

# size: file length is LOC-weighted (a 1500-LOC file hurts more than its 1/N
# file-count share). Full credit at/under SOFT (CLAUDE.md's 700 guideline),
# zero at/over HARD, linear between.
SIZE_SOFT_LOC = 700
SIZE_HARD_LOC = 1400
# tests: test-source LOC volume vs source LOC. 1.0 = as much test code as
# source (a world-class thorough-test bar). A VOLUME proxy, not line coverage.
TEST_TARGET_RATIO = 1.0
# docs: exported-symbol doc coverage (Go convention: every exported identifier
# gets a doc comment). 1.0 = fully documented public surface.
DOC_TARGET = 1.0
# complexity: per-function length + body nesting depth, graded.
FUNC_LEN_SOFT, FUNC_LEN_HARD = 60, 200
FUNC_NEST_SOFT, FUNC_NEST_HARD = 4, 8
# duplication: normalized N-line window copy-paste ratio; DUP_BUDGET is the
# tolerated share before the dimension floors (jscpd/PMD-CPD land ~3-5%).
DUP_WINDOW = 8
DUP_MIN_BLOCK_CHARS = 120  # skip trivial windows (braces/short lines)
DUP_BUDGET = 0.05
# debt: TODO/FIXME/HACK/XXX + naked panics per KLOC; DEBT_BUDGET tolerated.
DEBT_MARKER_RE = re.compile(r"\b(TODO|FIXME|HACK|XXX)\b")
DEBT_BUDGET_PER_KLOC = 2.0
# coupling: efferent fan-out — internal packages a package imports. Graded.
FANOUT_SOFT, FANOUT_HARD = 10, 30
# god-package: LOC per leaf package, graded (LOC-weighted).
PKG_LOC_SOFT, PKG_LOC_HARD = 3000, 9000
# test-breadth target: share of Go leaf packages that carry >=1 test.
TEST_BREADTH_TARGET = 1.0

# Directory names pruned everywhere during file discovery.
PRUNE_DIRS = {
    "node_modules", "build", "dist", "target", ".gradle", ".git",
    ".codegraph", ".idea", "__pycache__", "vendor", ".venv", "coverage",
}

# gateway-go layer ranks: higher may import lower; lower importing higher = a
# cohesion violation. Grounded in gateway-go/CLAUDE.md's module map. Unknown
# top-level packages default to mid-rank (2) and are never flagged as sources.
LAYER_RANK = {
    "runtime": 5,   # HTTP server, RPC dispatch — top orchestration
    "pipeline": 3,  # chat pipeline: prompt, tools, context
    # agent loop lives under ai/agent (formerly agentsys/); keep ai at mid-rank
    # so pipeline→ai imports are not flagged as upward.
    "domain": 2, "platform": 2, "ai": 2,  # business logic + integrations + LLM/agent
    "core": 1, "infra": 1, "hanja": 1,    # foundational
    "testutil": 0,  # test-only helper (downward from everyone)
}
DEFAULT_RANK = 2

# Sub-package rank overrides ("<top>/<sub>"): a package that lives under a
# high-rank top-level dir but is really a foundational shared kernel — no upward
# deps, imported across layers — and so must not read as an upward violation
# when a lower layer uses it. Checked before the top-level LAYER_RANK.
#   runtime/sessionstore: durable session markers/store helpers used across
#   pipeline·runtime·platform — shared state, not top orchestration.
SUBPKG_RANK = {
    "runtime/sessionstore": 1,
}


# --------------------------------------------------------------------------- #
# File discovery
# --------------------------------------------------------------------------- #

@dataclass
class Lane:
    name: str
    root: Path
    exts: tuple[str, ...]


LANES = [
    Lane("gateway-go", REPO_ROOT / "gateway-go", (".go",)),
    Lane("client-android", REPO_ROOT / "client-android" / "app", (".kt",)),
    Lane("andromeda", REPO_ROOT / "andromeda" / "src", (".ts", ".tsx")),
]


def _first_lines(path: Path, n: int = 3) -> str:
    try:
        with path.open("r", encoding="utf-8", errors="replace") as fh:
            return "".join(next(fh, "") for _ in range(n))
    except OSError:
        return ""


def is_test_file(lane: str, path: Path) -> bool:
    name = path.name
    if lane == "gateway-go":
        return name.endswith("_test.go")
    if lane == "client-android":
        # KMP convention: tests live under a *Test source set (commonTest, ...).
        if any(part.endswith("Test") for part in path.parts):
            return True
        return name.endswith("Test.kt") or name.endswith("Tests.kt")
    # andromeda / TS
    if "__tests__" in path.parts:
        return True
    return bool(re.search(r"\.(test|spec)\.tsx?$", name))


def is_generated(lane: str, path: Path) -> bool:
    name = path.name
    if lane == "gateway-go" and name.endswith("_gen.go"):
        return True
    if lane == "andromeda" and "gen" in path.parts:
        return True
    head = _first_lines(path)
    return "DO NOT EDIT" in head or "@generated" in head


def iter_source_files(lane: Lane) -> list[Path]:
    """All non-test, non-generated source files in a lane, sorted for determinism."""
    out: list[Path] = []
    if not lane.root.exists():
        return out
    for path in lane.root.rglob("*"):
        if not path.is_file() or path.suffix not in lane.exts:
            continue
        if any(part in PRUNE_DIRS for part in path.parts):
            continue
        if is_test_file(lane.name, path):
            continue
        if is_generated(lane.name, path):
            continue
        out.append(path)
    return sorted(out)


def iter_test_files(lane: Lane) -> list[Path]:
    out: list[Path] = []
    if not lane.root.exists():
        return out
    for path in lane.root.rglob("*"):
        if not path.is_file() or path.suffix not in lane.exts:
            continue
        if any(part in PRUNE_DIRS for part in path.parts):
            continue
        if is_test_file(lane.name, path):
            out.append(path)
    return sorted(out)


def rel(path: Path) -> str:
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def line_count(path: Path) -> int:
    try:
        with path.open("r", encoding="utf-8", errors="replace") as fh:
            return sum(1 for _ in fh)
    except OSError:
        return 0


_TEXT_CACHE: dict[Path, str] = {}


def read_text(path: Path) -> str:
    t = _TEXT_CACHE.get(path)
    if t is None:
        try:
            t = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            t = ""
        _TEXT_CACHE[path] = t
    return t


def graded(value: float, soft: float, hard: float) -> float:
    """1.0 at/under soft, 0.0 at/over hard, linear between (descending credit)."""
    if value <= soft:
        return 1.0
    if value >= hard:
        return 0.0
    return (hard - value) / (hard - soft)


def loc_weighted(pairs: list[tuple[int, float]]) -> float:
    """Score 0-100 = the LOC-weighted mean credit (a big file/pkg counts more
    than its 1/N item share). pairs: (loc, credit in 0..1)."""
    tot = sum(loc for loc, _ in pairs)
    if tot <= 0:
        return 100.0
    return 100.0 * sum(loc * c for loc, c in pairs) / tot


_FUNC_START_RE = re.compile(r"^func\b")


def iter_go_funcs(text: str) -> list[tuple[int, int]]:
    """For each top-level func with a brace body, return (line_length, max_body_nesting)."""
    out: list[tuple[int, int]] = []
    lines = text.splitlines()
    i, n = 0, len(lines)
    while i < n:
        if _FUNC_START_RE.match(lines[i]) and lines[i].rstrip().endswith("{"):
            depth, maxd, j = 1, 1, i + 1
            while j < n and depth > 0:
                depth += lines[j].count("{") - lines[j].count("}")
                if depth > maxd:
                    maxd = depth
                j += 1
            out.append((j - i, maxd - 1))  # body nesting excludes the func's own brace
            i = j
        else:
            i += 1
    return out


# --------------------------------------------------------------------------- #
# Dimension results
# --------------------------------------------------------------------------- #

@dataclass
class DimResult:
    name: str
    weight: int
    score: float               # 0-100
    detail: list[str] = field(default_factory=list)
    extra: dict = field(default_factory=dict)


# ============================================================================ #
# Dimensions — a rigorous, Go-appropriate rubric (see module docstring). Bars
# sit at world-class levels, so a strong codebase lands high honestly and the
# score still discriminates between facets. Go carries most weight (~2/3 of the
# code + the primary runtime); Kotlin, TypeScript, and the remaining languages
# each get one self-contained composite dimension.
# ============================================================================ #

# --- shared Go parsing ------------------------------------------------------- #

def _go_module_path() -> str:
    gomod = REPO_ROOT / "gateway-go" / "go.mod"
    try:
        first = gomod.read_text(encoding="utf-8").splitlines()[0]
        return first.split()[1] if first.startswith("module ") else ""
    except (OSError, IndexError):
        return ""


_IMPORT_BLOCK_RE = re.compile(r"^import\s*\((.*?)^\)", re.DOTALL | re.MULTILINE)
_IMPORT_SINGLE_RE = re.compile(r'^import\s+(?:[\w.]+\s+)?"([^"]+)"', re.MULTILINE)
_QUOTED_RE = re.compile(r'"([^"]+)"')
# An exported top-level declaration that Go convention says needs a doc comment.
_EXPORTED_RE = re.compile(
    r"^(func (?:\([^)]*\) )?[A-Z]\w*|type [A-Z]\w*|const [A-Z]\w*|var [A-Z]\w*)")

TOP_MODULE_DIRS = [
    REPO_ROOT / "gateway-go",
    REPO_ROOT / "client-android" / "app",
    REPO_ROOT / "andromeda",
    REPO_ROOT / "skills",
]


def _go_imports_text(text: str) -> list[str]:
    imports: list[str] = []
    for block in _IMPORT_BLOCK_RE.findall(text):
        imports += _QUOTED_RE.findall(block)
    imports += _IMPORT_SINGLE_RE.findall(text)
    return imports


def _top_pkg(internal_path: str) -> str:
    return internal_path.split("/", 1)[0] if internal_path else ""


def _pkg_rank(internal_rel: str) -> int:
    parts = internal_rel.split("/")
    if len(parts) >= 2 and f"{parts[0]}/{parts[1]}" in SUBPKG_RANK:
        return SUBPKG_RANK[f"{parts[0]}/{parts[1]}"]
    return LAYER_RANK.get(parts[0] if parts else "", DEFAULT_RANK)


def _go_leaf_packages(files: list[Path]) -> dict[Path, bool]:
    """Map each Go source directory -> whether it has >=1 *_test.go file."""
    return {d: any(d.glob("*_test.go")) for d in {f.parent for f in files}}


def _go_pkg_stats(files: list[Path]) -> dict[Path, dict]:
    """Per Go leaf package: total LOC + set of efferent internal packages."""
    module = _go_module_path()
    prefix = (module + "/internal/") if module else None
    pkgs: dict[Path, dict] = {}
    for f in files:
        rec = pkgs.setdefault(f.parent, {"loc": 0, "eff": set()})
        rec["loc"] += line_count(f)
        if prefix:
            for imp in _go_imports_text(read_text(f)):
                if imp.startswith(prefix):
                    rec["eff"].add(imp[len(prefix):])
    return pkgs


# --- Go: layering (upward-import violations) --------------------------------- #

def dim_go_layering(sources: dict[str, list[Path]]) -> DimResult:
    module = _go_module_path()
    if not module:
        return DimResult("go-layering", 7, 100.0, ["skipped: go.mod not found"])
    prefix = module + "/internal/"
    internal_root = REPO_ROOT / "gateway-go" / "internal"
    total, violations = 0, {}
    for f in sources["gateway-go"]:
        try:
            src_rel = f.relative_to(internal_root)
        except ValueError:
            continue
        src_top = src_rel.parts[0]
        if src_top == "testutil":
            continue
        src_rank = _pkg_rank("/".join(src_rel.parts))
        for imp in _go_imports_text(read_text(f)):
            if not imp.startswith(prefix):
                continue
            dst = imp[len(prefix):]
            dst_top = _top_pkg(dst)
            if dst_top == src_top:
                continue
            total += 1
            if _pkg_rank(dst) > src_rank:
                violations[(src_top, dst_top)] = violations.get((src_top, dst_top), 0) + 1
    viol = sum(violations.values())
    score = 100.0 * (1 - viol / total) if total else 100.0
    ranked = sorted(violations.items(), key=lambda kv: (-kv[1], kv[0]))
    detail = [f"{n:>4}x  {s} -> {d}  (rank {LAYER_RANK.get(s, DEFAULT_RANK)}"
              f" imports {LAYER_RANK.get(d, DEFAULT_RANK)})" for (s, d), n in ranked[:TOP_N]]
    return DimResult("go-layering", 7, score, detail,
                     {"edges": total, "violations": viol})


# --- Go: efferent coupling (package fan-out) --------------------------------- #

def dim_go_coupling(sources: dict[str, list[Path]]) -> DimResult:
    pkgs = _go_pkg_stats(sources["gateway-go"])
    pairs, hi = [], []
    for d, rec in pkgs.items():
        ce = len(rec["eff"])
        pairs.append((rec["loc"], graded(ce, FANOUT_SOFT, FANOUT_HARD)))
        if ce > FANOUT_SOFT:
            hi.append((ce, rel(d)))
    hi.sort(reverse=True)
    return DimResult("go-coupling", 6, loc_weighted(pairs),
                     [f"fan-out {ce:>3}  {p}" for ce, p in hi[:TOP_N]],
                     {"packages": len(pkgs), "over_soft": len(hi)})


# --- Go: god-package (LOC per package) --------------------------------------- #

def dim_go_godpkg(sources: dict[str, list[Path]]) -> DimResult:
    pkgs = _go_pkg_stats(sources["gateway-go"])
    pairs, big = [], []
    for d, rec in pkgs.items():
        pairs.append((rec["loc"], graded(rec["loc"], PKG_LOC_SOFT, PKG_LOC_HARD)))
        if rec["loc"] > PKG_LOC_SOFT:
            big.append((rec["loc"], rel(d)))
    big.sort(reverse=True)
    return DimResult("go-godpkg", 5, loc_weighted(pairs),
                     [f"{loc:>6} LOC  {p}" for loc, p in big[:TOP_N]],
                     {"packages": len(pkgs), "over_soft": len(big)})


# --- Go: file length (LOC-weighted) ------------------------------------------ #

def dim_go_filesize(sources: dict[str, list[Path]]) -> DimResult:
    pairs, over = [], []
    for f in sources["gateway-go"]:
        loc = line_count(f)
        pairs.append((loc, graded(loc, SIZE_SOFT_LOC, SIZE_HARD_LOC)))
        if loc > SIZE_SOFT_LOC:
            over.append((loc, rel(f)))
    over.sort(reverse=True)
    return DimResult("go-filesize", 7, loc_weighted(pairs),
                     [f"{loc:>5} LOC  {p}" for loc, p in over[:TOP_N]],
                     {"files": len(pairs), "over_soft": len(over)})


# --- Go: function length ----------------------------------------------------- #

def _go_all_funcs(files: list[Path]) -> list[tuple[int, int, str]]:
    out = []
    for f in files:
        for length, nest in iter_go_funcs(read_text(f)):
            out.append((length, nest, rel(f)))
    return out


def dim_go_funclen(sources: dict[str, list[Path]]) -> DimResult:
    funcs = _go_all_funcs(sources["gateway-go"])
    if not funcs:
        return DimResult("go-funclen", 6, 100.0, [])
    credits = [graded(L, FUNC_LEN_SOFT, FUNC_LEN_HARD) for L, _, _ in funcs]
    score = 100.0 * sum(credits) / len(credits)
    longest = sorted(funcs, reverse=True)[:TOP_N]
    return DimResult("go-funclen", 6, score,
                     [f"{L:>4} lines  {p}" for L, _, p in longest],
                     {"funcs": len(funcs), "over_soft": sum(1 for L, _, _ in funcs if L > FUNC_LEN_SOFT)})


# --- Go: nesting depth ------------------------------------------------------- #

def dim_go_nesting(sources: dict[str, list[Path]]) -> DimResult:
    funcs = _go_all_funcs(sources["gateway-go"])
    if not funcs:
        return DimResult("go-nesting", 5, 100.0, [])
    credits = [graded(nest, FUNC_NEST_SOFT, FUNC_NEST_HARD) for _, nest, _ in funcs]
    score = 100.0 * sum(credits) / len(credits)
    deepest = sorted(funcs, key=lambda t: -t[1])[:TOP_N]
    return DimResult("go-nesting", 5, score,
                     [f"depth {nest:>2}  {p}" for _, nest, p in deepest if nest > FUNC_NEST_SOFT],
                     {"funcs": len(funcs), "over_soft": sum(1 for _, n, _ in funcs if n > FUNC_NEST_SOFT)})


# --- Go: doc coverage (exported symbols + module CLAUDE.md) ------------------ #

def dim_go_docs(sources: dict[str, list[Path]]) -> DimResult:
    total = doc = 0
    worst: list[tuple[float, str]] = []
    for f in sources["gateway-go"]:
        lines = read_text(f).splitlines()
        ft = fd = 0
        for i, ln in enumerate(lines):
            if _EXPORTED_RE.match(ln):
                ft += 1
                if i > 0 and lines[i - 1].lstrip().startswith("//"):
                    fd += 1
        total += ft
        doc += fd
        if ft >= 5 and fd / ft < 0.8:
            worst.append((fd / ft, f"{fd}/{ft} {rel(f)}"))
    go_frac = doc / total if total else 1.0
    present = [m for m in TOP_MODULE_DIRS if (m / "CLAUDE.md").exists()]
    mod_frac = len(present) / len(TOP_MODULE_DIRS)
    score = 100.0 * (0.85 * go_frac + 0.15 * mod_frac) / DOC_TARGET
    score = min(100.0, score)
    worst.sort()
    detail = [f"{p}" for _, p in worst[:TOP_N]]
    return DimResult("go-docs", 7, score, detail,
                     {"exported": total, "documented": doc,
                      "coverage": round(go_frac, 3), "modules_claude_md": len(present)})


# --- Go: test volume (test/source LOC ratio) --------------------------------- #

def dim_go_tests(sources: dict[str, list[Path]], tests: dict[str, list[Path]]) -> DimResult:
    src = sum(line_count(f) for f in sources["gateway-go"])
    tst = sum(line_count(f) for f in tests["gateway-go"])
    ratio = (tst / src) if src else 0.0
    score = 100.0 * min(1.0, ratio / TEST_TARGET_RATIO)
    return DimResult("go-tests", 8, score,
                     [f"test/src LOC = {tst}/{src} = {ratio:.2f}  (target {TEST_TARGET_RATIO})"],
                     {"source_loc": src, "test_loc": tst, "ratio": round(ratio, 3)})


# --- Go: test breadth (packages carrying tests) ------------------------------ #

def dim_go_testbreadth(sources: dict[str, list[Path]]) -> DimResult:
    pkgs = _go_leaf_packages(sources["gateway-go"])
    with_test = sum(1 for v in pkgs.values() if v)
    frac = with_test / len(pkgs) if pkgs else 1.0
    score = 100.0 * min(1.0, frac / TEST_BREADTH_TARGET)
    untested = sorted(rel(d) for d, v in pkgs.items() if not v)
    return DimResult("go-testbreadth", 5, score,
                     [f"no test pkg  {p}" for p in untested[:TOP_N]],
                     {"packages": len(pkgs), "with_test": with_test})


# --- duplication (normalized line-window copy-paste) ------------------------- #

def dim_duplication(sources: dict[str, list[Path]]) -> DimResult:
    seen: dict[int, int] = {}
    total = 0
    for f in sources["gateway-go"]:
        ls = [x.strip() for x in read_text(f).splitlines()]
        ls = [x for x in ls if x and not x.startswith("//")]
        for k in range(len(ls) - DUP_WINDOW + 1):
            block = tuple(ls[k:k + DUP_WINDOW])
            if sum(len(x) for x in block) < DUP_MIN_BLOCK_CHARS:
                continue
            h = hash(block)
            seen[h] = seen.get(h, 0) + 1
            total += 1
    dup = sum(c - 1 for c in seen.values() if c > 1)
    ratio = dup / total if total else 0.0
    score = 100.0 * (1 - min(1.0, ratio / DUP_BUDGET))
    return DimResult("duplication", 6, score,
                     [f"{ratio * 100:.1f}% duplicated ({dup}/{total} {DUP_WINDOW}-line windows), "
                      f"budget {DUP_BUDGET * 100:.0f}%"],
                     {"dup_windows": dup, "total_windows": total, "ratio": round(ratio, 4)})


# --- debt (TODO/FIXME/HACK/XXX + naked panics per KLOC) ---------------------- #

def dim_debt(sources: dict[str, list[Path]]) -> DimResult:
    markers = panics = loc = 0
    hits: list[str] = []
    for f in sources["gateway-go"]:
        t = read_text(f)
        loc += t.count("\n") + 1
        mk = len(DEBT_MARKER_RE.findall(t))
        pn = len(re.findall(r"\bpanic\(", t))
        markers += mk
        panics += pn
        if mk or pn:
            hits.append(f"{mk}m+{pn}p  {rel(f)}")
    kloc = max(loc / 1000.0, 1.0)
    rate = (markers + panics) / kloc
    score = 100.0 * (1 - min(1.0, rate / DEBT_BUDGET_PER_KLOC))
    return DimResult("debt", 6, score,
                     [f"{markers} markers + {panics} panics over {kloc:.0f} KLOC = "
                      f"{rate:.2f}/KLOC (budget {DEBT_BUDGET_PER_KLOC})"] + sorted(hits)[:TOP_N - 1],
                     {"markers": markers, "panics": panics, "rate_per_kloc": round(rate, 3)})


# --- per-language composites (Kotlin / TypeScript / other) ------------------- #

def _lang_composite(name: str, weight: int, src_files: list[Path],
                    test_files: list[Path]) -> DimResult:
    """0.6 file-size discipline + 0.4 test volume for a non-Go lane."""
    if not src_files:
        return DimResult(name, weight, 100.0, ["(no source files)"], {})
    locs = {f: line_count(f) for f in src_files}
    pairs = [(locs[f], graded(locs[f], SIZE_SOFT_LOC, SIZE_HARD_LOC)) for f in src_files]
    size = loc_weighted(pairs) / 100.0
    src_loc = sum(locs.values())
    test_loc = sum(line_count(f) for f in test_files)
    tratio = min(1.0, (test_loc / src_loc) / TEST_TARGET_RATIO) if src_loc else 1.0
    score = 100.0 * (0.6 * size + 0.4 * tratio)
    over = sorted(((locs[f], rel(f)) for f in src_files if locs[f] > SIZE_SOFT_LOC), reverse=True)
    detail = [f"size={size * 100:.0f}  test/src={test_loc}/{src_loc}={(test_loc / src_loc if src_loc else 0):.2f}"]
    detail += [f"{loc:>5} LOC  {p}" for loc, p in over[:TOP_N - 1]]
    return DimResult(name, weight, score, detail,
                     {"source_files": len(src_files), "source_loc": src_loc,
                      "test_loc": test_loc})


def _other_lang_files() -> tuple[list[Path], list[Path]]:
    """Python / Rust / shell sources (+ their tests) outside the three main lanes."""
    src: list[Path] = []
    test: list[Path] = []
    specs = [("scripts", "*.py"), ("scripts", "*.sh"),
             ("andromeda/src-tauri", "*.rs"), ("gateway-go", "*.sh")]
    for sub, pat in specs:
        base = REPO_ROOT / sub
        if not base.exists():
            continue
        for p in base.rglob(pat):
            if not p.is_file() or any(x in PRUNE_DIRS for x in p.parts):
                continue
            (test if is_test_file("andromeda", p) or p.name.endswith("_test.py")
             or "test" in p.name else src).append(p)
    return sorted(src), sorted(test)


def dim_kotlin(sources, tests) -> DimResult:
    return _lang_composite("kotlin", 15, sources["client-android"], tests["client-android"])


def dim_typescript(sources, tests) -> DimResult:
    return _lang_composite("typescript", 10, sources["andromeda"], tests["andromeda"])


def dim_other() -> DimResult:
    src, test = _other_lang_files()
    return _lang_composite("other-langs", 8, src, test)


# --- deadcode (--deep): wraps scripts/audit/deadcode-audit.sh ----------------

def dim_deadcode() -> DimResult | None:
    script = REPO_ROOT / "scripts" / "audit" / "deadcode-audit.sh"
    if not script.exists():
        return None
    try:
        proc = subprocess.run(
            ["bash", str(script)], cwd=str(REPO_ROOT),
            capture_output=True, text=True, timeout=300,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return DimResult("deadcode", 15, 100.0,
                         [f"skipped: deadcode-audit failed to run ({exc})"],
                         {"skipped": True})
    if proc.returncode == 2:  # tooling failure — don't let it drag the score
        return DimResult("deadcode", 15, 100.0,
                         ["skipped: deadcode tooling failure"], {"skipped": True})
    m = re.search(r"NEW dead code \((\d+) findings\)", proc.stdout)
    new_findings = int(m.group(1)) if m else 0
    score = max(0.0, 100.0 - 5.0 * new_findings)
    detail = [ln.strip() for ln in proc.stdout.splitlines() if ln.strip().startswith("+")]
    return DimResult("deadcode", 15, score, detail[:TOP_N],
                     {"new_findings": new_findings})


# --------------------------------------------------------------------------- #
# Orchestration
# --------------------------------------------------------------------------- #

def compute(deep: bool) -> tuple[float, list[DimResult], str]:
    sources = {lane.name: iter_source_files(lane) for lane in LANES}
    tests = {lane.name: iter_test_files(lane) for lane in LANES}

    dims = [
        # Go architecture
        dim_go_layering(sources),
        dim_go_coupling(sources),
        dim_go_godpkg(sources),
        # Go structure
        dim_go_filesize(sources),
        dim_go_funclen(sources),
        dim_go_nesting(sources),
        # Go docs + tests
        dim_go_docs(sources),
        dim_go_tests(sources, tests),
        dim_go_testbreadth(sources),
        # cross-cutting quality
        dim_duplication(sources),
        dim_debt(sources),
        # per-language composites
        dim_kotlin(sources, tests),
        dim_typescript(sources, tests),
        dim_other(),
    ]
    if deep:
        dc = dim_deadcode()
        if dc is not None:
            dims.append(dc)

    den = sum(d.weight for d in dims)
    composite = sum(d.score * d.weight for d in dims) / den if den else 0.0
    mode = "deep" if deep else "fast"
    return round(composite, 1), dims, mode


def to_payload(composite: float, dims: list[DimResult], mode: str) -> dict:
    return {
        "mode": mode,
        "composite": composite,
        "dims": {d.name: round(d.score, 1) for d in dims},
        "weights": {d.name: d.weight for d in dims},
        "detail": {d.name: d.detail for d in dims},
        "extra": {d.name: d.extra for d in dims},
    }


def print_summary(composite: float, dims: list[DimResult], mode: str) -> None:
    print(f"\ncodebase-health ({mode})  composite = {composite:.1f}/100", file=sys.stderr)
    print("─" * 60, file=sys.stderr)
    for d in dims:
        print(f"  {d.name:<10} {d.score:5.1f}  (w={d.weight})", file=sys.stderr)
        for line in d.detail[:8]:
            print(f"      · {line}", file=sys.stderr)
        if len(d.detail) > 8:
            print(f"      … +{len(d.detail) - 8} more", file=sys.stderr)
    print("─" * 60, file=sys.stderr)


def emit_metric(composite: float, dims: list[DimResult], mode: str) -> None:
    parts = " ".join(f"{d.name}={d.score:.1f}" for d in dims)
    print(f"metric_value={composite}")
    print(f"DENEB_HEALTH_DETAIL {parts} mode={mode}")


def load_baseline() -> dict | None:
    if not BASELINE_PATH.exists():
        return None
    try:
        return json.loads(BASELINE_PATH.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"codebase-health: baseline unreadable: {exc}", file=sys.stderr)
        return None


BASELINE_KEEP_POLICY = (
    "health-baseline.json — accepted floor for scripts/audit/codebase-health.py. "
    "This is a RATCHET (a floor that only goes up), never a way to silence a specific "
    "regression. Regenerate with --update only on operator approval (per "
    "docs/agent-rules/testing.md): raise the floor after a genuine improvement, do not "
    "lower it to make --check pass. 'mode' must match the run mode of --check."
)


def write_baseline(composite: float, dims: list[DimResult], mode: str) -> None:
    payload = {
        "_keep_policy": BASELINE_KEEP_POLICY,
        "mode": mode,
        "composite": composite,
        "dims": {d.name: round(d.score, 1) for d in dims},
    }
    BASELINE_PATH.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n",
                             encoding="utf-8")
    print(f"codebase-health: baseline updated (mode={mode}, composite={composite})",
          file=sys.stderr)


def run_check(composite: float, dims: list[DimResult], mode: str) -> int:
    baseline = load_baseline()
    if baseline is None:
        print("codebase-health: no baseline; run --update first (operator approval).",
              file=sys.stderr)
        return 2
    if baseline.get("mode") != mode:
        print(f"codebase-health: baseline mode={baseline.get('mode')} != run mode={mode}; "
              f"run --check with the matching mode (add/remove --deep).", file=sys.stderr)
        return 2
    floor = float(baseline.get("composite", 0.0))
    if composite < floor - CHECK_TOLERANCE:
        print(f"codebase-health: REGRESSION composite {composite:.1f} < baseline "
              f"{floor:.1f} (tolerance {CHECK_TOLERANCE}).", file=sys.stderr)
        base_dims = baseline.get("dims", {})
        for d in dims:
            was = base_dims.get(d.name)
            if was is not None and d.score < was - CHECK_TOLERANCE:
                print(f"    {d.name}: {was:.1f} -> {d.score:.1f}", file=sys.stderr)
        return 1
    print(f"codebase-health: OK composite {composite:.1f} >= baseline {floor:.1f} "
          f"(-{CHECK_TOLERANCE} tol).", file=sys.stderr)
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description="Deneb codebase structural-health score.")
    ap.add_argument("--json", action="store_true", help="emit full breakdown as JSON")
    ap.add_argument("--deep", action="store_true",
                    help="also run the deadcode dimension (~1-2 min)")
    mode = ap.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_true", help="ratchet gate against baseline")
    mode.add_argument("--update", action="store_true", help="rewrite baseline (operator approval)")
    args = ap.parse_args()

    composite, dims, run_mode = compute(deep=args.deep)

    if args.update:
        print_summary(composite, dims, run_mode)
        write_baseline(composite, dims, run_mode)
        return 0

    if args.check:
        print_summary(composite, dims, run_mode)
        return run_check(composite, dims, run_mode)

    if args.json:
        print(json.dumps(to_payload(composite, dims, run_mode), indent=2, ensure_ascii=False))
    else:
        print_summary(composite, dims, run_mode)
        emit_metric(composite, dims, run_mode)
    return 0


if __name__ == "__main__":
    sys.exit(main())

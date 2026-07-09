#!/usr/bin/env python3
"""codebase-health.py — deterministic structural-health score for the Deneb repo.

Deneb already scores response quality (scripts/dev/quality-test.py), retrieval
(gateway-go/cmd/recall-bench), and skill candidates (genesis validation_engine.go),
all on the same skeleton: fixed inputs -> run -> score -> compare to a baseline ->
emit a grep-able metric line. This harness fills the missing tier — the *codebase's
own structural health* (file-size discipline, layer cohesion, test presence, doc
coverage, dead code) — which no single hard gate (vet/fmt/lint in `make check`)
tracks as a trend, so it rots silently (the 2026-06 audits removed ~8,700 LOC of
such rot). It turns CLAUDE.md's "좁고 깊게: 완성도·응집도 우선" and "파일 ~700 LOC
이하 권장" into one number that review can watch and iterate.sh can optimize.

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
  metric_value=82.4
  DENEB_HEALTH_DETAIL cohesion=95.0 tests=88.1 docs=79.3 size=97.2 mode=fast

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

# 700-LOC soft cap from CLAUDE.md ("파일 ~700 LOC 이하 권장").
LOC_GUIDELINE = 700
# Composite tolerance for --check: a floor that only ratchets up (deadcode-audit style).
CHECK_TOLERANCE = 1.0
# Healthy test-file-to-source-file ratio for lanes without Go's package semantics.
KTTS_TEST_TARGET_RATIO = 0.15
# How many offending items to surface per dimension (actionable, not a dump).
TOP_N = 15

# Directory names pruned everywhere during file discovery.
PRUNE_DIRS = {
    "node_modules", "build", "dist", "target", ".gradle", ".git",
    ".codegraph", ".idea", "__pycache__", "vendor", ".venv", "coverage",
}

# gateway-go layer ranks: higher may import lower; lower importing higher = a
# cohesion violation. Grounded in gateway-go/CLAUDE.md's module map. Unknown
# top-level packages default to mid-rank (2) and are never flagged as sources.
LAYER_RANK = {
    "runtime": 5,   # HTTP server, RPC dispatch, session — top orchestration
    "agentsys": 4,  # agent loop + hooks
    "pipeline": 3,  # chat pipeline: prompt, tools, context
    "domain": 2, "platform": 2, "ai": 2,  # business logic + integrations + LLM
    "core": 1, "infra": 1, "hanja": 1,    # foundational
    "testutil": 0,  # test-only helper (downward from everyone)
}
DEFAULT_RANK = 2

# Sub-package rank overrides ("<top>/<sub>"): a package that lives under a
# high-rank top-level dir but is really a foundational shared kernel — no upward
# deps, imported across layers — and so must not read as an upward violation
# when a lower layer uses it. Checked before the top-level LAYER_RANK.
#   runtime/session: the session state store. Verified it imports nothing under
#   runtime/agentsys/pipeline and is used by pipeline·runtime·platform — shared
#   state, not top orchestration.
SUBPKG_RANK = {
    "runtime/session": 1,
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


# --- size: file-length discipline vs the 700-LOC guideline -------------------

def dim_size(sources: dict[str, list[Path]]) -> DimResult:
    total = 0
    oversized: list[tuple[int, str]] = []
    for files in sources.values():
        for f in files:
            total += 1
            loc = line_count(f)
            if loc > LOC_GUIDELINE:
                oversized.append((loc, rel(f)))
    within = total - len(oversized)
    score = 100.0 * within / total if total else 100.0
    oversized.sort(reverse=True)
    detail = [f"{loc:>5} LOC  {p}" for loc, p in oversized[:TOP_N]]
    return DimResult("size", 15, score, detail,
                     {"total_files": total, "oversized": len(oversized)})


# --- tests: Go package coverage + Kt/TS test-presence proxy ------------------

def _go_leaf_packages(files: list[Path]) -> dict[Path, bool]:
    """Map each Go source directory -> whether it has >=1 *_test.go file."""
    dirs = {f.parent for f in files}
    has_test: dict[Path, bool] = {}
    for d in dirs:
        has_test[d] = any(d.glob("*_test.go"))
    return has_test


def dim_tests(sources: dict[str, list[Path]], tests: dict[str, list[Path]]) -> DimResult:
    lane_fracs: list[tuple[float, int]] = []  # (fraction, weight-by-source-files)
    detail: list[str] = []
    extra: dict = {}

    # Go: fraction of leaf packages carrying at least one test file.
    go_pkgs = _go_leaf_packages(sources["gateway-go"])
    if go_pkgs:
        with_test = sum(1 for v in go_pkgs.values() if v)
        frac = with_test / len(go_pkgs)
        lane_fracs.append((frac, len(sources["gateway-go"])))
        extra["go_pkgs"] = {"total": len(go_pkgs), "with_test": with_test}
        untested = sorted(rel(d) for d, v in go_pkgs.items() if not v)
        detail += [f"no test pkg  {p}" for p in untested[:TOP_N]]

    # Kt / TS: test-file-to-source-file ratio proxy (no per-file package unit).
    for lane in ("client-android", "andromeda"):
        src_n = len(sources[lane])
        test_n = len(tests[lane])
        if src_n == 0:
            continue
        frac = min(1.0, (test_n / src_n) / KTTS_TEST_TARGET_RATIO)
        lane_fracs.append((frac, src_n))
        extra[lane] = {"source_files": src_n, "test_files": test_n,
                       "ratio": round(test_n / src_n, 3)}

    if not lane_fracs:
        return DimResult("tests", 25, 100.0, detail, extra)
    num = sum(frac * w for frac, w in lane_fracs)
    den = sum(w for _, w in lane_fracs)
    return DimResult("tests", 25, 100.0 * num / den, detail, extra)


# --- docs: Go package-comment coverage + module CLAUDE.md presence -----------

# A package is "documented" if any of its files opens with a Go doc comment —
# `// Package x …` for libraries or `// Command x …` for cmd/ mains (Go convention).
_PKG_COMMENT_RE = re.compile(r"^// (Package|Command) \w", re.MULTILINE)

TOP_MODULE_DIRS = [
    REPO_ROOT / "gateway-go",
    REPO_ROOT / "client-android" / "app",
    REPO_ROOT / "andromeda",
    REPO_ROOT / "skills",
]


def _pkg_has_doc(pkg_dir: Path) -> bool:
    for f in sorted(pkg_dir.glob("*.go")):
        if f.name.endswith("_test.go"):
            continue
        try:
            head = f.read_text(encoding="utf-8", errors="replace")[:4096]
        except OSError:
            continue
        if _PKG_COMMENT_RE.search(head):
            return True
    return False


def dim_docs(sources: dict[str, list[Path]]) -> DimResult:
    go_dirs = sorted({f.parent for f in sources["gateway-go"]})
    documented = [d for d in go_dirs if _pkg_has_doc(d)]
    go_frac = len(documented) / len(go_dirs) if go_dirs else 1.0

    present = [m for m in TOP_MODULE_DIRS if (m / "CLAUDE.md").exists()]
    mod_frac = len(present) / len(TOP_MODULE_DIRS) if TOP_MODULE_DIRS else 1.0

    score = 100.0 * (0.85 * go_frac + 0.15 * mod_frac)
    undoc = sorted(rel(d) for d in go_dirs if d not in set(documented))
    detail = [f"no pkg comment  {p}" for p in undoc[:TOP_N]]
    missing_mod = [rel(m) for m in TOP_MODULE_DIRS if m not in present]
    detail += [f"missing CLAUDE.md  {p}" for p in missing_mod]
    return DimResult("docs", 20, score, detail,
                     {"go_pkgs": len(go_dirs), "documented": len(documented),
                      "modules_with_claude_md": len(present)})


# --- cohesion: gateway-go layer-rank import violations -----------------------

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


def _go_imports(path: Path) -> list[str]:
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return []
    imports: list[str] = []
    for block in _IMPORT_BLOCK_RE.findall(text):
        imports += _QUOTED_RE.findall(block)
    imports += _IMPORT_SINGLE_RE.findall(text)
    return imports


def _top_pkg(internal_path: str) -> str:
    # internal_path like ".../internal/pipeline/chat/foo" -> "pipeline"
    return internal_path.split("/", 1)[0] if internal_path else ""


def _pkg_rank(internal_rel: str) -> int:
    # internal_rel is a path relative to internal/ (e.g. "runtime/session/manager.go"
    # or "pipeline/chat"). A "<top>/<sub>" kernel override wins over the top-level rank.
    parts = internal_rel.split("/")
    if len(parts) >= 2 and f"{parts[0]}/{parts[1]}" in SUBPKG_RANK:
        return SUBPKG_RANK[f"{parts[0]}/{parts[1]}"]
    return LAYER_RANK.get(parts[0] if parts else "", DEFAULT_RANK)


def dim_cohesion(sources: dict[str, list[Path]]) -> DimResult:
    module = _go_module_path()
    if not module:
        return DimResult("cohesion", 25, 100.0, ["skipped: go.mod module path not found"])
    internal_prefix = module + "/internal/"

    total_edges = 0
    violations: dict[tuple[str, str], int] = {}
    internal_root = REPO_ROOT / "gateway-go" / "internal"
    for f in sources["gateway-go"]:
        try:
            src_rel = f.relative_to(internal_root)
        except ValueError:
            continue  # only internal/ participates in layering
        src_top = src_rel.parts[0]
        src_rank = _pkg_rank("/".join(src_rel.parts))
        if src_top == "testutil":
            continue
        for imp in _go_imports(f):
            if not imp.startswith(internal_prefix):
                continue
            dst_internal = imp[len(internal_prefix):]
            dst_top = _top_pkg(dst_internal)
            if dst_top == src_top:
                continue  # intra-package-family is lateral, always fine
            total_edges += 1
            dst_rank = _pkg_rank(dst_internal)
            if dst_rank > src_rank:
                violations[(src_top, dst_top)] = violations.get((src_top, dst_top), 0) + 1

    viol_edges = sum(violations.values())
    score = 100.0 * (1 - viol_edges / total_edges) if total_edges else 100.0
    ranked = sorted(violations.items(), key=lambda kv: (-kv[1], kv[0]))
    detail = [f"{n:>4}x  {src} -> {dst}  (rank {LAYER_RANK.get(src, DEFAULT_RANK)}"
              f" imports {LAYER_RANK.get(dst, DEFAULT_RANK)})"
              for (src, dst), n in ranked[:TOP_N]]
    return DimResult("cohesion", 25, score, detail,
                     {"internal_edges": total_edges, "violation_edges": viol_edges})


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
        dim_cohesion(sources),
        dim_tests(sources, tests),
        dim_docs(sources),
        dim_size(sources),
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

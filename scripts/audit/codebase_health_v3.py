#!/usr/bin/env python3
"""Health Bench 3.0 orchestration and CLI."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path
from typing import Sequence

AUDIT_DIR = Path(__file__).resolve().parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

from health_v3.baseline import (  # noqa: E402
    BaselineError,
    BaselineRegressionError,
    check as check_baseline,
    load as load_baseline,
    update as update_baseline,
)
from health_v3.fitness import append_history, evaluate_fitness  # noqa: E402
from health_v3.model import RUBRIC_VERSION, Report  # noqa: E402
from health_v3.report import render_human, render_markdown  # noqa: E402
from health_v3.runtime import evaluate_runtime  # noqa: E402
from health_v3.structure import evaluate_structure  # noqa: E402

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_BASELINE = REPO_ROOT / "scripts" / "audit" / "health-v3-baseline.json"
DEFAULT_SNAPSHOT = REPO_ROOT / "scripts" / "audit" / "health-v3-snapshot.json"


class HealthToolError(RuntimeError):
    """Required repository or execution evidence could not be collected."""


def _revision(root: Path) -> str:
    proc = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=root, text=True, capture_output=True, check=False
    )
    if proc.returncode != 0:
        raise HealthToolError(proc.stderr.strip() or "could not resolve git revision")
    revision = proc.stdout.strip()
    dirty = subprocess.run(
        ["git", "status", "--porcelain", "--untracked-files=all"],
        cwd=root,
        text=True,
        capture_output=True,
        check=False,
    )
    if dirty.returncode != 0:
        raise HealthToolError(dirty.stderr.strip() or "could not inspect worktree state")
    return revision + ("+dirty" if dirty.stdout else "")


def collect_report(
    root: Path = REPO_ROOT,
    *,
    profile: str = "fast",
    refresh_runtime_cache: bool = False,
) -> Report:
    structure = evaluate_structure(root, profile=profile)
    runtime = evaluate_runtime(root, profile=profile, refresh_cache=refresh_runtime_cache)
    fitness = evaluate_fitness(root)
    evidence = list(structure.evidence) + list(runtime.evidence) + list(fitness.evidence)
    required_missing = [item for item in runtime.evidence if item.required and item.status != "measured"]
    if required_missing and profile == "fast":
        names = ", ".join(item.name for item in required_missing)
        raise HealthToolError(
            f"required runtime evidence unavailable: {names}. "
            "Refresh with --refresh-runtime-cache on the gateway host "
            "(writes ~/.deneb/data/health-v3-runtime-cache.json) or restore "
            "the checked-in seed scripts/audit/health-v3-runtime-cache.json"
        )
    return Report(
        profile=profile,
        revision=_revision(root),
        domains=[structure, runtime, fitness],
        evidence=evidence,
        readiness={},
    )


def _band(value: str) -> tuple[float, float]:
    try:
        low_raw, high_raw = value.split(":", 1)
        low, high = float(low_raw), float(high_raw)
    except (ValueError, TypeError) as exc:
        raise argparse.ArgumentTypeError("band must be LOW:HIGH") from exc
    if not 0 <= low <= high <= 100:
        raise argparse.ArgumentTypeError("band must satisfy 0 <= LOW <= HIGH <= 100")
    return low, high


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Deneb Health Bench 3.0")
    parser.add_argument("--profile", choices=("fast", "deep"), default="fast")
    parser.add_argument("--deep", action="store_true", help="alias for --profile deep")
    parser.add_argument("--format", choices=("human", "json", "markdown"), default="human")
    parser.add_argument("--json", action="store_true", help="alias for --format json")
    parser.add_argument("--json-out", type=Path)
    parser.add_argument("--top", type=int, default=5)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--baseline", type=Path, default=DEFAULT_BASELINE)
    parser.add_argument("--update-baseline", action="store_true")
    parser.add_argument("--write-baseline", type=Path)
    parser.add_argument("--write-snapshot", type=Path, nargs="?", const=DEFAULT_SNAPSHOT)
    parser.add_argument("--append-history", action="store_true")
    parser.add_argument("--refresh-runtime-cache", action="store_true")
    parser.add_argument("--expect-band", type=_band)
    parser.add_argument("--root", type=Path, default=REPO_ROOT)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = _parser()
    args = parser.parse_args(argv)
    if args.deep:
        args.profile = "deep"
    if args.json:
        args.format = "json"
    if args.top < 0:
        parser.error("--top must not be negative")
    try:
        report = collect_report(
            args.root,
            profile=args.profile,
            refresh_runtime_cache=args.refresh_runtime_cache or args.profile == "deep",
        )
        if args.expect_band and not args.expect_band[0] <= report.overall <= args.expect_band[1]:
            raise HealthToolError(
                f"score {report.overall:.1f} is outside expected migration band "
                f"{args.expect_band[0]:.1f}:{args.expect_band[1]:.1f}; review the rubric"
            )

        baseline_target = args.write_baseline or (args.baseline if args.update_baseline else None)
        if baseline_target:
            update_baseline(
                baseline_target,
                report,
                provenance={"reason": "health-bench-3.0 baseline write", "migration": "initial-or-update"},
            )

        payload = report.to_dict()
        if args.write_snapshot is not None:
            args.write_snapshot.parent.mkdir(parents=True, exist_ok=True)
            args.write_snapshot.write_text(
                json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n",
                encoding="utf-8",
            )
        if args.append_history:
            append_history(args.root, payload)

        exit_code = 0
        check_lines: list[str] = []
        if args.check:
            result = check_baseline(report, load_baseline(args.baseline))
            check_lines = result.format_lines()
            if not result.ok:
                exit_code = 1

        canonical = json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n"
        if args.json_out:
            args.json_out.parent.mkdir(parents=True, exist_ok=True)
            args.json_out.write_text(canonical, encoding="utf-8")
        if args.format == "json":
            sys.stdout.write(canonical)
        elif args.format == "markdown":
            sys.stdout.write(render_markdown(report, top=args.top))
        else:
            sys.stdout.write(render_human(report, top=args.top))
            for line in check_lines:
                print(line)
            print(
                f"DENEB_HEALTH_V3 score={report.overall:.1f} profile={report.profile} "
                f"rubric={RUBRIC_VERSION} "
                f"structure={payload['score']['domains']['structure']:.1f} "
                f"runtime={payload['score']['domains']['runtime']:.1f} "
                f"fitness={payload['score']['domains']['fitness']:.1f}"
            )
        if args.format != "human" and check_lines:
            for line in check_lines:
                print(line, file=sys.stderr)
        return exit_code
    except (BaselineError, BaselineRegressionError, HealthToolError, OSError, ValueError) as exc:
        print(f"health-v3: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())

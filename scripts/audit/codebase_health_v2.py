#!/usr/bin/env python3
"""Health Bench 2.0 orchestration and CLI.

V2 is deliberately independent of ``codebase-health.py``.  The old metric is
kept for compatibility; this rubric measures maintainability evidence and
change risk rather than attempting to translate the old score.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from concurrent.futures import ProcessPoolExecutor
from pathlib import Path
from typing import Callable, Sequence

# Support both the dashed executable and importlib-based unit tests without
# requiring scripts/audit to be installed as a package.
AUDIT_DIR = Path(__file__).resolve().parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

from health_v2 import architecture, delivery, operations, testing
from health_v2.baseline import (
    BaselineError,
    BaselineRegressionError,
    check as check_baseline,
    load as load_baseline,
    migrate as migrate_baseline,
    update as update_baseline,
)
from health_v2.model import Evidence, Pillar, Report, RUBRIC_VERSION
from health_v2.report import group_interventions, render_human, render_markdown

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_BASELINE = REPO_ROOT / "scripts" / "audit" / "health-v2-baseline.json"
READINESS_CHECKS = ("go-format", "go-vet", "go-lint", "go-test", "go-race")


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


def _run(command: list[str], cwd: Path, *, timeout: int) -> tuple[bool | None, str]:
    try:
        proc = subprocess.run(
            command,
            cwd=cwd,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            timeout=timeout,
            env={**os.environ, "CGO_ENABLED": os.environ.get("CGO_ENABLED", "0")},
        )
    except FileNotFoundError as exc:
        return None, f"tool unavailable: {exc.filename}"
    except subprocess.TimeoutExpired:
        return False, f"timed out after {timeout}s"
    output = " ".join(proc.stdout.strip().split())
    if len(output) > 600:
        output = output[-600:]
    return proc.returncode == 0, output or f"exit {proc.returncode}"


def _deep_evidence(root: Path) -> tuple[dict[str, bool | None], list[Evidence], float | None]:
    gateway = root / "gateway-go"
    readiness: dict[str, bool | None] = {}
    evidence: list[Evidence] = []
    commands = {
        "go-format": (["make", "go-fmt"], root, 300),
        "go-vet": (["go", "vet", "./..."], gateway, 600),
    }
    for name, (command, cwd, timeout) in commands.items():
        state, detail = _run(command, cwd, timeout=timeout)
        readiness[name] = state
        status = "unavailable" if state is None else "measured"
        evidence.append(Evidence(name, status, detail, required=True))

    version_state, version_detail = _run(["golangci-lint", "version"], gateway, timeout=60)
    if version_state is None or not re.search(r"\bversion\s+v?2\.5(?:\.|\b)", version_detail):
        readiness["go-lint"] = None
        evidence.append(
            Evidence(
                "go-lint",
                "unavailable",
                f"expected golangci-lint 2.5.x (CI pin), got: {version_detail}",
                required=True,
            )
        )
    else:
        state, detail = _run(["golangci-lint", "run", "./..."], gateway, timeout=900)
        readiness["go-lint"] = state
        evidence.append(Evidence("go-lint", "measured", detail, required=True))

    coverage: float | None = None
    with tempfile.TemporaryDirectory(prefix="deneb-health-v2-") as folder:
        profile = Path(folder) / "coverage.out"
        command = [
            "go", "test", "-count=1", "-shuffle=1700000001",
            f"-coverprofile={profile}", "-covermode=atomic", "./...",
        ]
        state, detail = _run(command, gateway, timeout=1200)
        readiness["go-test"] = state
        evidence.append(
            Evidence("go-test", "unavailable" if state is None else "measured", detail, required=True)
        )
        if state and profile.exists():
            cover_state, cover_detail = _run(
                ["go", "tool", "cover", f"-func={profile}"], gateway, timeout=120
            )
            if cover_state:
                for line in cover_detail.split(" "):
                    if line.endswith("%"):
                        try:
                            coverage = float(line[:-1])
                        except ValueError:
                            pass
                evidence.append(
                    Evidence(
                        "go-statement-coverage",
                        "measured" if coverage is not None else "unavailable",
                        f"{coverage:.1f}%" if coverage is not None else "could not parse total coverage",
                        required=True,
                    )
                )
            else:
                evidence.append(Evidence("go-statement-coverage", "unavailable", cover_detail, required=True))

    # Race uses its own build mode; report a missing compiler/toolchain as
    # unavailable rather than granting success.
    old_cgo = os.environ.get("CGO_ENABLED")
    os.environ["CGO_ENABLED"] = "1"
    try:
        state, detail = _run(
            ["go", "test", "-race", "-count=1", "-shuffle=1700000001", "./..."],
            gateway,
            timeout=1800,
        )
    finally:
        if old_cgo is None:
            os.environ.pop("CGO_ENABLED", None)
        else:
            os.environ["CGO_ENABLED"] = old_cgo
    readiness["go-race"] = state
    evidence.append(Evidence("go-race", "unavailable" if state is None else "measured", detail, required=True))
    return readiness, evidence, coverage


def _apply_deep_delivery(pillars: list[Pillar], readiness: dict[str, bool | None], coverage: float | None) -> None:
    delivery_pillar = next((pillar for pillar in pillars if pillar.id == "delivery-confidence"), None)
    if delivery_pillar is None:
        return
    scored_checks = ("go-vet", "go-lint", "go-test", "go-race")
    present_checks = [name for name in scored_checks if name in readiness]
    checks = [
        100.0 if readiness[name] else 0.0
        for name in present_checks
    ]
    if not checks:
        return
    execution = sum(checks) / len(checks)
    static_score = delivery_pillar.score
    delivery_pillar.score = 0.6 * static_score + 0.4 * execution
    delivery_pillar.metrics["deep_execution_score"] = round(execution, 1)
    delivery_pillar.metrics["deep_execution_checks"] = present_checks
    delivery_pillar.metrics["static_delivery_design_score"] = round(static_score, 1)
    if coverage is not None:
        delivery_pillar.metrics["go_statement_coverage"] = {
            "scored": False,
            "value": round(coverage, 1),
            "reason": "raw statement coverage does not prove behavior or oracle quality",
        }


def _evaluate_module(
    evaluator: Callable[[Path], tuple[list[Pillar], list[Evidence]]], root: Path
) -> tuple[list[Pillar], list[Evidence]]:
    """Pickle-safe evaluator adapter for the fast profile process pool."""
    return evaluator(root)


def collect_report(
    root: Path = REPO_ROOT,
    *,
    profile: str = "fast",
    readiness_passed: set[str] | None = None,
    readiness_failed: set[str] | None = None,
) -> Report:
    pillars: list[Pillar] = []
    evidence: list[Evidence] = []
    evaluators = (architecture.evaluate, operations.evaluate, testing.evaluate)
    # The parsers are CPU-heavy as well as read-heavy, so threads serialize on
    # the GIL and worsen WSL filesystem contention. Separate processes keep the
    # fast path near the slowest evaluator while the lightweight CI scan stays
    # in the parent. Future collection order remains deterministic.
    with ProcessPoolExecutor(max_workers=len(evaluators)) as pool:
        futures = [pool.submit(_evaluate_module, evaluator, root) for evaluator in evaluators]
        delivery_result = delivery.evaluate(root)
        results = [future.result() for future in futures]
    results.append(delivery_result)
    for module_pillars, module_evidence in results:
        pillars.extend(module_pillars)
        evidence.extend(module_evidence)

    readiness: dict[str, bool | None] = {
        name: True if name in (readiness_passed or set()) else False if name in (readiness_failed or set()) else None
        for name in READINESS_CHECKS
    }
    if profile == "deep":
        readiness, deep_evidence, coverage = _deep_evidence(root)
        evidence.extend(deep_evidence)
        _apply_deep_delivery(pillars, readiness, coverage)

    report = Report(
        profile=profile,
        revision=_revision(root),
        pillars=pillars,
        evidence=evidence,
        readiness=readiness,
    )
    report.interventions = group_interventions(report)
    return report


def _band(value: str) -> tuple[float, float]:
    try:
        low_raw, high_raw = value.split(":", 1)
        low, high = float(low_raw), float(high_raw)
    except (ValueError, TypeError) as exc:
        raise argparse.ArgumentTypeError("band must be LOW:HIGH") from exc
    if not 0 <= low <= high <= 100:
        raise argparse.ArgumentTypeError("band must satisfy 0 <= LOW <= HIGH <= 100")
    return low, high


def _read_v1_provenance(path: Path) -> dict[str, object]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise BaselineError(f"could not read v1 migration source {path}: {exc}") from exc
    score = payload.get("composite", payload.get("score", payload.get("metric_value")))
    return {
        "migration": "v1-to-v2-remeasurement",
        "v1_baseline_path": path.relative_to(REPO_ROOT).as_posix() if path.is_relative_to(REPO_ROOT) else str(path),
        "v1_baseline_score": score,
        "note": "Scores are not converted or comparable across rubric versions.",
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Deneb Health Bench 2.0")
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
    parser.add_argument("--migrate-v1", type=Path)
    parser.add_argument(
        "--migrate-rubric",
        type=Path,
        help="explicitly remeasure an existing v2 baseline after a rubric version change",
    )
    parser.add_argument(
        "--migration-reason",
        help="reviewed reason recorded in provenance for an explicit rubric migration",
    )
    parser.add_argument("--expect-band", type=_band)
    parser.add_argument("--readiness-passed", action="append", choices=READINESS_CHECKS, default=[])
    parser.add_argument("--readiness-failed", action="append", choices=READINESS_CHECKS, default=[])
    parser.add_argument("--require-readiness", action="store_true")
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
    if args.migrate_v1 and args.migrate_rubric:
        parser.error("--migrate-v1 and --migrate-rubric are mutually exclusive")
    if args.migrate_rubric and not args.expect_band:
        parser.error("--migrate-rubric requires --expect-band")
    if args.migrate_rubric and not args.migration_reason:
        parser.error("--migrate-rubric requires --migration-reason")
    if args.migration_reason and not args.migrate_rubric:
        parser.error("--migration-reason requires --migrate-rubric")
    if args.migrate_rubric and not (args.write_baseline or args.update_baseline):
        parser.error("--migrate-rubric requires --write-baseline or --update-baseline")
    overlap = set(args.readiness_passed) & set(args.readiness_failed)
    if overlap:
        parser.error(f"readiness cannot both pass and fail: {', '.join(sorted(overlap))}")
    try:
        report = collect_report(
            profile=args.profile,
            readiness_passed=set(args.readiness_passed),
            readiness_failed=set(args.readiness_failed),
        )
        required_missing = [item for item in report.evidence if item.required and item.status != "measured"]
        if required_missing:
            names = ", ".join(item.name for item in required_missing)
            raise HealthToolError(f"required evidence unavailable: {names}")
        if args.expect_band and not args.expect_band[0] <= report.overall <= args.expect_band[1]:
            raise HealthToolError(
                f"score {report.overall:.1f} is outside expected migration band "
                f"{args.expect_band[0]:.1f}:{args.expect_band[1]:.1f}; review the rubric instead of forcing a baseline"
            )

        provenance = _read_v1_provenance(args.migrate_v1.resolve()) if args.migrate_v1 else None
        baseline_target = args.write_baseline or (args.baseline if args.update_baseline else None)
        if baseline_target:
            if args.migrate_rubric:
                migrate_baseline(
                    baseline_target,
                    report,
                    args.migrate_rubric,
                    provenance={"reason": args.migration_reason},
                )
            else:
                update_baseline(baseline_target, report, provenance=provenance)

        exit_code = 0
        check_lines: list[str] = []
        if args.check:
            result = check_baseline(report, load_baseline(args.baseline))
            check_lines = result.format_lines()
            if not result.ok:
                exit_code = 1
        if args.require_readiness and not report.healthy:
            check_lines.append("REGRESSION: executable readiness is failed or unmeasured")
            exit_code = 1

        payload = report.to_dict()
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
                f"DENEB_HEALTH_V2 score={report.overall:.1f} profile={report.profile} "
                f"rubric={RUBRIC_VERSION} healthy={str(report.healthy).lower()}"
            )
        if args.format != "human" and check_lines:
            for line in check_lines:
                print(line, file=sys.stderr)
        return exit_code
    except (BaselineError, BaselineRegressionError, HealthToolError, OSError, ValueError) as exc:
        print(f"health-v2: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())

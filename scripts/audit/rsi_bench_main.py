#!/usr/bin/env python3
"""RSI Bench orchestration and CLI."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Sequence

AUDIT_DIR = Path(__file__).resolve().parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

from rsi_bench.baseline import (  # noqa: E402
    BaselineError,
    BaselineRegressionError,
    check as check_baseline,
    load as load_baseline,
    update as update_baseline,
)
from rsi_bench.cache import (  # noqa: E402
    CacheError,
    default_cache_path,
    load_cache,
    refresh_cache,
)
from rsi_bench.model import RUBRIC_VERSION, Report  # noqa: E402
from rsi_bench.process import evaluate_process  # noqa: E402
from rsi_bench.report import render_human, render_markdown  # noqa: E402
from rsi_bench.token_economics import load_token_economics  # noqa: E402
from rsi_bench.utility import evaluate_utility  # noqa: E402

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_BASELINE = REPO_ROOT / "scripts" / "audit" / "rsi-bench-baseline.json"
DEFAULT_SNAPSHOT = REPO_ROOT / "scripts" / "audit" / "rsi-bench-snapshot.json"


class RSIBenchError(RuntimeError):
    """Required evidence could not be collected."""


def _revision(root: Path) -> str:
    proc = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=root, text=True, capture_output=True, check=False
    )
    if proc.returncode != 0:
        raise RSIBenchError(proc.stderr.strip() or "could not resolve git revision")
    revision = proc.stdout.strip()
    dirty = subprocess.run(
        ["git", "status", "--porcelain", "--untracked-files=all"],
        cwd=root,
        text=True,
        capture_output=True,
        check=False,
    )
    if dirty.returncode != 0:
        raise RSIBenchError(dirty.stderr.strip() or "could not inspect worktree state")
    return revision + ("+dirty" if dirty.stdout else "")


def collect_report(
    root: Path = REPO_ROOT,
    *,
    profile: str = "fast",
    refresh: bool = False,
    data_dir: Path | None = None,
) -> Report:
    cache_path = default_cache_path(root)
    cache = None
    if refresh or profile == "deep":
        try:
            cache = refresh_cache(cache_path, root=root)
        except CacheError as exc:
            # deep prefers fresh cache but can fall back to ledger-only if refresh fails
            if profile == "deep":
                cache = load_cache(cache_path, required=False)
            if cache is None and profile == "fast":
                raise RSIBenchError(str(exc)) from exc
    else:
        cache = load_cache(cache_path, required=False)

    process = evaluate_process(root, cache=cache, data=data_dir)
    utility = evaluate_utility(root, cache=cache, data=data_dir)
    evidence = list(process.evidence) + list(utility.evidence)
    return Report(
        profile=profile,
        revision=_revision(root),
        domains=[process, utility],
        evidence=evidence,
        readiness={},
    )


def append_history(root: Path, report_dict: dict) -> None:
    path = root / "scripts" / "audit" / "rsi-bench-history.jsonl"
    row = {
        "ts": datetime.now(timezone.utc).isoformat(),
        "overall": report_dict.get("score", {}).get("overall"),
        "score": report_dict.get("score"),
        "revision": report_dict.get("revision"),
    }
    with path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(row, sort_keys=True) + "\n")


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
    parser = argparse.ArgumentParser(description="Deneb RSI Bench (process + utility)")
    parser.add_argument("--profile", choices=("fast", "deep"), default="fast")
    parser.add_argument("--deep", action="store_true", help="alias for --profile deep")
    parser.add_argument("--format", choices=("human", "json", "markdown"), default="human")
    parser.add_argument("--json", action="store_true", help="alias for --format json")
    parser.add_argument("--json-out", type=Path)
    parser.add_argument("--top", type=int, default=5)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--baseline", type=Path, default=DEFAULT_BASELINE)
    parser.add_argument("--update-baseline", action="store_true")
    parser.add_argument("--migrate-rubric", action="store_true", help="replace baseline on rubric bump")
    parser.add_argument("--write-baseline", type=Path)
    parser.add_argument("--write-snapshot", type=Path, nargs="?", const=DEFAULT_SNAPSHOT)
    parser.add_argument("--append-history", action="store_true")
    parser.add_argument("--refresh-cache", action="store_true")
    parser.add_argument("--expect-band", type=_band)
    parser.add_argument("--data-dir", type=Path, help="override DENEB_DATA_DIR")
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
            refresh=args.refresh_cache or args.profile == "deep",
            data_dir=args.data_dir,
        )
        if args.expect_band and not args.expect_band[0] <= report.overall <= args.expect_band[1]:
            raise RSIBenchError(
                f"score {report.overall:.1f} is outside expected migration band "
                f"{args.expect_band[0]:.1f}:{args.expect_band[1]:.1f}; review the rubric"
            )

        baseline_target = args.write_baseline or (args.baseline if args.update_baseline else None)
        if baseline_target:
            update_baseline(
                baseline_target,
                report,
                provenance={
                    "reason": "rsi-bench-1.2 baseline write",
                    "migration": "initial-or-update",
                },
                migrate_rubric=args.migrate_rubric,
            )

        payload = report.to_dict()
        # Advisory token-economics sidecar (arXiv:2607.06906): tau/CPM per completed
        # task from the runtime's own agent-logs, so the loop can SEE whether it is
        # token-maxing. Deliberately outside the scored Report — never enters a
        # domain score, the ratchet, confidence, or the baseline check.
        token_economics = load_token_economics()
        payload["token_economics"] = token_economics.to_dict()
        if args.write_snapshot is not None:
            args.write_snapshot.parent.mkdir(parents=True, exist_ok=True)
            args.write_snapshot.write_text(
                json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n",
                encoding="utf-8",
            )
        if args.append_history:
            append_history(args.root, payload)

        if args.format == "json":
            text = json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n"
        elif args.format == "markdown":
            text = render_markdown(report, top=args.top)
        else:
            text = render_human(report, top=args.top)
            text += (
                f"DENEB_RSI_BENCH score={report.overall} profile={report.profile} "
                f"rubric={RUBRIC_VERSION} process={payload['score']['domains']['process']} "
                f"utility={payload['score']['domains']['utility']}\n"
            )
            # Next to the score, never inside it: CPM/tau make token-maxing visible.
            text += f"DENEB_RSI_TOKEN_ECONOMICS {token_economics.summary()} [advisory]\n"

        if args.json_out:
            args.json_out.write_text(
                json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n",
                encoding="utf-8",
            )
        sys.stdout.write(text)

        if args.check:
            baseline = load_baseline(args.baseline)
            result = check_baseline(report, baseline)
            for line in result.format_lines():
                print(line)
            return 0 if result.ok else 1
        return 0
    except (RSIBenchError, BaselineError, BaselineRegressionError, CacheError) as exc:
        print(f"RSI Bench error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())

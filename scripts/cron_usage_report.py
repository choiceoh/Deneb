#!/usr/bin/env python3
"""Cron usage report: parses JSONL run logs and aggregates token usage by job/model."""

import json
import math
import os
import sys
from datetime import datetime, timezone
from pathlib import Path


def parse_args(argv: list[str]) -> dict[str, str | bool]:
    args: dict[str, str | bool] = {}
    i = 0
    while i < len(argv):
        a = argv[i]
        if not a.startswith("--"):
            i += 1
            continue
        key = a[2:]
        nxt = argv[i + 1] if i + 1 < len(argv) else None
        if nxt and not nxt.startswith("--"):
            args[key] = nxt
            i += 2
        else:
            args[key] = True
            i += 1
    return args


def usage_and_exit(code: int) -> None:
    sys.stderr.write(
        "\n".join(
            [
                "cron_usage_report.py",
                "",
                "Required (choose one):",
                "  --store <path-to-cron-store-json>   (derive runs dir as dirname(store)/runs)",
                "  --runsDir <path-to-runs-dir>",
                "",
                "Time window:",
                "  --hours <n>        (default 24)",
                "  --from <iso>       (overrides --hours)",
                "  --to <iso>         (default now)",
                "",
                "Filters:",
                "  --jobId <id>",
                "  --model <name>",
                "",
                "Output:",
                "  --json             (emit JSON)",
                "",
            ]
        )
    )
    sys.exit(code)


def list_jsonl_files(directory: str) -> list[Path]:
    d = Path(directory)
    if not d.is_dir():
        return []
    return sorted(p for p in d.iterdir() if p.is_file() and p.suffix == ".jsonl")


def safe_parse_line(line: str) -> dict | None:
    try:
        obj = json.loads(line)
    except (json.JSONDecodeError, ValueError):
        return None
    if not isinstance(obj, dict):
        return None
    if obj.get("action") != "finished":
        return None
    ts = obj.get("ts")
    if not isinstance(ts, (int, float)) or not math.isfinite(ts):
        return None
    job_id = obj.get("jobId", "")
    if not isinstance(job_id, str) or not job_id.strip():
        return None
    return obj


def fmt_int(n: int | float) -> str:
    return f"{int(n):,}"


def iso_to_ms(iso_str: str) -> float:
    dt = datetime.fromisoformat(iso_str.replace("Z", "+00:00"))
    return dt.timestamp() * 1000


def ms_to_iso(ms: float) -> str:
    return datetime.fromtimestamp(ms / 1000, tz=timezone.utc).isoformat()


def main() -> None:
    args = parse_args(sys.argv[1:])
    store = args.get("store") if isinstance(args.get("store"), str) else None
    runs_dir_arg = args.get("runsDir") if isinstance(args.get("runsDir"), str) else None

    if runs_dir_arg:
        runs_dir = runs_dir_arg
    elif store:
        runs_dir = os.path.join(os.path.dirname(os.path.abspath(store)), "runs")
    else:
        usage_and_exit(2)
        return  # unreachable

    hours_raw = args.get("hours")
    hours = float(hours_raw) if isinstance(hours_raw, str) else 24.0

    now_ms = datetime.now(tz=timezone.utc).timestamp() * 1000
    to_arg = args.get("to")
    to_ms = iso_to_ms(to_arg) if isinstance(to_arg, str) else now_ms

    from_arg = args.get("from")
    if isinstance(from_arg, str):
        from_ms = iso_to_ms(from_arg)
    else:
        safe_hours = max(1.0, hours if math.isfinite(hours) else 24.0)
        from_ms = to_ms - safe_hours * 60 * 60 * 1000

    job_id_val = args.get("jobId")
    filter_job_id = job_id_val.strip() if isinstance(job_id_val, str) else ""
    model_val = args.get("model")
    filter_model = model_val.strip() if isinstance(model_val, str) else ""
    as_json = args.get("json") is True

    files = list_jsonl_files(runs_dir)

    # {jobId: {jobId, runs, models: {model: {...}}, input_tokens, ...}}
    totals_by_job: dict[str, dict] = {}

    for file_path in files:
        try:
            raw = file_path.read_text(encoding="utf-8")
        except OSError:
            continue
        if not raw.strip():
            continue

        for line in raw.split("\n"):
            entry = safe_parse_line(line.strip())
            if entry is None:
                continue
            ts = entry["ts"]
            if ts < from_ms or ts > to_ms:
                continue
            job_id = entry["jobId"]
            if filter_job_id and job_id != filter_job_id:
                continue
            model = (entry.get("model") or "<unknown>").strip() or "<unknown>"
            if filter_model and model != filter_model:
                continue

            usage = entry.get("usage") or {}
            has_usage = any(
                usage.get(k) is not None
                for k in ("total_tokens", "input_tokens", "output_tokens")
            )

            if job_id not in totals_by_job:
                totals_by_job[job_id] = {
                    "jobId": job_id,
                    "runs": 0,
                    "models": {},
                    "input_tokens": 0,
                    "output_tokens": 0,
                    "total_tokens": 0,
                    "missingUsageRuns": 0,
                }
            job_agg = totals_by_job[job_id]
            job_agg["runs"] += 1

            if model not in job_agg["models"]:
                job_agg["models"][model] = {
                    "model": model,
                    "runs": 0,
                    "input_tokens": 0,
                    "output_tokens": 0,
                    "total_tokens": 0,
                    "missingUsageRuns": 0,
                }
            model_agg = job_agg["models"][model]
            model_agg["runs"] += 1

            if not has_usage:
                job_agg["missingUsageRuns"] += 1
                model_agg["missingUsageRuns"] += 1
                continue

            inp_raw = usage.get("input_tokens")
            out_raw = usage.get("output_tokens")
            total_raw = usage.get("total_tokens")
            inp = max(0, int(inp_raw if inp_raw is not None else 0))
            out = max(0, int(out_raw if out_raw is not None else 0))
            total = max(0, int(total_raw if total_raw is not None else (inp + out)))

            job_agg["input_tokens"] += inp
            job_agg["output_tokens"] += out
            job_agg["total_tokens"] += total
            model_agg["input_tokens"] += inp
            model_agg["output_tokens"] += out
            model_agg["total_tokens"] += total

    rows = sorted(
        [
            {**job, "models": sorted(job["models"].values(), key=lambda m: -m["total_tokens"])}
            for job in totals_by_job.values()
        ],
        key=lambda r: -r["total_tokens"],
    )

    if as_json:
        sys.stdout.write(
            json.dumps(
                {
                    "from": ms_to_iso(from_ms),
                    "to": ms_to_iso(to_ms),
                    "runsDir": runs_dir,
                    "jobs": rows,
                },
                indent=2,
            )
            + "\n"
        )
        return

    print("Cron usage report")
    print(f"  runsDir: {runs_dir}")
    print(f"  window: {ms_to_iso(from_ms)} → {ms_to_iso(to_ms)}")
    if filter_job_id:
        print(f"  filter jobId: {filter_job_id}")
    if filter_model:
        print(f"  filter model: {filter_model}")
    print()

    if not rows:
        print("No matching cron run entries found.")
        return

    for job in rows:
        print(f"jobId: {job['jobId']}")
        print(f"  runs: {fmt_int(job['runs'])} (missing usage: {fmt_int(job['missingUsageRuns'])})")
        print(
            f"  tokens: total {fmt_int(job['total_tokens'])} "
            f"(in {fmt_int(job['input_tokens'])} / out {fmt_int(job['output_tokens'])})"
        )
        for m in job["models"]:
            print(
                f"    model {m['model']}: runs {fmt_int(m['runs'])} "
                f"(missing usage: {fmt_int(m['missingUsageRuns'])}), "
                f"total {fmt_int(m['total_tokens'])} "
                f"(in {fmt_int(m['input_tokens'])} / out {fmt_int(m['output_tokens'])})"
            )
        print()


if __name__ == "__main__":
    main()

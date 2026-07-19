#!/usr/bin/env python3
"""RSI P5-2 calibration-window harvest (roadmap graduation-ladder row).

The 2026-07-12 → 2026-08-23 calibration window accelerates the slow loop via a
systemd drop-in (rsi-calibration.conf). Its whole point is the DATA: per-epoch
bench samples and fitness evidence. Both the revert and the conclusion used to
be manual, so the campaign could end silently with nothing harvested — this
script closes the window deliberately:

  1. harvest  — read meta_evolution_log.jsonl, compute per-epoch benched-cycle
                counts since window open (target: >= 10 each) plus a cycle
                inventory, and write a markdown conclusion to the state dir.
  2. publish  — best-effort: create a wiki page via the gateway RPC so the
                conclusion is operator-visible and recallable (fail-open).
  3. revert   — with --revert: delete the drop-in, daemon-reload, and reload
                the gateway via SIGTERM to MainPID (the unit refuses
                `systemctl restart`; Restart=always brings it back with the
                default cadence).

Scheduled by deneb-calibration-harvest.timer (OnCalendar=2026-08-23) — see
scripts/systemd/setup-calibration-harvest.sh. Safe to run early by hand for a
mid-window readout (without --revert).
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import subprocess
import sys
import urllib.request
from pathlib import Path

WINDOW_OPEN_UTC = dt.datetime(2026, 7, 12, tzinfo=dt.timezone.utc)
WINDOW_CLOSE = "2026-08-23"
BENCH_TARGET = 10
EPOCHS = ("producer", "evaluator", "genesis")
BENCH_FIELDS = ("benchIncumbent", "benchShadow", "benchGenesis")
CALIBRATION_CONF = Path.home() / ".config/systemd/user/deneb-gateway.service.d/rsi-calibration.conf"


def load_cycles(ledger: Path) -> list[dict]:
    if not ledger.is_file():
        return []
    rows = []
    for line in ledger.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            rows.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return rows


def harvest(cycles: list[dict]) -> dict:
    """Per-epoch benched-cycle counts since window open + cycle inventory.

    Mirrors the Go ladderCalibrationRow rule: a cycle counts when it was
    created at/after the window open, names an epoch, and carries at least one
    bench payload.
    """
    open_ms = int(WINDOW_OPEN_UTC.timestamp() * 1000)
    benched = {e: 0 for e in EPOCHS}
    inventory = []
    for c in cycles:
        try:
            created = int(c.get("createdAt") or 0)
        except (TypeError, ValueError):
            continue
        if created < open_ms:
            continue
        epoch = str(c.get("epoch") or "")
        has_bench = any(c.get(f) is not None for f in BENCH_FIELDS)
        if epoch in benched and has_bench:
            benched[epoch] += 1
        inventory.append(
            {
                "createdAt": created,
                "epoch": epoch or "(none)",
                "benched": has_bench,
                "reason": str(c.get("reason") or "")[:120],
            }
        )
    return {
        "benched": benched,
        "target": BENCH_TARGET,
        "ready": all(v >= BENCH_TARGET for v in benched.values()),
        "cyclesSinceOpen": len(inventory),
        "inventory": inventory,
    }


def render_report(result: dict, now: dt.datetime, conf_present: bool, reverted: bool) -> str:
    b = result["benched"]
    lines = [
        "# RSI P5-2 캘리브레이션 윈도 하베스트",
        "",
        f"- 생성: {now.isoformat(timespec='seconds')} (자동 — calibration_harvest.py)",
        f"- 윈도: 2026-07-12 → {WINDOW_CLOSE} (roadmap P5 workstream 2)",
        f"- 드롭인: {'존재' if conf_present else '없음'}"
        + (" → 이번 실행에서 제거·게이트웨이 리로드" if reverted else ""),
        "",
        "## 에폭별 벤치 사이클 (목표 각 " + str(BENCH_TARGET) + ")",
        "",
    ]
    for e in EPOCHS:
        verdict = "달성" if b[e] >= BENCH_TARGET else f"미달 ({BENCH_TARGET - b[e]} 부족)"
        lines.append(f"- {e}: **{b[e]}** — {verdict}")
    lines += [
        "",
        f"**종합: {'목표 달성 — 사다리 행 졸업 근거 확보' if result['ready'] else '목표 미달 — 아래 사이클 인벤토리로 병목 진단'}**",
        "",
        f"## 윈도 내 사이클 인벤토리 ({result['cyclesSinceOpen']}건)",
        "",
    ]
    for c in result["inventory"]:
        when = dt.datetime.fromtimestamp(c["createdAt"] / 1000, tz=dt.timezone.utc).astimezone()
        mark = "벤치 ✓" if c["benched"] else "벤치 없음"
        lines.append(f"- {when:%m-%d %H:%M} · {c['epoch']} · {mark} · {c['reason']}")
    if not result["ready"]:
        lines += [
            "",
            "## 미달 시 참고",
            "",
            "- 벤치는 메타 사이클(Epoch 있는 개정)에만 붙는다 — 사이클 자체가 돌았는데 벤치가 비면"
            " 벤치 실행 실패/스킵을 의심하라 (meta_*_bench 경로).",
            "- 케이던스 노브 이력: 2026-07-12 2d 개시 → 2026-07-18 1d 보정.",
        ]
    lines.append("")
    return "\n".join(lines)


def publish_wiki(report: str, now: dt.datetime, gateway: str) -> str:
    """Best-effort wiki publication; returns the created path or ''."""
    try:
        token = (Path.home() / ".deneb" / "client_token").read_text(encoding="utf-8").strip()
        body = json.dumps(
            {
                "type": "req",
                "id": "calibration-harvest",
                "method": "miniapp.memory.create_page",
                "params": {
                    "title": f"RSI 캘리브레이션 하베스트 {now:%Y-%m-%d}",
                    "category": "시스템",
                    "body": report,
                },
            }
        ).encode()
        req = urllib.request.Request(
            f"{gateway.rstrip('/')}/api/v1/miniapp/rpc",
            data=body,
            headers={"Content-Type": "application/json", "X-Deneb-Client-Token": token},
        )
        with urllib.request.urlopen(req, timeout=20) as resp:
            payload = json.load(resp).get("payload") or {}
        return str(payload.get("path") or "")
    except Exception as exc:  # noqa: BLE001 — publication is best-effort
        print(f"wiki publish skipped: {exc}", file=sys.stderr)
        return ""


def revert_calibration() -> bool:
    """Remove the drop-in and reload the gateway. Returns True when reverted."""
    if not CALIBRATION_CONF.is_file():
        print("revert: drop-in already absent — nothing to do")
        return False
    CALIBRATION_CONF.unlink()
    env = dict(os.environ)
    env.setdefault("XDG_RUNTIME_DIR", f"/run/user/{os.getuid()}")
    subprocess.run(["systemctl", "--user", "daemon-reload"], check=True, env=env)
    # The gateway unit sets RefuseManualStop; SIGTERM to MainPID + Restart=always
    # is the sanctioned reload path (docs/agent-rules/release-and-deploy.md).
    show = subprocess.run(
        ["systemctl", "--user", "show", "deneb-gateway", "-p", "MainPID", "--value"],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )
    pid = show.stdout.strip()
    if pid.isdigit() and int(pid) > 0:
        subprocess.run(["kill", "-TERM", pid], check=False)
        print(f"revert: drop-in removed, gateway reload signaled (pid {pid})")
    else:
        print(
            "revert: drop-in removed; gateway MainPID unresolved — reload it manually",
            file=sys.stderr,
        )
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--state-dir", default=str(Path.home() / ".deneb"))
    parser.add_argument("--gateway", default="http://127.0.0.1:18789")
    parser.add_argument("--report-path", default="", help="override the report file location")
    parser.add_argument(
        "--revert",
        action="store_true",
        help="remove the calibration drop-in and reload the gateway",
    )
    parser.add_argument(
        "--no-wiki", action="store_true", help="skip the best-effort wiki publication"
    )
    args = parser.parse_args()

    state = Path(args.state_dir).expanduser()
    now = dt.datetime.now().astimezone()
    result = harvest(load_cycles(state / "data" / "meta_evolution_log.jsonl"))
    conf_present = CALIBRATION_CONF.is_file()

    reverted = revert_calibration() if args.revert else False
    report = render_report(result, now, conf_present, reverted)

    report_path = (
        Path(args.report_path)
        if args.report_path
        else state / "data" / f"calibration-harvest-{now:%Y%m%d}.md"
    )
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(report, encoding="utf-8")
    print(f"report: {report_path}")

    if not args.no_wiki:
        if path := publish_wiki(report, now, args.gateway):
            print(f"wiki: {path}")

    b = result["benched"]
    print(
        "benched cycles since window open — "
        + " · ".join(f"{e}: {b[e]}/{BENCH_TARGET}" for e in EPOCHS)
        + (" — READY" if result["ready"] else " — SHORTFALL")
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

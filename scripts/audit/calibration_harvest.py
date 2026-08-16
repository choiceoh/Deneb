#!/usr/bin/env python3
"""RSI P5-2 calibration-window harvest (roadmap graduation-ladder row).

The 2026-07-12 → 2026-08-23 calibration window accelerates the slow loop via a
systemd drop-in (rsi-calibration.conf). Its whole point is the DATA: per-epoch
bench samples and fitness evidence. Both the revert and the conclusion used to
be manual, so the campaign could end silently with nothing harvested — this
script closes the window deliberately:

  1. harvest  — read meta_evolution_log.jsonl: per-epoch benched-cycle counts
                (target: >= 10 each), adopted/rejected/reverted revisions,
                first-vs-last 7d feed-card snapshot, and a cycle inventory.
                Write a markdown conclusion to the state dir. The report must
                name which meta revisions landed — not just that the loop ran.
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
ADOPT_ACTIONS = ("adopted", "auto_adopted")
REJECT_ACTIONS = ("rejected",)
REVERT_ACTIONS = ("operator_reverted", "auto_reverted")
CALIBRATION_CONF = Path.home() / ".config/systemd/user/deneb-gateway.service.d/rsi-calibration.conf"


def _as_int(value: object, default: int = 0) -> int:
    try:
        return int(value or 0)
    except (TypeError, ValueError):
        return default


def _as_float(value: object, default: float = 0.0) -> float:
    try:
        return float(value or 0)
    except (TypeError, ValueError):
        return default


def _utility_snapshot(row: dict) -> dict | None:
    raw = row.get("operatorUtility")
    if not isinstance(raw, dict):
        return None
    return {
        "at": _as_int(row.get("createdAt")),
        "adopted7d": _as_int(raw.get("adopted7d")),
        "rejected7d": _as_int(raw.get("rejected7d")),
        "reverted7d": _as_int(raw.get("reverted7d")),
        "adoptionRate": _as_float(raw.get("adoptionRate")),
    }


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
    """Window-scoped harvest: benches, landed revisions, feed-card snapshots.

    Cycle rows (empty action) feed the ladderCalibrationRow bench counts.
    Action rows (adopted/rejected/reverted) are the operator-visible outcome
    of those cycles — they must not inflate the bench denominator.
    """
    open_ms = int(WINDOW_OPEN_UTC.timestamp() * 1000)
    benched = {e: 0 for e in EPOCHS}
    cycles_by_epoch = {e: 0 for e in EPOCHS}
    inventory = []
    adoptions: list[dict] = []
    verdicts = {"adopted": 0, "rejected": 0, "reverted": 0}
    utilities: list[dict] = []
    unbenched_reasons: list[str] = []
    for c in cycles:
        created = _as_int(c.get("createdAt"))
        if created < open_ms:
            continue
        action = str(c.get("action") or "")
        if action:
            item = {
                "createdAt": created,
                "action": action,
                "epoch": str(c.get("epoch") or ""),
                "artifact": str(c.get("artifact") or ""),
                "reason": str(c.get("reason") or "")[:120],
                "revisionClass": str(c.get("revisionClass") or ""),
            }
            if action in ADOPT_ACTIONS:
                verdicts["adopted"] += 1
                adoptions.append(item)
            elif action in REJECT_ACTIONS:
                verdicts["rejected"] += 1
            elif action in REVERT_ACTIONS:
                verdicts["reverted"] += 1
            continue
        epoch = str(c.get("epoch") or "")
        has_bench = any(c.get(f) is not None for f in BENCH_FIELDS)
        if epoch in cycles_by_epoch:
            cycles_by_epoch[epoch] += 1
        if epoch in benched and has_bench:
            benched[epoch] += 1
        elif epoch in benched:
            reason = str(c.get("reason") or "").strip() or "벤치 없음"
            unbenched_reasons.append(reason[:80])
        inventory.append(
            {
                "createdAt": created,
                "epoch": epoch or "(none)",
                "benched": has_bench,
                "reason": str(c.get("reason") or "")[:120],
                "artifact": str(c.get("artifact") or ""),
            }
        )
        if snap := _utility_snapshot(c):
            utilities.append(snap)
    ready = all(v >= BENCH_TARGET for v in benched.values())
    utilities.sort(key=lambda row: row["at"])
    feed = None
    if utilities:
        first, last = utilities[0], utilities[-1]
        feed = {
            "first": first,
            "last": last,
            "rateDelta": last["adoptionRate"] - first["adoptionRate"],
        }
    bottleneck = None
    if not ready:
        bottleneck = {
            "starved": [e for e in EPOCHS if benched[e] < BENCH_TARGET],
            "cyclesByEpoch": cycles_by_epoch,
            "unbenched": unbenched_reasons[:8],
        }
    return {
        "benched": benched,
        "target": BENCH_TARGET,
        "ready": ready,
        "cyclesSinceOpen": len(inventory),
        "inventory": inventory,
        "adoptions": adoptions,
        "verdicts": verdicts,
        "feed": feed,
        "bottleneck": bottleneck,
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
        f"**종합: {'목표 달성 — 사다리 행 졸업 근거 확보' if result['ready'] else '목표 미달 — 병목을 남기고 창은 예정대로 닫는다'}**",
        "",
    ]
    lines += _render_adoptions(result)
    lines += _render_feed(result)
    lines += [
        f"## 윈도 내 사이클 인벤토리 ({result['cyclesSinceOpen']}건)",
        "",
    ]
    for c in result["inventory"]:
        when = dt.datetime.fromtimestamp(c["createdAt"] / 1000, tz=dt.timezone.utc).astimezone()
        mark = "벤치 ✓" if c["benched"] else "벤치 없음"
        artifact = f" · {c['artifact']}" if c.get("artifact") else ""
        lines.append(f"- {when:%m-%d %H:%M} · {c['epoch']}{artifact} · {mark} · {c['reason']}")
    if bottleneck := result.get("bottleneck"):
        lines += _render_bottleneck(bottleneck, result["benched"])
    lines.append("")
    return "\n".join(lines)


def _when(ms: int) -> str:
    return dt.datetime.fromtimestamp(ms / 1000, tz=dt.timezone.utc).astimezone().strftime("%m-%d %H:%M")


def _render_adoptions(result: dict) -> list[str]:
    v = result.get("verdicts") or {}
    adopted = result.get("adoptions") or []
    lines = [
        "## 채택된 메타 개정",
        "",
        f"- 윈도 내 판정: 채택 {v.get('adopted', 0)} · 기각 {v.get('rejected', 0)} · 되돌림 {v.get('reverted', 0)}",
        "",
    ]
    if not adopted:
        lines.append("- 채택된 개정 없음 — 루프가 돌아도 메타 아티팩트는 그대로다.")
        lines.append("")
        return lines
    for row in adopted:
        klass = row["revisionClass"] or "미분류"
        artifact = row["artifact"] or "(artifact 없음)"
        lines.append(
            f"- {_when(row['createdAt'])} · {row['action']} · {row['epoch'] or '?'} · "
            f"{artifact} ({klass}) · {row['reason'] or '사유 없음'}"
        )
    lines.append("")
    return lines


def _render_feed(result: dict) -> list[str]:
    lines = ["## 피드 카드 판정 (윈도 초 → 말, 7일 스냅샷)", ""]
    feed = result.get("feed")
    if not feed:
        lines += [
            "- 원장에 operatorUtility 스냅샷이 없다. 피드 채택률 델타를 계산할 수 없음.",
            "",
        ]
        return lines
    first, last = feed["first"], feed["last"]
    delta = feed["rateDelta"]
    sign = "+" if delta >= 0 else ""
    lines += [
        f"- 초({_when(first['at'])}): 채택 {first['adopted7d']} · 기각 {first['rejected7d']} · "
        f"되돌림 {first['reverted7d']} (채택률 {first['adoptionRate'] * 100:.0f}%)",
        f"- 말({_when(last['at'])}): 채택 {last['adopted7d']} · 기각 {last['rejected7d']} · "
        f"되돌림 {last['reverted7d']} (채택률 {last['adoptionRate'] * 100:.0f}%)",
        f"- 채택률 델타: {sign}{delta * 100:.0f}pp (자문 — 게이트 아님)",
        "",
    ]
    return lines


def _render_bottleneck(bottleneck: dict, benched: dict) -> list[str]:
    lines = [
        "",
        "## 병목",
        "",
        "- 창은 예정대로 닫는다. 아래는 다음 창을 열지 말아야 할 이유다.",
    ]
    cycles = bottleneck.get("cyclesByEpoch") or {}
    for epoch in bottleneck.get("starved") or []:
        have, ran = benched.get(epoch, 0), cycles.get(epoch, 0)
        lines.append(
            f"- {epoch}: 벤치 {have}/{BENCH_TARGET} · 사이클 {ran}건 "
            f"(부족 {BENCH_TARGET - have})"
        )
    for reason in bottleneck.get("unbenched") or []:
        lines.append(f"- 벤치 없는 사이클: {reason}")
    lines += [
        "- 벤치는 메타 사이클(Epoch 있는 개정)에만 붙는다 — 사이클이 있는데 벤치가 비면 "
        "meta_*_bench 스킵/실패를 의심하라.",
        "- 케이던스 노브 이력: 2026-07-12 2d 개시 → 2026-07-18 1d 보정.",
    ]
    return lines


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
    v = result["verdicts"]
    print(
        "benched cycles since window open — "
        + " · ".join(f"{e}: {b[e]}/{BENCH_TARGET}" for e in EPOCHS)
        + (" — READY" if result["ready"] else " — SHORTFALL")
        + f" · adopted {v['adopted']} rejected {v['rejected']} reverted {v['reverted']}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

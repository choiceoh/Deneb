"""Render the Go-owned RSI status snapshot for operators and generated docs.

Layer classification lives exclusively in ``genesis.Tracker.RSIStatus`` and is
served by ``miniapp.rsi.status``. This module validates that read model and
formats it; it never reinterprets ledgers or mirrors policy constants.
"""

from __future__ import annotations

import argparse
import datetime
import json
import os
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, TextIO

DEFAULT_GATEWAY_URL = "http://127.0.0.1:18789"
DEFAULT_STATE_DIR = "~/.deneb"
VALID_STATES = frozenset({"LIVE", "DATA-GATED", "STARVED", "FROZEN", "IDLE"})
STATE_GLYPH = {"LIVE": "●", "DATA-GATED": "◐", "STARVED": "○", "FROZEN": "❄", "IDLE": "·"}


class StatusError(RuntimeError):
    """The canonical RSI snapshot is unavailable or malformed."""


@dataclass(frozen=True)
class LayerStatus:
    key: str
    title: str
    state: str
    diagnosis: str
    detail: str
    metrics: dict[str, str]


@dataclass(frozen=True)
class StatusSnapshot:
    layers: tuple[LayerStatus, ...]
    turning: int
    health: dict[str, Any]


def load_token(state_dir: str) -> str:
    token = os.environ.get("DENEB_CLIENT_TOKEN", "").strip()
    if token:
        return token
    try:
        return (Path(state_dir).expanduser() / "client_token").read_text(encoding="utf-8").strip()
    except OSError:
        return ""


def fetch_status(base_url: str, token: str, timeout: float = 10) -> StatusSnapshot:
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token
    request = urllib.request.Request(
        f"{base_url.rstrip('/')}/api/v1/miniapp/rpc",
        data=json.dumps({
            "type": "req",
            "id": "rsi-status",
            "method": "miniapp.rsi.status",
            "params": {},
        }).encode(),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            envelope = json.loads(response.read())
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError) as exc:
        raise StatusError(f"miniapp.rsi.status unavailable: {exc}") from exc
    if not isinstance(envelope, dict) or envelope.get("ok") is False or envelope.get("error"):
        error = envelope.get("error") if isinstance(envelope, dict) else envelope
        raise StatusError(f"miniapp.rsi.status rejected: {error}")
    return snapshot_from_payload(envelope.get("payload"))


def snapshot_from_payload(payload: Any) -> StatusSnapshot:
    if not isinstance(payload, dict) or not isinstance(payload.get("layers"), list):
        raise StatusError("miniapp.rsi.status returned no layers")
    layers = tuple(_layer_from_payload(raw) for raw in payload["layers"])
    turning = payload.get("turning")
    loop_count = sum(layer.key != "GRAD" for layer in layers)
    if isinstance(turning, bool) or not isinstance(turning, int) or not 0 <= turning <= loop_count:
        raise StatusError(f"miniapp.rsi.status returned invalid turning count: {turning!r}")
    health = payload.get("health")
    if not isinstance(health, dict):
        raise StatusError("miniapp.rsi.status returned no health snapshot")
    return StatusSnapshot(layers=layers, turning=turning, health=health)


def _layer_from_payload(raw: Any) -> LayerStatus:
    if not isinstance(raw, dict):
        raise StatusError("miniapp.rsi.status returned a malformed layer")
    key = _required_text(raw, "key")
    title = _required_text(raw, "title")
    state = _required_text(raw, "state")
    diagnosis = _required_text(raw, "diagnosis")
    if state not in VALID_STATES:
        raise StatusError(f"miniapp.rsi.status returned unknown state {state!r} for {key}")
    metrics: dict[str, str] = {}
    for metric in raw.get("metrics") or []:
        if not isinstance(metric, dict):
            raise StatusError(f"miniapp.rsi.status returned a malformed metric for {key}")
        label = _required_text(metric, "label")
        if label in metrics:
            raise StatusError(f"miniapp.rsi.status returned duplicate metric {label!r} for {key}")
        metrics[label] = str(metric.get("value") or "")
    return LayerStatus(
        key=key,
        title=title,
        state=state,
        diagnosis=diagnosis,
        detail=str(raw.get("detail") or ""),
        metrics=metrics,
    )


def _required_text(raw: dict[str, Any], field: str) -> str:
    value = raw.get(field)
    if not isinstance(value, str) or not value.strip():
        raise StatusError(f"miniapp.rsi.status returned invalid {field}")
    return value.strip()


def snapshot_json(snapshot: StatusSnapshot) -> dict[str, Any]:
    return {
        "turning": snapshot.turning,
        "health": snapshot.health,
        "layers": [
            {
                "key": layer.key,
                "title": layer.title,
                "state": layer.state,
                "diagnosis": layer.diagnosis,
                "detail": layer.detail,
                "metrics": layer.metrics,
            }
            for layer in snapshot.layers
        ],
    }


def print_summary(snapshot: StatusSnapshot, stream: TextIO) -> None:
    loop_count = sum(layer.key != "GRAD" for layer in snapshot.layers)
    print(f"\nRSI loop status  ({snapshot.turning}/{loop_count} turning)", file=stream)
    print("─" * 72, file=stream)
    for layer in snapshot.layers:
        print(f"  {STATE_GLYPH[layer.state]} {layer.key}  {layer.title:<22} {layer.state}", file=stream)
        print(f"      · {layer.diagnosis}", file=stream)
    print("─" * 72, file=stream)


def render_markdown(snapshot: StatusSnapshot, now_ms: int, gateway_url: str) -> str:
    generated = datetime.datetime.fromtimestamp(
        now_ms / 1000, tz=datetime.timezone.utc
    ).isoformat().replace("+00:00", "Z")
    loop_count = sum(layer.key != "GRAD" for layer in snapshot.layers)
    lines = [
        "# Deneb RSI live status",
        "",
        f"> Generated {generated} from `{gateway_url.rstrip('/')}` via `miniapp.rsi.status`. Do not edit by hand.",
        "",
        f"**Turning: {snapshot.turning}/{loop_count}**",
        "",
        "| Layer | State | Diagnosis |",
        "|---|---|---|",
    ]
    for layer in snapshot.layers:
        diagnosis = layer.diagnosis.replace("|", "\\|").replace("\n", " ")
        lines.append(f"| {layer.key} — {layer.title} | {layer.state} | {diagnosis} |")
    lines.extend(["", "## Metrics", ""])
    for layer in snapshot.layers:
        lines.extend([
            f"### {layer.key}",
            "",
            "```json",
            json.dumps(layer.metrics, ensure_ascii=False, indent=2, sort_keys=True),
            "```",
            "",
        ])
    lines.extend([
        "### Health",
        "",
        "```json",
        json.dumps(snapshot.health, ensure_ascii=False, indent=2, sort_keys=True),
        "```",
        "",
    ])
    return "\n".join(lines).rstrip() + "\n"


def _write_atomic(path: str, content: str) -> None:
    target = Path(path).expanduser().resolve()
    target.parent.mkdir(parents=True, exist_ok=True)
    temp = target.with_name(target.name + ".tmp")
    temp.write_text(content, encoding="utf-8")
    temp.replace(target)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Render the Go-owned Deneb RSI status snapshot.")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--json", action="store_true")
    mode.add_argument("--markdown", action="store_true")
    parser.add_argument("--write-markdown", metavar="PATH")
    parser.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", DEFAULT_GATEWAY_URL))
    parser.add_argument("--state-dir", default=os.environ.get("DENEB_STATE_DIR", DEFAULT_STATE_DIR))
    parser.add_argument("--token", default="")
    parser.add_argument("--timeout", type=float, default=10)
    parser.add_argument("--now-ms", type=int, default=None, help=argparse.SUPPRESS)
    return parser


def main(
    argv: list[str] | None = None,
    *,
    stdout: TextIO | None = None,
    stderr: TextIO | None = None,
) -> int:
    args = _parser().parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    err = stderr if stderr is not None else sys.stderr
    try:
        snapshot = fetch_status(args.url, args.token.strip() or load_token(args.state_dir), args.timeout)
    except StatusError as exc:
        print(f"RSI status unavailable: {exc}", file=err)
        return 2

    now_ms = args.now_ms if args.now_ms is not None else int(time.time() * 1000)
    markdown = render_markdown(snapshot, now_ms, args.url)
    if args.write_markdown:
        _write_atomic(args.write_markdown, markdown)
    if args.markdown:
        print(markdown, end="", file=out)
        return 0
    if args.json:
        print(json.dumps(snapshot_json(snapshot), ensure_ascii=False, indent=2), file=out)
        return 0

    print_summary(snapshot, err)
    print("DENEB_RSI_STATUS " + " ".join(
        f"{layer.key}={layer.state}" for layer in snapshot.layers
    ), file=out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

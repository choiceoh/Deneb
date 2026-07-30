"""Optional RSI Bench cache from live /health (and optional rsi.status)."""

from __future__ import annotations

import json
import os
import tempfile
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

DEFAULT_TTL_HOURS = 72


class CacheError(RuntimeError):
    """Cache missing, stale, or unreadable when required."""


def default_cache_path(root: Path) -> Path:
    """Runtime cache lives in the state dir, NOT the repo tree.

    It used to sit at scripts/audit/rsi-bench-cache.json — a tracked file the
    bench rewrote in place, which kept the production checkout permanently
    dirty and forced the auto-deploy timer through a git stash/pop on every
    tick (2,671 of 2,683 auto-stashes over three days were this one file).
    Runtime state must never dirty the deployable tree. The `root` parameter
    is kept for signature stability (and future per-tree isolation if a dev
    instance ever needs it).
    """
    del root
    state = Path(os.environ.get("DENEB_STATE_DIR", "") or (Path.home() / ".deneb"))
    return state / "data" / "rsi-bench-cache.json"


def _parse_generated_at(raw: object) -> datetime | None:
    if not isinstance(raw, str) or not raw.strip():
        return None
    try:
        return datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        return None


def load_cache(path: Path, *, ttl_hours: float = DEFAULT_TTL_HOURS, required: bool = False) -> dict[str, Any] | None:
    if not path.is_file():
        if required:
            raise CacheError(f"missing required cache {path}")
        return None
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        if required:
            raise CacheError(f"unreadable cache {path}: {exc}") from exc
        return None
    if not isinstance(payload, dict):
        if required:
            raise CacheError("cache root must be an object")
        return None
    generated = _parse_generated_at(payload.get("generated_at"))
    if generated is None:
        if required:
            raise CacheError("cache missing generated_at")
        return None
    age_h = (datetime.now(timezone.utc) - generated.astimezone(timezone.utc)).total_seconds() / 3600.0
    if age_h > ttl_hours:
        if required:
            raise CacheError(f"cache stale: age={age_h:.1f}h ttl={ttl_hours}h")
        return None
    return payload


def _fetch_json(url: str, timeout: float = 5.0) -> dict[str, Any] | None:
    try:
        req = urllib.request.Request(url, headers={"Accept": "application/json"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read())
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError, json.JSONDecodeError):
        return None
    return data if isinstance(data, dict) else None


def _rpc_rsi_status(base: str, token: str, timeout: float = 5.0) -> dict[str, Any] | None:
    headers = {"Content-Type": "application/json", "Accept": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token
    body = json.dumps(
        {"type": "req", "id": "rsi-bench-cache", "method": "miniapp.rsi.status", "params": {}}
    ).encode()
    try:
        req = urllib.request.Request(f"{base}/api/v1/miniapp/rpc", data=body, headers=headers, method="POST")
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            envelope = json.loads(resp.read())
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError, json.JSONDecodeError):
        return None
    if not isinstance(envelope, dict) or envelope.get("ok") is False:
        return None
    payload = envelope.get("payload") or envelope.get("result")
    return payload if isinstance(payload, dict) else None


def _snapshot_scores(snap: dict[str, Any]) -> tuple[float, dict[str, Any]]:
    """(overall, per-domain scores) out of a health-v3 snapshot payload."""
    score = snap.get("score") if isinstance(snap.get("score"), dict) else {}
    overall = float(score.get("overall", snap.get("overall") or 0))
    domains = score.get("domains") if isinstance(score.get("domains"), dict) else {}
    return overall, dict(domains)


def _embed_health_v3(root: Path, *, force: bool = True) -> dict[str, Any] | None:
    """Embed Health Bench 3 scores for codebase-delta (leaf: run Python bench externally).

    Carries per-domain scores, not just overall: codebase-delta reads the
    **structure** domain (see ``utility.score_codebase_delta`` — overall is the
    self-referential signal it must not touch), so a cache without ``domains``
    makes the pillar read bootstrap on snapshot-less hosts.

    When ``force`` (default on refresh_cache), always recompute and overwrite the
    gitignored snapshot so cadence timers cannot stick on a stale overall.
    """
    snap_path = root / "scripts" / "audit" / "health-v3-snapshot.json"
    if not force and snap_path.is_file():
        try:
            overall, domains = _snapshot_scores(json.loads(snap_path.read_text(encoding="utf-8")))
            if overall > 0:
                return {"overall": overall, "domains": domains, "source": "snapshot"}
        except (OSError, json.JSONDecodeError, TypeError, ValueError):
            pass
    try:
        import codebase_health_v3 as hv3

        report = hv3.collect_report(root, profile="fast")
        payload = report.to_dict()
        snap_path.parent.mkdir(parents=True, exist_ok=True)
        snap_path.write_text(
            json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )
        _, domains = _snapshot_scores(payload)
        return {"overall": report.overall, "domains": domains, "source": "computed"}
    except Exception as exc:  # noqa: BLE001 — advisory embed; never fail cache refresh
        if snap_path.is_file():
            try:
                overall, domains = _snapshot_scores(json.loads(snap_path.read_text(encoding="utf-8")))
                if overall > 0:
                    return {
                        "overall": overall,
                        "domains": domains,
                        "source": "snapshot-fallback",
                        "error": str(exc)[:200],
                    }
            except (OSError, json.JSONDecodeError, TypeError, ValueError):
                pass
        return {"overall": None, "source": "unavailable", "error": str(exc)[:200]}


def refresh_cache(
    path: Path, *, gateway_url: str | None = None, root: Path | None = None
) -> dict[str, Any]:
    base = (gateway_url or os.environ.get("DENEB_GATEWAY_URL") or "http://127.0.0.1:18789").rstrip("/")
    health = _fetch_json(f"{base}/health")
    if not health:
        raise CacheError(f"could not fetch {base}/health")
    section = health.get("self_evolution") or health.get("propus") or {}
    if not isinstance(section, dict) or not section:
        raise CacheError("health payload missing self_evolution/propus section")

    token = os.environ.get("DENEB_CLIENT_TOKEN", "").strip()
    if not token:
        try:
            token = (Path.home() / ".deneb" / "client_token").read_text(encoding="utf-8").strip()
        except OSError:
            token = ""
    rsi_status = _rpc_rsi_status(base, token)

    repo_root = root or path.resolve().parents[2]
    health_v3 = _embed_health_v3(repo_root)

    payload = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "gateway_url": base,
        "self_evolution": section,
        "rsi_status": rsi_status,
        "health_v3": health_v3,
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    text = json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n"
    fd, tmp_name = tempfile.mkstemp(prefix="rsi-bench-cache-", dir=str(path.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(text)
        os.replace(tmp_name, path)
    finally:
        if os.path.exists(tmp_name):
            try:
                os.unlink(tmp_name)
            except OSError:
                pass
    return payload


def self_evolution_from_cache(cache: dict[str, Any] | None) -> dict[str, Any]:
    if not cache:
        return {}
    section = cache.get("self_evolution")
    return section if isinstance(section, dict) else {}

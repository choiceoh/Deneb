"""Native-client test transport for live chat/quality tests.

Drop-in replacement for `scripts/mock_telegram_client.py`'s `TelegramTestClient`.
Instead of injecting fake updates into a mock Telegram Bot API server (the path
that PR #1922 broke when the Telegram plugin was removed), this client drives the
gateway through the *real* native-client surface:

    POST /api/v1/miniapp/rpc
      X-Deneb-Client-Token: <secret>
      { "type":"req", "id":..., "method":"miniapp.chat.send",
        "params": { "message":..., "sessionKey":... } }

`miniapp.chat.send` runs one synchronous agent turn (SendSync) and returns the
reply text in the RPC response — exactly what the standalone native client does.
This is the supported, production injection path, so live tests now exercise the
same code the daily-driver app uses.

Synchronous vs streaming: the native surface returns a final reply, not a token
stream, so there are no per-token deltas or per-tool events to capture. We
synthesize a single delta/event from the final reply so the existing check
evaluators (korean, substance, latency, streaming-flow) keep working unchanged;
first-token latency therefore equals full turn latency. Per-tool introspection
(`--expect-tool`) is not observable on this surface and is reported as
unsupported rather than silently passing.

Auth: the client token is read from (in order) the DENEB_LIVETEST_CLIENT_TOKEN
env var, then `<DENEB_LIVETEST_STATE_DIR>/client_token`, then
`~/.deneb/client_token`. The dev harness (scripts/dev/live-test.sh via
lib-server.sh) generates a token in the dev state dir and exports both env vars,
so tests authenticate against the same secret the dev gateway verifies.
"""
from __future__ import annotations

import json
import os
import time
import urllib.request
import urllib.error
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Optional

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 18790
CLIENT_TOKEN_HEADER = "X-Deneb-Client-Token"

# Shared per-process counter so successive create_session() calls without an
# explicit key get distinct, non-colliding session keys.
_session_counter = 0


@dataclass
class ChatCapture:
    """Capture from one native-client chat turn.

    Field layout mirrors the mock-Telegram ChatCapture so existing check
    evaluators (korean, substance, latency, tools, etc.) work unchanged.
    """
    events: list = field(default_factory=list)
    deltas: list = field(default_factory=list)
    tool_starts: list = field(default_factory=list)
    tool_results: list = field(default_factory=list)
    heartbeats: list = field(default_factory=list)
    status_changes: list = field(default_factory=list)
    final_response: dict = field(default_factory=dict)
    errors: list = field(default_factory=list)
    reply_text: str = ""
    start_time: float = 0
    end_time: float = 0
    all_text: str = ""
    token_usage_data: dict = field(default_factory=dict)

    # Telegram-specific fields kept so reproduce.py stays compatible.
    final_messages: list = field(default_factory=list)
    draft_edits: list = field(default_factory=list)
    reactions: list = field(default_factory=list)
    raw_events: list = field(default_factory=list)

    @property
    def latency_ms(self) -> float:
        return (self.end_time - self.start_time) * 1000

    @property
    def first_token_ms(self) -> float:
        if self.deltas:
            return (self.deltas[0]["ts"] - self.start_time) * 1000
        return 0

    @property
    def token_usage(self) -> dict:
        return self.token_usage_data


# --- Auth / endpoint resolution ---

def resolve_client_token() -> str:
    """Return the native-client bearer secret, or "" if none is available."""
    tok = os.environ.get("DENEB_LIVETEST_CLIENT_TOKEN", "").strip()
    if tok:
        return tok
    candidates = []
    state_dir = os.environ.get("DENEB_LIVETEST_STATE_DIR", "").strip()
    if state_dir:
        candidates.append(Path(state_dir) / "client_token")
    candidates.append(Path.home() / ".deneb" / "client_token")
    for path in candidates:
        try:
            return path.read_text(encoding="utf-8").strip()
        except OSError:
            continue
    return ""


def _gateway_base(host: str, port: int) -> str:
    url = os.environ.get("DENEB_LIVETEST_GW_URL", "").strip()
    if url:
        return url.rstrip("/")
    return f"http://{host}:{port}"


# --- HTTP helpers ---

def _http_get_json(url: str, timeout: float = 10.0) -> dict[str, Any]:
    with urllib.request.urlopen(url, timeout=timeout) as resp:
        return json.loads(resp.read())


def _miniapp_rpc(base: str, token: str, method: str, params: dict[str, Any],
                 timeout: float) -> dict[str, Any]:
    """POST one frame to /api/v1/miniapp/rpc and return the decoded response.

    Returns a dict with keys: ok (bool), payload (dict), error (dict|None).
    Transport/auth failures are surfaced as ok=False with an error dict rather
    than raising, so callers can record them on the capture.
    """
    frame = {"type": "req", "id": f"lt-{int(time.time()*1000)}", "method": method}
    if params:
        frame["params"] = params
    data = json.dumps(frame).encode("utf-8")
    req = urllib.request.Request(
        f"{base}/api/v1/miniapp/rpc",
        data=data,
        method="POST",
        headers={"Content-Type": "application/json", CLIENT_TOKEN_HEADER: token},
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = json.loads(resp.read())
    except urllib.error.HTTPError as exc:
        detail = ""
        try:
            detail = exc.read().decode("utf-8", "replace")
        except Exception:  # noqa: BLE001 - best-effort error body
            pass
        return {"ok": False, "payload": {}, "error": {"message": f"HTTP {exc.code}: {detail or exc.reason}"}}
    except Exception as exc:  # noqa: BLE001 - connection refused, timeout, etc.
        return {"ok": False, "payload": {}, "error": {"message": str(exc)}}

    # A miniapp.* method returns a protocol.ResponseFrame {ok, payload, error}.
    if body.get("ok"):
        payload = body.get("payload")
        return {"ok": True, "payload": payload if isinstance(payload, dict) else {}, "error": None}
    err = body.get("error")
    if isinstance(err, dict):
        return {"ok": False, "payload": {}, "error": err}
    # Non-frame error shape (auth/validation handlers write {"error": "..."}).
    return {"ok": False, "payload": {}, "error": {"message": str(err or body.get("error") or "rpc failed")}}


# --- Prerequisites ---

def check_prerequisites(host: str = DEFAULT_HOST, port: int = DEFAULT_PORT) -> tuple[bool, str]:
    """Return (ok, detail) — whether the native injection path is usable.

    Verifies the gateway /health endpoint is reachable and a client token is
    available. Kept module-level so existing `from ... import check_prerequisites`
    call sites keep working.
    """
    base = _gateway_base(host, port)
    try:
        _http_get_json(f"{base}/health", timeout=3.0)
    except Exception as exc:  # noqa: BLE001
        return False, (
            f"gateway not reachable at {base}/health: {exc}. "
            f"Start the dev gateway via scripts/dev/live-test.sh start."
        )
    if not resolve_client_token():
        return False, (
            "no native client token found (set DENEB_LIVETEST_CLIENT_TOKEN, or "
            "ensure the dev state dir has a client_token). The dev harness "
            "generates one automatically — start via scripts/dev/live-test.sh start."
        )
    return True, "ok"


# --- Native test client ---

class NativeTestClient:
    """Drives gateway chat turns through the native miniapp.chat.send RPC.

    Interface-compatible with the legacy TelegramTestClient: connect(),
    disconnect(), reset_session(), set_chat_id(), create_session(), chat(),
    rpc(), close(). `bot_username` is accepted and ignored for call-site
    compatibility.
    """

    def __init__(self, host: str = DEFAULT_HOST, port: int = DEFAULT_PORT,
                 bot_username: str = "", session_path: Optional[str] = None):
        self.host = host
        self.port = port
        self.base = _gateway_base(host, port)
        self.token = resolve_client_token()
        self.session_key = f"client:lt-{os.getpid()}"
        self._connected = False

    async def connect(self) -> str:
        # Validate the gateway is up and report the model for run metadata.
        model = "unknown"
        try:
            health = _http_get_json(f"{self.base}/health", timeout=5.0)
            model = str(health.get("model") or health.get("modelName") or "unknown")
        except Exception:  # noqa: BLE001 - health is best-effort metadata here
            pass
        if not self.token:
            self.token = resolve_client_token()
        self._connected = True
        return f"native:{model}"

    async def disconnect(self) -> None:
        self._connected = False

    async def close(self) -> None:
        await self.disconnect()

    async def reset_session(self) -> None:
        await self.chat("/reset", timeout=30.0)

    def set_chat_id(self, chat_id: int) -> None:
        """Rotate the session key for per-test isolation (chat_id analog)."""
        self.session_key = f"client:lt-{chat_id}"

    async def create_session(self, key: str = "") -> str:
        global _session_counter
        key = (key or "").strip()
        if key:
            self.session_key = key if ":" in key else f"client:{key}"
        else:
            _session_counter += 1
            self.session_key = f"client:lt-{os.getpid()}-{_session_counter}"
        return self.session_key

    async def chat(self, message: str, timeout: float = 120.0,
                   session_key: str = "") -> ChatCapture:
        cap = ChatCapture()
        cap.start_time = time.time()
        sess = (session_key or "").strip() or self.session_key
        if not self.token:
            self.token = resolve_client_token()
        if not self.token:
            cap.end_time = time.time()
            cap.errors.append("no client token available")
            cap.final_response = {"ok": False}
            return cap

        res = _miniapp_rpc(
            self.base, self.token, "miniapp.chat.send",
            {"message": message, "sessionKey": sess}, timeout,
        )
        cap.end_time = time.time()

        if not res["ok"]:
            msg = (res.get("error") or {}).get("message", "rpc failed")
            cap.errors.append(msg)
            cap.final_response = {"ok": False, "error": res.get("error")}
            return cap

        payload = res["payload"]
        reply = str(payload.get("text") or "")
        cap.reply_text = reply
        cap.all_text = reply
        cap.final_response = {"ok": True}
        cap.final_messages = [{"text": reply}] if reply else []

        usage = payload.get("usage") or {}
        if isinstance(usage, dict):
            # Key names match the consumers in quality-test.py / reproduce.py
            # (token_usage.get("inputTokens"/"outputTokens")).
            cap.token_usage_data = {
                "inputTokens": int(usage.get("inputTokens") or 0),
                "outputTokens": int(usage.get("outputTokens") or 0),
            }

        # Synthesize one delta + one event from the synchronous reply so the
        # streaming-flow / first-token checks have something to measure. For a
        # synchronous turn, "first token" is the full turn latency.
        if reply:
            cap.deltas.append({"text": reply, "ts": cap.end_time})
            cap.events.append({"event": "chat.reply", "ts": cap.end_time})
        cap.raw_events.append({
            "time": cap.end_time, "type": "miniapp.chat.send",
            "data": {"model": payload.get("model"), "fellBack": payload.get("fellBack")},
        })
        return cap

    async def rpc(self, method: str, params: dict = None, timeout: float = 30.0) -> dict:
        """Minimal RPC bridge.

        - "health" → GET /health (matches the legacy contract).
        - "miniapp.*" → forwarded to the miniapp RPC endpoint.
        - anything else → unsupported (the native surface only admits miniapp.*).
        """
        if method == "health":
            try:
                data = _http_get_json(f"{self.base}/health", timeout=5.0)
                return {"ok": True, "payload": data}
            except Exception as e:  # noqa: BLE001
                return {"ok": False, "error": {"message": str(e)}}
        if method.startswith("miniapp."):
            res = _miniapp_rpc(self.base, self.token, method, params or {}, timeout)
            if res["ok"]:
                return {"ok": True, "payload": res["payload"]}
            return {"ok": False, "error": res.get("error")}
        return {"ok": False, "error": {"message": f"RPC not supported on native surface: {method}"}}


# Backwards-compatible alias so existing imports that expect the old class name
# keep working when this module is swapped in for mock_telegram_client.
TelegramTestClient = NativeTestClient

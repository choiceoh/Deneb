#!/usr/bin/env python3
"""
Mock Telegram client for Deneb live-testing.

Drop-in replacement for the real-Telegram Telethon client that ran the quality
and reproduction suites through api.telegram.org. Instead of holding a user
session and round-tripping through Telegram's servers, this module talks to
scripts/mock_telegram_server.py, which the dev gateway is also pointed at via
TELEGRAM_API_BASE.

Flow per chat turn:
    1. Inject a synthetic user update into the mock server.
    2. The gateway's long-polling loop pulls the update and runs the chat
       pipeline exactly like production.
    3. Every sendMessage/editMessageText/deleteMessage/setMessageReaction the
       gateway emits is captured by the mock.
    4. This client polls /_test/wait (server-side settle detection) and turns
       the captured events into a ChatCapture compatible with the existing
       check evaluators.

Prerequisites:
    - The mock server must be running. live-test.sh manages this alongside
      the dev gateway.
    - The dev gateway must be started with
          TELEGRAM_API_BASE=http://127.0.0.1:18792/bot
      so its Telegram plugin talks to the mock.

Environment:
    DENEB_MOCK_TELEGRAM_URL   — mock server base URL (default: http://127.0.0.1:18792)
    DENEB_MOCK_CHAT_ID        — chat_id used for injected updates (default: 10000001)
    DENEB_MOCK_USER_ID        — user_id used for injected updates (default: 10000001)
"""

from __future__ import annotations

import json
import os
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Optional


# --- Dotenv (same convention as the old client so ~/.deneb/.env picks up overrides) ---

def _load_dotenv(path: str) -> None:
    if not os.path.isfile(path):
        return
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, _, v = line.partition("=")
            k, v = k.strip(), v.strip().strip("'\"")
            if k not in os.environ:
                os.environ[k] = v


_load_dotenv(os.path.expanduser("~/.deneb/.env"))


# --- Configuration ---

MOCK_URL = os.environ.get("DENEB_MOCK_TELEGRAM_URL", "http://127.0.0.1:18792").rstrip("/")
MOCK_CHAT_ID = int(os.environ.get("DENEB_MOCK_CHAT_ID", "10000001"))
MOCK_USER_ID = int(os.environ.get("DENEB_MOCK_USER_ID", "10000001"))
MOCK_USER_NAME = os.environ.get("DENEB_MOCK_USER_NAME", "Mock User")

SETTLE_SECS = 3.0
TIMEOUT_SECS = 120


# --- ChatCapture (field-compatible with the old WebSocket/Telethon capture) ---

@dataclass
class ChatCapture:
    """Capture from a mock Telegram chat interaction.

    Field layout mirrors the previous Telethon-based ChatCapture so existing
    check evaluators (korean, substance, latency, tools, etc.) work unchanged.
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

    # Telegram-specific fields (kept for reproduce.py compatibility).
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


# --- HTML Utilities (identical to the legacy client) ---

def _strip_html(html: str) -> str:
    text = re.sub(r"<[^>]+>", "", html)
    text = text.replace("&lt;", "<").replace("&gt;", ">")
    text = text.replace("&amp;", "&").replace("&quot;", '"')
    return text


# --- Mock HTTP helpers ---

def _http_post_json(url: str, payload: dict[str, Any], timeout: float = 10.0) -> dict[str, Any]:
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read())


def _http_get_json(url: str, timeout: float = 10.0) -> dict[str, Any]:
    with urllib.request.urlopen(url, timeout=timeout) as resp:
        return json.loads(resp.read())


# --- Mock Telegram Client ---

class TelegramTestClient:
    """Mock-backed test client. Same public surface as the legacy Telethon client."""

    def __init__(self, bot_username: str = "", session_path: str = "") -> None:
        # bot_username/session_path are kept for call-site compatibility but
        # have no meaning in mock mode. The mock server identifies itself
        # regardless of the token the gateway uses.
        self.bot_username = bot_username or "denebmockbot"
        self.session_path = session_path  # unused
        self._last_seq = 0
        self._chat_id = MOCK_CHAT_ID
        self._connected = False

    async def connect(self) -> str:
        """Verify the mock server is reachable and zero the capture cursor."""
        try:
            stats = _http_get_json(f"{MOCK_URL}/_test/stats", timeout=3.0)
        except (urllib.error.URLError, OSError) as exc:
            raise RuntimeError(
                f"mock Telegram server not reachable at {MOCK_URL}: {exc}. "
                f"Start it via scripts/dev/live-test.sh start."
            ) from exc
        # Fresh connect: start ignoring any traffic that happened before us.
        self._last_seq = int(stats.get("next_seq", 1)) - 1
        self._connected = True
        return f"@{self.bot_username}"

    async def disconnect(self) -> None:
        self._connected = False

    async def close(self) -> None:
        await self.disconnect()

    async def reset_session(self) -> None:
        """Send the /reset slash command so the gateway clears transcripts."""
        if not self._connected:
            return
        await self.chat("/reset", timeout=30.0)

    def set_chat_id(self, chat_id: int) -> None:
        """Rotate the active chat_id so subsequent chat()/inject calls use it.

        Lets the test runner isolate each test into its own gateway session —
        the gateway derives session key from chat_id, so a unique chat_id per
        test means a fresh transcript without relying on /reset having
        fully drained state between tests.

        Also rebases ``_last_seq`` to the current mock server sequence so the
        next /_test/wait call only observes events from THIS test, not any
        residual outbound that landed while the previous chat_id was active.
        """
        self._chat_id = int(chat_id)
        try:
            stats = _http_get_json(f"{MOCK_URL}/_test/stats", timeout=2.0)
            self._last_seq = int(stats.get("next_seq", 1)) - 1
        except Exception:
            # Best-effort: if stats probe fails we fall back to the old
            # counter; subsequent /_test/wait will still time out rather
            # than silently match wrong events.
            pass

    async def create_session(self, key: str = "") -> str:
        """Reset the session. Returns a pseudo session key for compatibility."""
        await self.reset_session()
        return key or f"mock-{int(time.time() * 1000)}"

    async def chat(
        self,
        message: str,
        timeout: float = TIMEOUT_SECS,
        reset: bool = False,
    ) -> ChatCapture:
        if not self._connected:
            raise RuntimeError("Not connected. Call connect() first.")
        if reset:
            await self.reset_session()

        capture = ChatCapture(start_time=time.time())

        # 1. Inject the user message into the mock update queue. The gateway's
        # long poll pulls it within ~300ms.
        try:
            inject_resp = _http_post_json(
                f"{MOCK_URL}/_test/inject",
                {
                    "chat_id": self._chat_id,
                    "from_id": MOCK_USER_ID,
                    "first_name": MOCK_USER_NAME,
                    "text": message,
                },
                timeout=5.0,
            )
        except Exception as exc:
            capture.errors.append(f"inject failed: {exc}")
            capture.end_time = time.time()
            capture.final_response = {
                "ok": False,
                "payload": {"state": "error"},
                "error": {"message": str(exc)},
            }
            return capture

        if not inject_resp.get("ok"):
            capture.errors.append(f"inject rejected: {inject_resp}")
            capture.end_time = time.time()
            capture.final_response = {
                "ok": False,
                "payload": {"state": "error"},
                "error": {"message": str(inject_resp)},
            }
            return capture

        # 2. Wait for outbound activity to settle. The server blocks on our
        # behalf so we don't busy-poll.
        since_seq = self._last_seq
        wait_url = (
            f"{MOCK_URL}/_test/wait?"
            f"since_seq={since_seq}&chat_id={self._chat_id}"
            f"&settle={SETTLE_SECS:.2f}&timeout={timeout:.0f}"
        )
        try:
            with urllib.request.urlopen(wait_url, timeout=timeout + 10) as resp:
                wait_resp = json.loads(resp.read())
        except Exception as exc:
            capture.errors.append(f"wait failed: {exc}")
            capture.end_time = time.time()
            capture.final_response = {
                "ok": False,
                "payload": {"state": "error"},
                "error": {"message": str(exc)},
            }
            return capture

        capture.end_time = time.time()
        events = wait_resp.get("events") or []
        if events:
            self._last_seq = max(self._last_seq, int(wait_resp.get("seq") or self._last_seq))

        _assemble_capture(capture, events)

        if not capture.reply_text and not capture.errors:
            capture.errors.append(f"No response after {timeout}s")
            capture.final_response = {
                "ok": False,
                "payload": {"state": "error"},
                "error": {"message": "timeout"},
            }
        elif capture.reply_text or capture.final_messages:
            capture.final_response = {
                "ok": True,
                "payload": {
                    "state": "done",
                    "text": capture.reply_text,
                },
            }
        return capture

    async def rpc(self, method: str, params: dict | None = None, timeout: float = 10.0) -> dict:
        """Compatibility shim for the small number of call sites that expect an RPC hook."""
        if method == "health":
            # Caller performs its own HTTP health check; return synthetic OK.
            return {"ok": True, "payload": {"status": "ok"}}
        return {
            "ok": False,
            "error": {"message": f"RPC not supported in mock mode: {method}"},
        }


# --- Event assembly: mock server events → ChatCapture ---

def _assemble_capture(capture: ChatCapture, events: list[dict[str, Any]]) -> None:
    """Convert mock server events into ChatCapture fields.

    The mock records every Telegram Bot API call the gateway makes. We want
    capture fields to match the legacy Telethon capture shape:

    - final_messages: list of finalized bot messages (last text per message_id).
    - draft_edits: list of intermediate edits (used for tool inference and
      streaming detection).
    - deltas: timestamps of edit events (for first-token latency).
    - raw_events: chronological record for debugging.
    """
    # message_id → latest dict representation (text + buttons)
    messages_by_id: dict[int, dict[str, Any]] = {}
    # Track the original insertion order so we can render final_messages stably.
    ordered_ids: list[int] = []

    for event in events:
        kind = event.get("kind") or ""
        payload = event.get("payload") or {}
        ts = event.get("ts") or time.time()
        capture.events.append({
            "event": f"telegram.{kind}",
            "payload": payload,
            "_recv_ts": ts,
        })
        capture.raw_events.append({"time": ts, "type": kind, "data": payload})

        if kind == "sendMessage":
            mid = int(event.get("message_id") or 0)
            text = str(payload.get("text") or "")
            rec = _message_record(mid, text, payload)
            if mid and mid not in messages_by_id:
                ordered_ids.append(mid)
            messages_by_id[mid] = rec
            capture.final_messages.append(rec)

        elif kind in ("sendPhoto", "sendDocument", "sendVideo", "sendAudio", "sendVoice"):
            mid = int(event.get("message_id") or 0)
            caption = str(payload.get("caption") or "")
            rec = _message_record(mid, caption, payload, media_kind=kind)
            if mid and mid not in messages_by_id:
                ordered_ids.append(mid)
            messages_by_id[mid] = rec
            capture.final_messages.append(rec)

        elif kind == "editMessageText":
            mid = int(payload.get("message_id") or 0)
            text = str(payload.get("text") or "")
            rec = _message_record(mid, text, payload, edited=True)
            if mid and mid not in messages_by_id:
                ordered_ids.append(mid)
            # Replace the message in-place so final_messages reflects the
            # latest version the user would see.
            messages_by_id[mid] = rec
            capture.draft_edits.append(rec)
            capture.deltas.append({"text": rec.get("text", ""), "ts": ts})

        elif kind == "deleteMessage":
            mid = int(payload.get("message_id") or 0)
            if mid in messages_by_id:
                del messages_by_id[mid]
                ordered_ids = [m for m in ordered_ids if m != mid]

        elif kind == "setMessageReaction":
            capture.reactions.append(payload)

        # Other kinds (sendChatAction, answerCallbackQuery, setMyCommands, ...)
        # don't contribute user-visible text; we still record them in raw_events.

    # Rebuild final_messages from the ordered map so it matches what the user
    # would actually see at the end of the turn.
    final: list[dict[str, Any]] = []
    for mid in ordered_ids:
        if mid in messages_by_id:
            final.append(messages_by_id[mid])
    # Also keep any records without a message_id for completeness.
    for rec in capture.final_messages:
        if not rec.get("message_id") and rec not in final:
            final.append(rec)
    capture.final_messages = final

    all_html = "\n".join(m.get("text", "") for m in final)
    capture.all_text = all_html
    capture.reply_text = _strip_html(all_html)

    _infer_tools(capture)


def _message_record(
    message_id: int,
    text: str,
    payload: dict[str, Any],
    edited: bool = False,
    media_kind: str | None = None,
) -> dict[str, Any]:
    buttons: list[str] = []
    markup = payload.get("reply_markup")
    if isinstance(markup, dict):
        for row in markup.get("inline_keyboard") or []:
            if isinstance(row, list):
                for btn in row:
                    if isinstance(btn, dict) and btn.get("text"):
                        buttons.append(str(btn["text"]))
    rec = {
        "text": text,
        "has_entities": bool(payload.get("entities")),
        "message_id": message_id,
        "buttons": buttons,
    }
    if edited:
        rec["edited"] = True
    if media_kind:
        rec["media"] = media_kind
    return rec


def _infer_tools(capture: ChatCapture) -> None:
    """Infer tool_starts/tool_results from draft edits.

    Same heuristic as the legacy Telethon client — the Deneb bot shows tool
    progress via message edits, so keywords in the edit stream imply tool
    usage. This keeps the existing check evaluators happy without introducing
    a new event format.
    """
    tool_keywords = {
        "exec": "exec", "명령어": "exec",
        "read": "read", "파일 읽": "read",
        "write": "write", "파일 작성": "write",
        "edit": "edit", "파일 수정": "edit",
        "grep": "grep", "코드 검색": "grep",
        "health": "health", "상태": "health",
        "vega": "vega", "검색": "vega",
        "memory": "memory", "메모리": "memory",
        "kv": "kv",
        "gmail": "gmail",
        "message": "message",
    }

    seen_tools: set[str] = set()
    for edit in capture.draft_edits:
        text = _strip_html(str(edit.get("text", ""))).lower()
        for keyword, tool_name in tool_keywords.items():
            if keyword in text and tool_name not in seen_tools:
                seen_tools.add(tool_name)
                capture.tool_starts.append({"name": tool_name, "ts": time.time()})
                capture.tool_results.append({
                    "name": tool_name,
                    "isError": False,
                    "ts": time.time(),
                })


def check_prerequisites() -> tuple[bool, str]:
    """Return (ok, detail) — whether the mock Telegram server is running.

    Kept as a module-level function so existing imports
    (from scripts.dev.quality_test import check_prerequisites) continue to work.
    """
    try:
        _http_get_json(f"{MOCK_URL}/_test/health", timeout=2.0)
    except Exception as exc:
        return False, (
            f"mock Telegram server not reachable at {MOCK_URL}: {exc}. "
            f"Start the dev gateway + mock via scripts/dev/live-test.sh start."
        )
    return True, "ok"


def reset_mock() -> None:
    """Clear mock server state (utility for test harnesses)."""
    try:
        _http_post_json(f"{MOCK_URL}/_test/reset", {}, timeout=3.0)
    except Exception:
        pass

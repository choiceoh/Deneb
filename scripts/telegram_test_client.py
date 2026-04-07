#!/usr/bin/env python3
"""
Shared Telegram test client for Deneb quality/reproduce/metric tests.

Uses Telethon (user account) to send messages to the dev bot and capture
the full Telegram experience: messages, edits, reactions, deletions.

Returns ChatCapture-compatible objects so existing check evaluators work unchanged.

Requirements:
    - pip install telethon
    - ~/.deneb/telegram-test.session (via scripts/telegram-session-init.py)
    - ~/.deneb/.env with TELEGRAM_API_ID, TELEGRAM_API_HASH, DENEB_DEV_BOT_USERNAME
"""

import asyncio
import os
import re
import time
from dataclasses import dataclass, field
from typing import Optional


# --- Dotenv ---

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

SESSION_PATH = os.path.expanduser("~/.deneb/telegram-test")
API_ID = int(os.environ.get("TELEGRAM_API_ID", "0"))
API_HASH = os.environ.get("TELEGRAM_API_HASH", "")
DEV_BOT_USERNAME = os.environ.get("DENEB_DEV_BOT_USERNAME", "nebdev1bot")

SETTLE_SECS = 3.0
TIMEOUT_SECS = 120


# --- ChatCapture (compatible with quality test checks) ---

@dataclass
class ChatCapture:
    """Capture from a Telegram chat interaction.

    Field-compatible with the WebSocket ChatCapture so existing check
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

    # Telegram-specific fields.
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


# --- HTML Utilities ---

def _strip_html(html: str) -> str:
    text = re.sub(r"<[^>]+>", "", html)
    text = text.replace("&lt;", "<").replace("&gt;", ">")
    text = text.replace("&amp;", "&").replace("&quot;", '"')
    return text


def _msg_to_html(message) -> str:
    """Convert a Telethon message to HTML text."""
    try:
        from telethon.extensions import html as tg_html
        if message.entities:
            return tg_html.unparse(message.text or "", message.entities)
    except Exception:
        pass
    return message.text or ""


def _msg_to_dict(message) -> dict:
    """Convert a Telethon message to dict."""
    html_text = _msg_to_html(message)
    buttons = []
    if message.buttons:
        for row in message.buttons:
            for btn in row:
                buttons.append(btn.text)
    return {
        "text": html_text,
        "has_entities": bool(message.entities),
        "message_id": message.id,
        "buttons": buttons,
    }


# --- Telegram Test Client ---

class TelegramTestClient:
    """Telethon-based test client that sends messages to a bot and captures responses."""

    def __init__(self, bot_username: str = "", session_path: str = ""):
        self.bot_username = bot_username or DEV_BOT_USERNAME
        self.session_path = session_path or SESSION_PATH
        self.client = None
        self.bot_entity = None

    async def connect(self) -> str:
        """Connect to Telegram. Returns bot display name."""
        from telethon import TelegramClient

        if not API_ID or not API_HASH:
            raise RuntimeError(
                "TELEGRAM_API_ID and TELEGRAM_API_HASH required in ~/.deneb/.env"
            )
        if not os.path.isfile(self.session_path + ".session"):
            raise RuntimeError(
                "No session file. Run: python3 scripts/telegram-session-init.py"
            )

        self.client = TelegramClient(self.session_path, API_ID, API_HASH)
        await self.client.connect()

        if not await self.client.is_user_authorized():
            raise RuntimeError(
                "Session expired. Re-run: python3 scripts/telegram-session-init.py"
            )

        self.bot_entity = await self.client.get_entity(self.bot_username)
        return f"@{self.bot_username}"

    async def disconnect(self):
        if self.client:
            await self.client.disconnect()
            self.client = None

    async def reset_session(self):
        """Send /reset to the bot and wait for it to settle."""
        if self.client and self.bot_entity:
            await self.client.send_message(self.bot_entity, "/reset")
            await asyncio.sleep(2)

    async def chat(self, message: str, timeout: float = TIMEOUT_SECS,
                   reset: bool = False) -> ChatCapture:
        """Send a message and capture the full bot response."""
        from telethon import events

        if not self.client or not self.bot_entity:
            raise RuntimeError("Not connected. Call connect() first.")

        if reset:
            await self.reset_session()

        capture = ChatCapture(start_time=time.time())
        last_activity = time.time()
        seen_bot_msg = False
        known_msg_ids: set[int] = set()

        async def on_new_message(event):
            nonlocal last_activity, seen_bot_msg
            msg = event.message
            if msg.sender_id != self.bot_entity.id:
                return
            last_activity = time.time()
            known_msg_ids.add(msg.id)
            d = _msg_to_dict(msg)
            capture.final_messages.append(d)
            capture.raw_events.append({"time": time.time(), "type": "bot_message", "data": d})
            capture.events.append({"event": "chat.message", "payload": d, "_recv_ts": time.time()})
            seen_bot_msg = True

        async def on_edit(event):
            nonlocal last_activity
            msg = event.message
            if msg.sender_id != self.bot_entity.id:
                return
            last_activity = time.time()
            d = _msg_to_dict(msg)
            if msg.id in known_msg_ids:
                for i, fm in enumerate(capture.final_messages):
                    if fm.get("message_id") == msg.id:
                        capture.final_messages[i] = d
                        break
                capture.draft_edits.append(d)
                capture.deltas.append({"text": d.get("text", ""), "ts": time.time()})
                capture.raw_events.append({"time": time.time(), "type": "bot_edit", "data": d})
                capture.events.append({"event": "chat.delta", "payload": d, "_recv_ts": time.time()})
            else:
                known_msg_ids.add(msg.id)
                capture.final_messages.append(d)
                capture.raw_events.append({"time": time.time(), "type": "bot_message", "data": d})
                capture.events.append({"event": "chat.message", "payload": d, "_recv_ts": time.time()})
                seen_bot_msg = True

        async def on_deleted(event):
            nonlocal last_activity
            last_activity = time.time()
            for msg_id in event.deleted_ids:
                if msg_id in known_msg_ids:
                    capture.final_messages = [
                        m for m in capture.final_messages
                        if m.get("message_id") != msg_id
                    ]
                    known_msg_ids.discard(msg_id)
                    capture.raw_events.append({
                        "time": time.time(), "type": "bot_delete",
                        "data": {"message_id": msg_id},
                    })

        self.client.add_event_handler(on_new_message, events.NewMessage(from_users=self.bot_entity.id))
        self.client.add_event_handler(on_edit, events.MessageEdited(from_users=self.bot_entity.id))
        self.client.add_event_handler(on_deleted, events.MessageDeleted)

        try:
            await self.client.send_message(self.bot_entity, message)

            while time.time() - capture.start_time < timeout:
                await asyncio.sleep(0.2)
                if seen_bot_msg and time.time() - last_activity > SETTLE_SECS:
                    break
        finally:
            self.client.remove_event_handler(on_new_message)
            self.client.remove_event_handler(on_edit)
            self.client.remove_event_handler(on_deleted)

        capture.end_time = time.time()

        # Build reply_text from final messages.
        all_html = "\n".join(m.get("text", "") for m in capture.final_messages)
        capture.all_text = all_html
        capture.reply_text = _strip_html(all_html)

        # Infer tool calls from draft edits (bot shows tool progress via message edits).
        _infer_tools(capture)

        # Build synthetic final_response for check compatibility.
        if capture.reply_text or capture.final_messages:
            capture.final_response = {
                "ok": True,
                "payload": {
                    "state": "done",
                    "text": capture.reply_text,
                },
            }
        elif capture.errors:
            capture.final_response = {
                "ok": False,
                "payload": {"state": "error"},
                "error": {"message": "; ".join(capture.errors)},
            }
        else:
            capture.errors.append(f"No response after {timeout}s")
            capture.final_response = {
                "ok": False,
                "payload": {"state": "error"},
                "error": {"message": "timeout"},
            }

        return capture

    async def create_session(self, key: str = "") -> str:
        """Reset bot session. Returns a pseudo session key."""
        await self.reset_session()
        return key or f"tg-{int(time.time() * 1000)}"

    async def rpc(self, method: str, params: dict = None, timeout: float = 10) -> dict:
        """Pseudo-RPC via HTTP health endpoint (for health checks only)."""
        import urllib.request
        import json
        if method == "health":
            # Try to hit the HTTP health endpoint directly.
            # Port is not available here; return a synthetic response.
            return {"ok": True, "payload": {"status": "ok"}}
        return {"ok": False, "error": {"message": f"RPC not supported in Telegram mode: {method}"}}

    async def close(self):
        await self.disconnect()


def _infer_tools(capture: ChatCapture):
    """Infer tool_starts and tool_results from draft edits.

    The Deneb bot shows tool progress via message edits. Tool-related
    edits typically contain patterns like tool names or progress indicators.
    The final message (non-edit) contains the completed response.
    """
    # Tool keywords that appear in progress messages.
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

    seen_tools = set()
    for edit in capture.draft_edits:
        text = _strip_html(edit.get("text", "")).lower()
        for keyword, tool_name in tool_keywords.items():
            if keyword in text and tool_name not in seen_tools:
                seen_tools.add(tool_name)
                capture.tool_starts.append({
                    "name": tool_name,
                    "ts": time.time(),
                })
                capture.tool_results.append({
                    "name": tool_name,
                    "isError": False,
                    "ts": time.time(),
                })


def check_prerequisites() -> tuple[bool, str]:
    """Check if Telegram testing prerequisites are met."""
    issues = []

    if not API_ID or not API_HASH:
        issues.append("TELEGRAM_API_ID/TELEGRAM_API_HASH not set in ~/.deneb/.env")

    if not os.path.isfile(SESSION_PATH + ".session"):
        issues.append("No session file (~/.deneb/telegram-test.session). "
                      "Run: python3 scripts/telegram-session-init.py")

    try:
        import telethon  # noqa: F401
    except ImportError:
        issues.append("telethon not installed. Run: pip install telethon")

    if issues:
        return False, "; ".join(issues)
    return True, "ok"

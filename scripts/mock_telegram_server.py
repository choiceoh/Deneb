#!/usr/bin/env python3
"""
Mock Telegram Bot API server for local live-testing of the Deneb gateway.

The Deneb gateway's Telegram plugin supports the TELEGRAM_API_BASE environment
variable to override the Bot API endpoint. This lets us swap the real Telegram
API for a local HTTP server that speaks the same protocol. The gateway runs
unchanged — same plugin, same polling, same send/edit code paths — but the
network hops stay on localhost.

Layout:
    /bot<TOKEN>/<method>   — Telegram Bot API surface (called by the gateway).
    /_test/<action>        — Test-side control plane (called by the test runner).

Telegram API methods implemented (subset that the gateway uses):
    getMe, getUpdates (long-poll), sendMessage, editMessageText,
    deleteMessage, sendChatAction, setMessageReaction, answerCallbackQuery,
    setMyCommands, getMyCommands, sendPhoto, sendDocument, sendVideo,
    sendAudio, sendVoice, getFile.

Test control plane:
    POST /_test/inject        — queue a user message update so getUpdates
                                returns it to the gateway. Body:
                                {"chat_id": 10000001, "text": "hello"}.
                                Returns {"update_id": N, "message_id": M}.
    POST /_test/inject_callback — queue a callback_query update (button click).
                                Body: {"chat_id": ..., "data": "...",
                                "message_id": M}.
    GET  /_test/outbound      — fetch captured outbound events. Query params:
                                chat_id (optional filter),
                                since_seq (optional, default 0).
                                Returns {"seq": N, "events": [...]}.
    POST /_test/reset         — clear all state (updates, outbound, seq).
    GET  /_test/stats         — health/stats snapshot.

Usage:
    python3 scripts/mock_telegram_server.py [--host 127.0.0.1] [--port 18792]

Runs in the foreground. Use SIGTERM/SIGINT to stop cleanly.
"""

from __future__ import annotations

import argparse
import json
import signal
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import parse_qs, urlparse


DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 18792

# Baseline IDs for the fake bot and primary test chat. These values must stay
# in sync with scripts/mock_telegram_client.py so injected updates reach the
# gateway through the same session identity on every run.
BOT_ID = 999999999
BOT_USERNAME = "denebmockbot"
BOT_FIRST_NAME = "Deneb Mock"
DEFAULT_CHAT_ID = 10000001
DEFAULT_USER_ID = 10000001
DEFAULT_USER_FIRST_NAME = "Mock User"

# Long-poll cap for getUpdates. The real Telegram API uses up to 50s but the
# gateway configures DefaultPollTimeout=30. Mirror that so timings feel normal.
LONG_POLL_CAP_SECS = 25

# Event kinds that represent actual content reaching the user. wait_for_outbound
# uses this set to gate its settle timer: a setMessageReaction or sendChatAction
# alone is not enough to conclude the turn is done.
_REPLY_KINDS = frozenset({
    "sendMessage",
    "editMessageText",
    "sendPhoto",
    "sendDocument",
    "sendVideo",
    "sendAudio",
    "sendVoice",
})


class MockState:
    """Thread-safe state for the mock Telegram server.

    Tracks the pending inbound update queue (consumed by getUpdates) plus a
    separate capture log of outbound bot actions (sendMessage, editMessageText,
    deleteMessage, setMessageReaction). The capture log is append-only so the
    test client can poll by sequence number.
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._cond = threading.Condition(self._lock)
        self._next_update_id = 1
        self._next_message_id = 100000
        self._next_seq = 1
        self._pending_updates: list[dict[str, Any]] = []
        self._outbound: list[dict[str, Any]] = []
        self._started_at = time.time()

    # --- Update queue (gateway-facing) ---

    def enqueue_update(self, update: dict[str, Any]) -> int:
        with self._cond:
            uid = self._next_update_id
            self._next_update_id += 1
            update["update_id"] = uid
            self._pending_updates.append(update)
            self._cond.notify_all()
            return uid

    def drain_updates(self, offset: int, timeout_secs: float) -> list[dict[str, Any]]:
        """Return updates with update_id >= offset, blocking up to timeout.

        Mirrors Telegram getUpdates semantics: clients pass offset = last_seen_id
        + 1. Updates with id < offset are discarded (the gateway has already
        confirmed them). Long-polls return as soon as any new update appears.
        """
        deadline = time.monotonic() + max(timeout_secs, 0)
        with self._cond:
            while True:
                if self._pending_updates and self._pending_updates[0]["update_id"] < offset:
                    self._pending_updates = [
                        u for u in self._pending_updates
                        if u["update_id"] >= offset
                    ]
                if self._pending_updates:
                    return list(self._pending_updates)
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return []
                self._cond.wait(timeout=min(remaining, LONG_POLL_CAP_SECS))

    # --- Outbound capture (test-facing) ---

    def record_outbound(self, kind: str, payload: dict[str, Any]) -> dict[str, Any]:
        """Append a bot-side action to the capture log and return the record.

        For sendMessage/sendPhoto/etc the record includes a newly minted
        message_id so editMessageText/deleteMessage can refer back to it later.
        """
        with self._cond:
            seq = self._next_seq
            self._next_seq += 1
            record = {
                "seq": seq,
                "kind": kind,
                "ts": time.time(),
                "payload": payload,
            }
            # Mint a message_id for send-family kinds so the gateway can
            # subsequently edit/delete the message by id.
            if kind in ("sendMessage", "sendPhoto", "sendDocument",
                        "sendVideo", "sendAudio", "sendVoice"):
                mid = self._next_message_id
                self._next_message_id += 1
                record["message_id"] = mid
            self._outbound.append(record)
            self._cond.notify_all()
            return record

    def fetch_outbound(self, since_seq: int, chat_id: int | None) -> list[dict[str, Any]]:
        with self._lock:
            events = [r for r in self._outbound if r["seq"] > since_seq]
        if chat_id is not None:
            events = [
                r for r in events
                if _extract_chat_id(r.get("payload", {})) == chat_id
            ]
        return events

    def wait_for_outbound(
        self,
        since_seq: int,
        chat_id: int | None,
        settle_secs: float,
        timeout_secs: float,
    ) -> list[dict[str, Any]]:
        """Block until outbound activity settles or timeout elapses.

        Returns all events with seq > since_seq. Settle rule: once a
        reply-worthy event has arrived (sendMessage / editMessageText /
        sendPhoto / etc.), wait settle_secs of quiet before returning.
        Non-reply events alone (setMessageReaction, sendChatAction,
        deleteMessage) do NOT start the settle timer — a gateway turn
        typically sends a reaction and typing indicator within the first
        second, but the actual reply text may not land for 5–15 seconds.
        Settling on reactions alone made tests falsely report
        "no response" when the real answer was still being generated.
        """
        deadline = time.monotonic() + max(timeout_secs, 0)
        last_activity = None
        with self._cond:
            while True:
                events = [r for r in self._outbound if r["seq"] > since_seq]
                if chat_id is not None:
                    events = [
                        r for r in events
                        if _extract_chat_id(r.get("payload", {})) == chat_id
                    ]
                reply_events = [
                    r for r in events if r.get("kind") in _REPLY_KINDS
                ]
                if reply_events:
                    newest_ts = max(r["ts"] for r in reply_events)
                    if last_activity is None or newest_ts > last_activity:
                        last_activity = newest_ts
                    quiet_for = time.time() - last_activity
                    if quiet_for >= settle_secs:
                        return events
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return events
                # Wake periodically to re-evaluate the settle window.
                self._cond.wait(timeout=min(remaining, 0.2))

    def next_message_id(self) -> int:
        """Allocate a message_id for synthetic inbound updates."""
        with self._lock:
            mid = self._next_message_id
            self._next_message_id += 1
            return mid

    def reset(self) -> None:
        with self._cond:
            self._next_update_id = 1
            self._next_message_id = 100000
            self._next_seq = 1
            self._pending_updates.clear()
            self._outbound.clear()
            self._cond.notify_all()

    def stats(self) -> dict[str, Any]:
        with self._lock:
            return {
                "uptime_secs": round(time.time() - self._started_at, 1),
                "pending_updates": len(self._pending_updates),
                "outbound_events": len(self._outbound),
                "next_update_id": self._next_update_id,
                "next_message_id": self._next_message_id,
                "next_seq": self._next_seq,
            }


def _extract_chat_id(payload: dict[str, Any]) -> int | None:
    val = payload.get("chat_id")
    if isinstance(val, int):
        return val
    if isinstance(val, str) and val.lstrip("-").isdigit():
        return int(val)
    return None


class MockTelegramHandler(BaseHTTPRequestHandler):
    state: MockState  # set on the server instance

    # Suppress default access log; routes are chatty.
    def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
        return

    # --- Shared helpers ---

    def _read_json(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0:
            return {}
        raw = self.rfile.read(length)
        if not raw:
            return {}
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return {}

    def _write_json(self, status: int, body: dict[str, Any]) -> None:
        data = json.dumps(body).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _ok(self, result: Any) -> None:
        self._write_json(200, {"ok": True, "result": result})

    def _api_error(self, code: int, description: str) -> None:
        self._write_json(200, {"ok": False, "error_code": code, "description": description})

    # --- Routing ---

    def do_GET(self) -> None:  # noqa: N802 (stdlib naming)
        parsed = urlparse(self.path)
        path = parsed.path
        if path == "/_test/outbound":
            self._handle_outbound(parsed.query)
        elif path == "/_test/wait":
            # GET so the client can use urllib.request.urlopen without a body.
            self._handle_wait(parsed.query)
        elif path == "/_test/stats":
            self._write_json(200, self.state.stats())
        elif path == "/_test/health":
            self._write_json(200, {"ok": True, "status": "mock-telegram-ready"})
        else:
            self._write_json(404, {"ok": False, "error": "not_found", "path": path})

    def do_POST(self) -> None:  # noqa: N802 (stdlib naming)
        parsed = urlparse(self.path)
        path = parsed.path

        if path == "/_test/inject":
            self._handle_inject_message()
            return
        if path == "/_test/inject_callback":
            self._handle_inject_callback()
            return
        if path == "/_test/reset":
            self.state.reset()
            self._write_json(200, {"ok": True})
            return

        # /bot<TOKEN>/<method>
        if path.startswith("/bot") and "/" in path[1:]:
            parts = path.split("/", 3)
            if len(parts) >= 3:
                method = parts[2]
                self._handle_bot_method(method)
                return

        self._write_json(404, {"ok": False, "error": "not_found", "path": path})

    # --- Telegram Bot API surface ---

    def _handle_bot_method(self, method: str) -> None:
        params = self._read_json()
        handler = _BOT_METHODS.get(method)
        if handler is None:
            # Unknown method — stay quiet like real Telegram and return an OK
            # empty response so unrelated calls don't crash polling loops.
            self._ok(True)
            return
        try:
            handler(self, params)
        except Exception as exc:  # pragma: no cover - defensive
            self._api_error(500, f"mock internal error: {exc}")

    def _handle_get_me(self, _params: dict[str, Any]) -> None:
        self._ok({
            "id": BOT_ID,
            "is_bot": True,
            "first_name": BOT_FIRST_NAME,
            "username": BOT_USERNAME,
            "can_join_groups": False,
            "can_read_all_group_messages": False,
            "supports_inline_queries": False,
        })

    def _handle_get_updates(self, params: dict[str, Any]) -> None:
        offset = int(params.get("offset") or 0)
        timeout = float(params.get("timeout") or 0)
        # Clamp so errant configs don't pin a socket forever.
        timeout = min(max(timeout, 0), LONG_POLL_CAP_SECS)
        updates = self.state.drain_updates(offset, timeout)
        self._ok(updates)

    def _handle_send_message(self, params: dict[str, Any]) -> None:
        record = self.state.record_outbound("sendMessage", params)
        self._ok(self._fake_message_result(params, record["message_id"]))

    def _handle_edit_message_text(self, params: dict[str, Any]) -> None:
        record = self.state.record_outbound("editMessageText", params)
        message_id = int(params.get("message_id") or record.get("message_id") or 0)
        self._ok(self._fake_message_result(params, message_id, edited=True))

    def _handle_delete_message(self, params: dict[str, Any]) -> None:
        self.state.record_outbound("deleteMessage", params)
        self._ok(True)

    def _handle_chat_action(self, params: dict[str, Any]) -> None:
        self.state.record_outbound("sendChatAction", params)
        self._ok(True)

    def _handle_set_reaction(self, params: dict[str, Any]) -> None:
        self.state.record_outbound("setMessageReaction", params)
        self._ok(True)

    def _handle_answer_callback(self, params: dict[str, Any]) -> None:
        self.state.record_outbound("answerCallbackQuery", params)
        self._ok(True)

    def _handle_set_my_commands(self, params: dict[str, Any]) -> None:
        self.state.record_outbound("setMyCommands", params)
        self._ok(True)

    def _handle_get_my_commands(self, _params: dict[str, Any]) -> None:
        self._ok([])

    def _handle_send_media(self, kind: str, params: dict[str, Any]) -> None:
        record = self.state.record_outbound(kind, params)
        self._ok(self._fake_message_result(params, record["message_id"]))

    def _handle_get_file(self, _params: dict[str, Any]) -> None:
        # Mock mode: no real files. Return an error matching Telegram's format
        # so the gateway treats it as a recoverable API error.
        self._api_error(404, "mock: file downloads are not supported")

    def _fake_message_result(
        self,
        params: dict[str, Any],
        message_id: int,
        edited: bool = False,
    ) -> dict[str, Any]:
        chat_id = _extract_chat_id(params) or DEFAULT_CHAT_ID
        result = {
            "message_id": message_id,
            "from": {
                "id": BOT_ID,
                "is_bot": True,
                "first_name": BOT_FIRST_NAME,
                "username": BOT_USERNAME,
            },
            "chat": {"id": chat_id, "type": "private"},
            "date": int(time.time()),
            "text": params.get("text", ""),
        }
        if edited:
            result["edit_date"] = int(time.time())
        return result

    # --- Test control plane ---

    def _handle_inject_message(self) -> None:
        body = self._read_json()
        chat_id = int(body.get("chat_id") or DEFAULT_CHAT_ID)
        from_id = int(body.get("from_id") or DEFAULT_USER_ID)
        first_name = str(body.get("first_name") or DEFAULT_USER_FIRST_NAME)
        text = str(body.get("text") or "")
        if not text:
            self._write_json(400, {"ok": False, "error": "text is required"})
            return

        msg_id = self.state.next_message_id()
        update = {
            "message": {
                "message_id": msg_id,
                "date": int(time.time()),
                "chat": {
                    "id": chat_id,
                    "type": "private",
                    "first_name": first_name,
                },
                "from": {
                    "id": from_id,
                    "is_bot": False,
                    "first_name": first_name,
                },
                "text": text,
            }
        }
        update_id = self.state.enqueue_update(update)
        self._write_json(200, {
            "ok": True,
            "update_id": update_id,
            "message_id": msg_id,
            "chat_id": chat_id,
        })

    def _handle_inject_callback(self) -> None:
        body = self._read_json()
        chat_id = int(body.get("chat_id") or DEFAULT_CHAT_ID)
        from_id = int(body.get("from_id") or DEFAULT_USER_ID)
        message_id = int(body.get("message_id") or 0)
        data = str(body.get("data") or "")
        if not data or not message_id:
            self._write_json(400, {"ok": False, "error": "message_id and data required"})
            return

        update = {
            "callback_query": {
                "id": f"mock-cb-{int(time.time() * 1000)}",
                "from": {
                    "id": from_id,
                    "is_bot": False,
                    "first_name": DEFAULT_USER_FIRST_NAME,
                },
                "message": {
                    "message_id": message_id,
                    "date": int(time.time()),
                    "chat": {"id": chat_id, "type": "private"},
                    "from": {
                        "id": BOT_ID,
                        "is_bot": True,
                        "first_name": BOT_FIRST_NAME,
                    },
                    "text": "",
                },
                "data": data,
                "chat_instance": f"mock-chat-{chat_id}",
            }
        }
        update_id = self.state.enqueue_update(update)
        self._write_json(200, {"ok": True, "update_id": update_id})

    def _handle_outbound(self, query_string: str) -> None:
        qs = parse_qs(query_string)
        since_seq = int((qs.get("since_seq") or ["0"])[0])
        chat_id_val = (qs.get("chat_id") or [""])[0]
        chat_id = int(chat_id_val) if chat_id_val else None
        events = self.state.fetch_outbound(since_seq, chat_id)
        next_seq = max([e["seq"] for e in events], default=since_seq)
        self._write_json(200, {
            "ok": True,
            "seq": next_seq,
            "events": events,
        })

    def _handle_wait(self, query_string: str) -> None:
        """Block until outbound traffic settles, then return captured events."""
        qs = parse_qs(query_string)
        since_seq = int((qs.get("since_seq") or ["0"])[0])
        chat_id_val = (qs.get("chat_id") or [""])[0]
        chat_id = int(chat_id_val) if chat_id_val else None
        settle = float((qs.get("settle") or ["3.0"])[0])
        timeout = float((qs.get("timeout") or ["120"])[0])
        events = self.state.wait_for_outbound(since_seq, chat_id, settle, timeout)
        next_seq = max([e["seq"] for e in events], default=since_seq)
        self._write_json(200, {
            "ok": True,
            "seq": next_seq,
            "events": events,
        })


# Bot method dispatch table. Keys are Telegram method names; values are bound
# method names on MockTelegramHandler called as `handler(params)`.
_BOT_METHODS: dict[str, Any] = {
    "getMe":               MockTelegramHandler._handle_get_me,
    "getUpdates":          MockTelegramHandler._handle_get_updates,
    "sendMessage":         MockTelegramHandler._handle_send_message,
    "editMessageText":     MockTelegramHandler._handle_edit_message_text,
    "deleteMessage":       MockTelegramHandler._handle_delete_message,
    "sendChatAction":      MockTelegramHandler._handle_chat_action,
    "setMessageReaction":  MockTelegramHandler._handle_set_reaction,
    "answerCallbackQuery": MockTelegramHandler._handle_answer_callback,
    "setMyCommands":       MockTelegramHandler._handle_set_my_commands,
    "getMyCommands":       MockTelegramHandler._handle_get_my_commands,
    "getFile":             MockTelegramHandler._handle_get_file,
    "sendPhoto":           lambda h, p: h._handle_send_media("sendPhoto", p),
    "sendDocument":        lambda h, p: h._handle_send_media("sendDocument", p),
    "sendVideo":           lambda h, p: h._handle_send_media("sendVideo", p),
    "sendAudio":           lambda h, p: h._handle_send_media("sendAudio", p),
    "sendVoice":           lambda h, p: h._handle_send_media("sendVoice", p),
}


def build_server(host: str, port: int) -> ThreadingHTTPServer:
    handler_cls = MockTelegramHandler
    server = ThreadingHTTPServer((host, port), handler_cls)
    handler_cls.state = MockState()  # shared across requests
    return server


def main() -> int:
    parser = argparse.ArgumentParser(description="Deneb mock Telegram Bot API server")
    parser.add_argument("--host", default=DEFAULT_HOST)
    parser.add_argument("--port", type=int, default=DEFAULT_PORT)
    args = parser.parse_args()

    server = build_server(args.host, args.port)

    def shutdown(_signum: int, _frame: Any) -> None:
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)

    print(f"mock-telegram listening on http://{args.host}:{args.port}")
    print(f"  bot route:  /bot<TOKEN>/<method>")
    print(f"  test route: /_test/inject, /_test/outbound, /_test/reset")
    sys.stdout.flush()
    try:
        server.serve_forever(poll_interval=0.2)
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    sys.exit(main())

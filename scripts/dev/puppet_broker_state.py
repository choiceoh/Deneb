"""Thread-safe held-request state and OpenAI payload helpers for puppet broker."""
from __future__ import annotations

import hashlib
import json
import os
import re
import threading
import time
from collections import deque

KEEPALIVE_INTERVAL = 10.0  # SSE comment cadence while a request is held
STREAM_HOLD_MAX = 1800.0  # absolute cap on holding a streaming request
# Non-stream requests can't keepalive; the broker must answer while the
# gateway is still listening, i.e. inside the 5-minute turn deadline
# (DefaultTurnDeadline) — not merely inside the 10-minute client timeout.
COMPLETE_HOLD_MAX = 270.0
PENDING_WAIT_CAP = 55.0  # long-poll cap (under common curl timeouts)
HISTORY_LIMIT = 100
# Finished requests whose FULL payload stays re-inspectable via `show`.
# ~100-200KB each in broker memory; the journal only keeps a truncated tail.
FINISHED_KEEP = 16
# Per-role seat aliases puppet.sh assigns in the config overlay. Listed in
# /v1/models so any discovery-style probe sees them; the broker itself
# accepts and echoes whatever model id a request carries.
SEAT_MODELS = ("main-seat", "lightweight-seat", "fallback-seat", "tiny-seat",
               "analysis-seat", "coding-seat", "chatbot-seat", "vision-seat",
               "subagent-seat")
JOURNAL_TRUNC = 2000  # per-field char cap when journaling requests
JOURNAL_MAX_BYTES = 5_000_000  # rotate past this — /tmp is tmpfs on the DGX
# Long text/arguments are emitted as multiple deltas: the gateway's SSE
# parser caps one line at 1 MB (internal/ai/llm/sse.go) and one delta is one
# data line. 48K chars stays far under the cap even after JSON escaping.
SSE_DELTA_CHARS = 48_000


# ---------------------------------------------------------------------------
# Broker state
# ---------------------------------------------------------------------------

class HeldRequest:
    """One /chat/completions request waiting for an operator response."""

    def __init__(self, rid: str, payload: dict, kind: str):
        self.id = rid
        self.payload = payload
        self.kind = kind  # "stream" | "complete"
        self.created = time.time()
        self.event = threading.Event()
        self.response: dict | None = None
        # Live states: waiting → answered. Finished (history) states:
        # done | failed | gone.
        self.status = "waiting"
        # System-prompt fingerprint, computed once at registration: `show`
        # compares it across requests so prompt-cache prefix stability (the
        # seat's core verification target) is visible without diffing 40K+
        # chars by eye.
        self.sys_hash, self.sys_chars = system_fingerprint(payload)

    def summary(self) -> dict:
        msgs = self.payload.get("messages") or []
        last = msgs[-1] if msgs else {}
        return {
            "id": self.id,
            "ageSec": round(time.time() - self.created, 1),
            "kind": self.kind,
            "model": self.payload.get("model", ""),
            "messages": len(msgs),
            "tools": len(self.payload.get("tools") or []),
            "sysHash": self.sys_hash,
            "sysChars": self.sys_chars,
            "lastRole": last.get("role", ""),
            "lastPreview": content_preview(last.get("content"), 200),
        }


class Broker:
    """Held-request registry. All mutating access goes through self.cond."""

    def __init__(self, model: str, journal_path: str):
        self.model = model
        self.journal_path = journal_path
        self.cond = threading.Condition()
        self.requests: dict[str, HeldRequest] = {}
        self.history: deque = deque(maxlen=HISTORY_LIMIT)
        # Finished entries with their FULL payload (insertion-ordered ids in
        # finished_order for FIFO eviction) — `show` on an answered request
        # falls back here instead of dying on "unknown request id".
        self.finished: dict[str, HeldRequest] = {}
        self.finished_order: deque = deque()
        self.seq = 0
        self.started = time.time()

    def register(self, payload: dict, kind: str) -> HeldRequest:
        with self.cond:
            self.seq += 1
            rid = f"r{self.seq}"
            entry = HeldRequest(rid, payload, kind)
            self.requests[rid] = entry
            self.cond.notify_all()
            return entry

    def pending(self) -> list[dict]:
        with self.cond:
            return [e.summary() for e in self.requests.values()
                    if e.status == "waiting"]

    def wait_pending(self, timeout: float) -> list[dict]:
        deadline = time.time() + min(timeout, PENDING_WAIT_CAP)
        with self.cond:
            while True:
                out = [e.summary() for e in self.requests.values()
                       if e.status == "waiting"]
                if out or time.time() >= deadline:
                    return out
                self.cond.wait(timeout=0.25)

    def get(self, rid: str) -> HeldRequest | None:
        with self.cond:
            return self.requests.get(rid)

    def get_any(self, rid: str) -> HeldRequest | None:
        """Live entry, or a finished one still in the retention window."""
        with self.cond:
            return self.requests.get(rid) or self.finished.get(rid)

    def respond(self, rid: str, response: dict) -> tuple[bool, str]:
        with self.cond:
            entry = self.requests.get(rid)
            if entry is None:
                done = self.finished.get(rid)
                if done is not None:
                    return False, (f"request {rid} already finished "
                                   f"(status={done.status}) — inspect it with "
                                   "show, it cannot be re-answered")
                return False, f"unknown request id {rid!r}"
            if entry.status != "waiting":
                return False, (f"request {rid} is {entry.status} "
                               "(gateway likely cancelled — turn deadline?)")
            entry.response = response
            entry.status = "answered"
            entry.event.set()
            return True, ""

    def final_response(self, entry: HeldRequest, timeout_message: str) -> dict:
        """Resolve a held request's response exactly once, under the lock.

        If the operator answered first, their payload wins. Otherwise the
        entry is claimed with a timeout error, so a late respond() is
        rejected loudly (status != waiting) instead of racing an
        unsynchronized overwrite and being silently discarded.
        """
        with self.cond:
            if entry.status == "waiting":
                entry.response = {"error": timeout_message}
                entry.status = "answered"
            return entry.response or {}

    def finish(self, entry: HeldRequest, status: str) -> None:
        """Move a request out of the live map and journal the exchange.

        Idempotent: only the first caller records; later calls no-op, so the
        try/finally safety net around the hold paths can't double-journal.
        """
        with self.cond:
            if self.requests.pop(entry.id, None) is None:
                return
            entry.status = status
            record = {
                "ts": round(time.time(), 3),
                "id": entry.id,
                "kind": entry.kind,
                "status": status,
                "heldSec": round(time.time() - entry.created, 1),
                "summary": entry.summary(),
                "response": entry.response,
            }
            self.history.append(record)
            # Keep the full payload re-inspectable for a while (bounded FIFO).
            self.finished[entry.id] = entry
            self.finished_order.append(entry.id)
            while len(self.finished_order) > FINISHED_KEEP:
                self.finished.pop(self.finished_order.popleft(), None)
        self._journal(entry, record)

    def _journal(self, entry: HeldRequest, record: dict) -> None:
        if not self.journal_path:
            return
        # Journal a truncated request so postmortems don't need the broker
        # alive; /tmp is small on some hosts, so cap every text field.
        msgs = []
        for m in (entry.payload.get("messages") or [])[-6:]:
            msgs.append({
                "role": m.get("role"),
                "content": content_preview(m.get("content"), JOURNAL_TRUNC),
                "tool_calls": m.get("tool_calls"),
                "tool_call_id": m.get("tool_call_id"),
            })
        line = dict(record)
        line["requestTail"] = msgs
        try:
            try:
                if os.path.getsize(self.journal_path) > JOURNAL_MAX_BYTES:
                    os.replace(self.journal_path, self.journal_path + ".1")
            except OSError:
                pass  # journal doesn't exist yet — the append creates it
            with open(self.journal_path, "a", encoding="utf-8") as f:
                f.write(json.dumps(line, ensure_ascii=False) + "\n")
        except OSError:
            pass

    def state(self) -> dict:
        with self.cond:
            return {
                "model": self.model,
                "uptimeSec": round(time.time() - self.started, 1),
                "waiting": sum(1 for e in self.requests.values()
                               if e.status == "waiting"),
                "served": self.seq,
            }


def content_preview(content, limit: int) -> str:
    """Flatten an OpenAI message content (str or parts list) to a preview."""
    if content is None:
        return ""
    if isinstance(content, str):
        text = content
    elif isinstance(content, list):
        parts = []
        for p in content:
            if isinstance(p, dict):
                if p.get("type") == "text":
                    parts.append(str(p.get("text", "")))
                elif p.get("type") == "image_url":
                    url = str((p.get("image_url") or {}).get("url", ""))
                    parts.append(f"[image:{len(url)}b]")
                else:
                    parts.append(f"[{p.get('type', 'part')}]")
            else:
                parts.append(str(p))
        text = " ".join(parts)
    else:
        text = str(content)
    # Slice before escaping — this runs on every pending poll and the
    # content can be hundreds of KB; never copy more than the preview needs.
    total = len(text)
    snippet = text[:limit].replace("\n", "\\n")
    if total > limit:
        snippet += f"… (+{total - limit} chars)"
    return snippet


def content_text(content) -> str:
    """Flatten an OpenAI message content (str or parts list) to full text."""
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for p in content:
            if isinstance(p, dict):
                if p.get("type") == "text":
                    parts.append(str(p.get("text", "")))
                elif p.get("type") == "image_url":
                    url = str((p.get("image_url") or {}).get("url", ""))
                    parts.append(f"[image:{len(url)}b]")
                else:
                    parts.append(f"[{p.get('type', 'part')}]")
            else:
                parts.append(str(p))
        return " ".join(parts)
    return str(content)


def system_fingerprint(payload: dict) -> tuple[str, int]:
    """Short hash + char count of the request's system message ("" if none)."""
    for m in payload.get("messages") or []:
        if m.get("role") == "system":
            text = content_text(m.get("content"))
            digest = hashlib.sha256(text.encode("utf-8")).hexdigest()[:8]
            return digest, len(text)
    return "", 0


def system_outline(text: str) -> list:
    """(chars, heading) rows for a markdown system prompt, sized to the next
    heading — the seat's quick map of what the prompt assembly produced."""
    heads = [(m.start(), m.group(1))
             for m in re.finditer(r"^(#{1,3} .+)$", text, re.M)]
    rows = []
    for i, (pos, head) in enumerate(heads):
        end = heads[i + 1][0] if i + 1 < len(heads) else len(text)
        rows.append((end - pos, head))
    return rows


def estimate_tokens(obj) -> int:
    try:
        return max(1, len(json.dumps(obj, ensure_ascii=False)) // 4)
    except (TypeError, ValueError):
        return 1


def usage_payload(prompt_obj, response_obj) -> dict:
    prompt = estimate_tokens(prompt_obj)
    completion = estimate_tokens(response_obj)
    return {"prompt_tokens": prompt, "completion_tokens": completion,
            "total_tokens": prompt + completion}


def split_delta(s: str) -> list:
    """Split a long string into SSE-safe delta pieces (>=1, even for "")."""
    return [s[i:i + SSE_DELTA_CHARS]
            for i in range(0, len(s), SSE_DELTA_CHARS)] or [""]

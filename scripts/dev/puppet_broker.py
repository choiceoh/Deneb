#!/usr/bin/env python3
"""Puppet broker — let a coding agent sit in Deneb's agent seat.

The broker is a local OpenAI-compatible endpoint the dev gateway is pointed at
(via scripts/dev/puppet.sh, which rewrites models.providers + agents.*Model in
the generated dev config). Instead of answering /chat/completions itself, it
HOLDS each request until an operator (a human, or a coding agent like Claude
Code) inspects it and submits the response. The operator therefore sees exactly
what Deneb's LLM sees — assembled system prompt, full message history, tool
schemas — and decides text/tool_calls, while the gateway executes the chosen
tools for real and loops back with the results. "Becoming the Deneb agent."

Server mode (started by puppet.sh):
    python3 puppet_broker.py serve --port 18793 [--model agent-seat]

Gateway-facing endpoints (OpenAI wire):
    POST /v1/chat/completions   stream + non-stream; held until respond/fail
    GET  /v1/models, /models    vLLM-style discovery (static catalog)

Operator endpoints (also wrapped by `puppet_broker.py <cmd>` CLI):
    GET  /puppet/health         broker liveness
    GET  /puppet/state          counts + uptime
    GET  /puppet/pending?wait=N long-poll for waiting requests (summaries)
    GET  /puppet/request/<id>   full request payload + meta
    POST /puppet/respond/<id>   {"text","reasoning","tool_calls",...} or {"error"}
    GET  /puppet/history        recent completed exchanges

Client mode (operator CLI; broker URL from --broker or DENEB_PUPPET_URL):
    python3 puppet_broker.py pending [--wait N]
    python3 puppet_broker.py show ID [--full | --raw]
    python3 puppet_broker.py reply ID [--text T] [--reasoning R]
                                      [--tool NAME ARGS_JSON]... [--finish FR]
                                      [--file PAYLOAD_JSON]
    python3 puppet_broker.py fail ID [--message M]
    python3 puppet_broker.py history

Timeout choreography (why keepalives exist):
  - gateway LLM HTTP client: 10 min hard cap, >=5 min per-request context
    (internal/ai/llm/client.go) — SSE comment lines keep bytes flowing.
  - miniapp.chat.send turn deadline: 5 min for the WHOLE turn
    (internal/runtime/server DefaultTurnDeadline). When it fires the gateway
    drops the connection; the next keepalive write fails and the request is
    marked gone.

Stdlib only — no external dependencies (matches mock_native_client.py).
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.request
from collections import deque
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

KEEPALIVE_INTERVAL = 10.0  # SSE comment cadence while a request is held
STREAM_HOLD_MAX = 1800.0  # absolute cap on holding a streaming request
COMPLETE_HOLD_MAX = 540.0  # non-stream requests can't keepalive; stay <10min
PENDING_WAIT_CAP = 55.0  # long-poll cap (under common curl timeouts)
HISTORY_LIMIT = 100
JOURNAL_TRUNC = 2000  # per-field char cap when journaling requests


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
        self.status = "waiting"  # waiting|answered|gone|expired

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

    def respond(self, rid: str, response: dict) -> tuple[bool, str]:
        with self.cond:
            entry = self.requests.get(rid)
            if entry is None:
                return False, f"unknown request id {rid!r}"
            if entry.status != "waiting":
                return False, (f"request {rid} is {entry.status} "
                               "(gateway likely cancelled — turn deadline?)")
            entry.response = response
            entry.status = "answered"
            entry.event.set()
            return True, ""

    def finish(self, entry: HeldRequest, status: str) -> None:
        """Move a request out of the live map and journal the exchange."""
        with self.cond:
            self.requests.pop(entry.id, None)
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
    text = text.replace("\n", "\\n")
    if len(text) > limit:
        return text[:limit] + f"… (+{len(text) - limit} chars)"
    return text


def estimate_tokens(obj) -> int:
    try:
        return max(1, len(json.dumps(obj, ensure_ascii=False)) // 4)
    except (TypeError, ValueError):
        return 1


# ---------------------------------------------------------------------------
# HTTP handler
# ---------------------------------------------------------------------------

BROKER: Broker | None = None


class Handler(BaseHTTPRequestHandler):
    # HTTP/1.0 keeps SSE simple: no chunked encoding, body ends at close.
    # Go's net/http reads EOF-delimited bodies fine.

    def log_message(self, fmt, *args):  # quiet default access log
        sys.stderr.write("%s - %s\n" % (self.address_string(), fmt % args))

    # -- helpers ------------------------------------------------------------

    def _json(self, code: int, obj: dict) -> None:
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _read_body(self) -> dict | None:
        try:
            length = int(self.headers.get("Content-Length") or 0)
            raw = self.rfile.read(length) if length else b"{}"
            return json.loads(raw.decode("utf-8") or "{}")
        except (ValueError, OSError):
            return None

    # -- routing ------------------------------------------------------------

    def do_GET(self):  # noqa: N802 (BaseHTTPRequestHandler API)
        path, _, query = self.path.partition("?")
        params = dict(p.split("=", 1) for p in query.split("&") if "=" in p)
        if path in ("/v1/models", "/models"):
            self._json(200, {"object": "list", "data": [
                {"id": BROKER.model, "object": "model",
                 "max_model_len": 200000},
            ]})
        elif path == "/puppet/health":
            self._json(200, {"status": "ready", **BROKER.state()})
        elif path == "/puppet/state":
            self._json(200, BROKER.state())
        elif path == "/puppet/pending":
            wait = float(params.get("wait", "0") or 0)
            items = (BROKER.wait_pending(wait) if wait > 0
                     else BROKER.pending())
            self._json(200, {"pending": items})
        elif path.startswith("/puppet/request/"):
            rid = path.rsplit("/", 1)[-1]
            entry = BROKER.get(rid)
            if entry is None:
                self._json(404, {"error": f"unknown request id {rid!r}"})
            else:
                self._json(200, {**entry.summary(),
                                 "request": entry.payload})
        elif path == "/puppet/history":
            with BROKER.cond:
                items = list(BROKER.history)
            self._json(200, {"history": items})
        else:
            self._json(404, {"error": f"no route {path}"})

    def do_POST(self):  # noqa: N802
        path = self.path.partition("?")[0]
        if path in ("/v1/chat/completions", "/chat/completions"):
            self._chat_completions()
        elif path.startswith("/puppet/respond/"):
            rid = path.rsplit("/", 1)[-1]
            body = self._read_body()
            if body is None:
                self._json(400, {"error": "invalid JSON body"})
                return
            ok, msg = BROKER.respond(rid, body)
            self._json(200 if ok else 409, {"ok": ok, "error": msg or None})
        else:
            self._json(404, {"error": f"no route {path}"})

    # -- the held LLM request ------------------------------------------------

    def _chat_completions(self) -> None:
        payload = self._read_body()
        if payload is None:
            self._json(400, {"error": {"message": "invalid JSON",
                                       "type": "bad_request"}})
            return
        kind = "stream" if payload.get("stream") else "complete"
        entry = BROKER.register(payload, kind)
        sys.stderr.write(
            f"[puppet] held {entry.id}: kind={kind} "
            f"messages={len(payload.get('messages') or [])} "
            f"tools={len(payload.get('tools') or [])}\n")
        if kind == "stream":
            self._hold_stream(entry)
        else:
            self._hold_complete(entry)

    def _hold_stream(self, entry: HeldRequest) -> None:
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        # Flush headers now — wfile is buffered and the next write may be a
        # keepalive 10s away; the gateway should see the stream open at once.
        if not self._write(b": seat\n\n"):
            BROKER.finish(entry, "gone")
            return

        deadline = time.time() + STREAM_HOLD_MAX
        answered = False
        while time.time() < deadline:
            if entry.event.wait(timeout=KEEPALIVE_INTERVAL):
                answered = True
                break
            # SSE comment line — ignored by the gateway's parser (sse.go)
            # but keeps bytes flowing under its 10-minute client timeout,
            # and detects a gateway-side cancel (write fails → gone).
            if not self._write(b": hold\n\n"):
                BROKER.finish(entry, "gone")
                return
        if not answered:
            entry.response = {"error": "puppet hold timeout "
                                       f"({int(STREAM_HOLD_MAX)}s)"}
        self._emit_sse_response(entry)

    def _write(self, data: bytes) -> bool:
        try:
            self.wfile.write(data)
            self.wfile.flush()
            return True
        except OSError:
            return False

    def _emit_sse_response(self, entry: HeldRequest) -> None:
        resp = entry.response or {}
        if resp.get("error"):
            # `event: error` is required — a bare data:{"error":...} parses as
            # an empty chunk and is silently skipped (openai_stream.go).
            payload = json.dumps({"type": "puppet_abort",
                                  "message": str(resp["error"])},
                                 ensure_ascii=False)
            ok = self._write(f"event: error\ndata: {payload}\n\n"
                             .encode("utf-8"))
            BROKER.finish(entry, "failed" if ok else "gone")
            return

        rid = f"chatcmpl-puppet-{entry.id}"
        base = {"id": rid, "object": "chat.completion.chunk",
                "created": int(time.time()), "model": BROKER.model}

        def chunk(delta: dict, finish=None, usage=None) -> bytes:
            c = dict(base)
            c["choices"] = [{"index": 0, "delta": delta,
                             "finish_reason": finish}]
            if usage is not None:
                c["usage"] = usage
            return ("data: " + json.dumps(c, ensure_ascii=False) + "\n\n") \
                .encode("utf-8")

        out = [chunk({"role": "assistant"})]
        if resp.get("reasoning"):
            out.append(chunk({"reasoning_content": str(resp["reasoning"])}))
        if resp.get("text") is not None:
            out.append(chunk({"content": str(resp.get("text") or "")}))
        tool_calls = resp.get("tool_calls") or []
        for i, tc in enumerate(tool_calls):
            args = tc.get("arguments", {})
            if not isinstance(args, str):
                args = json.dumps(args, ensure_ascii=False)
            out.append(chunk({"tool_calls": [{
                "index": i,
                "id": tc.get("id") or f"call_{entry.id}_{i}",
                "type": "function",
                "function": {"name": str(tc.get("name", "")),
                             "arguments": args},
            }]}))
        finish = resp.get("finish_reason") or \
            ("tool_calls" if tool_calls else "stop")
        usage = {
            "prompt_tokens": estimate_tokens(entry.payload.get("messages")),
            "completion_tokens": estimate_tokens(resp),
        }
        usage["total_tokens"] = usage["prompt_tokens"] + \
            usage["completion_tokens"]
        out.append(chunk({}, finish=finish, usage=usage))
        out.append(b"data: [DONE]\n\n")

        ok = all(self._write(b) for b in out)
        BROKER.finish(entry, "done" if ok else "gone")

    def _hold_complete(self, entry: HeldRequest) -> None:
        # Non-stream callers (e.g. title generation via Complete()) can't
        # receive keepalives; the gateway's 10-minute client timeout bounds us.
        if not entry.event.wait(timeout=COMPLETE_HOLD_MAX):
            entry.response = {"error": "puppet hold timeout "
                                       f"({int(COMPLETE_HOLD_MAX)}s)"}
        resp = entry.response or {}
        if resp.get("error"):
            # 400 = permanent for the gateway's retry logic (no retry storm).
            self._json(400, {"error": {"message": str(resp["error"]),
                                       "type": "puppet_abort"}})
            BROKER.finish(entry, "failed")
            return
        text = str(resp.get("text") or "")
        usage = {
            "prompt_tokens": estimate_tokens(entry.payload.get("messages")),
            "completion_tokens": estimate_tokens(text),
        }
        usage["total_tokens"] = usage["prompt_tokens"] + \
            usage["completion_tokens"]
        self._json(200, {
            "id": f"chatcmpl-puppet-{entry.id}",
            "object": "chat.completion",
            "model": BROKER.model,
            "choices": [{"index": 0, "finish_reason": "stop",
                         "message": {"role": "assistant", "content": text}}],
            "usage": usage,
        })
        BROKER.finish(entry, "done")


def serve(args) -> int:
    global BROKER
    BROKER = Broker(model=args.model, journal_path=args.journal)
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    server.daemon_threads = True
    sys.stderr.write(f"[puppet] broker on http://{args.host}:{args.port} "
                     f"model={args.model} journal={args.journal or '-'}\n")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


# ---------------------------------------------------------------------------
# Operator CLI (client mode)
# ---------------------------------------------------------------------------

def broker_url(args) -> str:
    url = (getattr(args, "broker", "") or
           os.environ.get("DENEB_PUPPET_URL", "")).strip()
    return (url or "http://127.0.0.1:18793").rstrip("/")


def api(args, method: str, path: str, body: dict | None = None) -> dict:
    url = broker_url(args) + path
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=70.0) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        try:
            return json.loads(e.read().decode("utf-8"))
        except (ValueError, OSError):
            return {"error": f"HTTP {e.code}"}
    except (urllib.error.URLError, OSError) as e:
        print(f"broker unreachable at {broker_url(args)}: {e}",
              file=sys.stderr)
        sys.exit(2)


def cmd_pending(args) -> int:
    budget = max(float(args.wait or 0), 0.0)
    while True:
        step = min(budget, PENDING_WAIT_CAP) if budget > 0 else 0
        res = api(args, "GET", f"/puppet/pending?wait={step:g}")
        items = res.get("pending") or []
        budget -= step
        if items or budget <= 0:
            break
    if not items:
        print("(no pending requests)")
        return 1
    for it in items:
        print(f"{it['id']}  +{it['ageSec']}s  {it['kind']}  "
              f"msgs={it['messages']} tools={it['tools']}  "
              f"last={it['lastRole']}: {it['lastPreview'][:120]}")
    return 0


def render_message(i: int, m: dict, limit: int) -> str:
    role = m.get("role", "?")
    lines = [f" [{i}] {role}"]
    if m.get("tool_call_id"):
        lines[0] += f" (tool_call_id={m['tool_call_id']})"
    body = content_preview(m.get("content"), limit)
    if body:
        lines.append(f"     {body}")
    for tc in m.get("tool_calls") or []:
        fn = tc.get("function") or {}
        args = str(fn.get("arguments", ""))
        if len(args) > limit:
            args = args[:limit] + "…"
        lines.append(f"     ↳ tool_call {tc.get('id')}: "
                     f"{fn.get('name')}({args})")
    return "\n".join(lines)


def cmd_show(args) -> int:
    res = api(args, "GET", f"/puppet/request/{args.id}")
    if res.get("error"):
        print(res["error"], file=sys.stderr)
        return 1
    if args.raw:
        print(json.dumps(res.get("request"), ensure_ascii=False, indent=2))
        return 0
    req = res.get("request") or {}
    msgs = req.get("messages") or []
    tools = req.get("tools") or []
    limit = 100000 if args.full else 600
    print(f"== {res['id']}  held {res['ageSec']}s  {res['kind']}  "
          f"model={req.get('model')}  messages={len(msgs)}  "
          f"tools={len(tools)}")
    shown = msgs if args.full else msgs[-8:]
    skipped = len(msgs) - len(shown)
    if skipped:
        print(f" … {skipped} earlier message(s) hidden (--full to show)")
    for i, m in enumerate(shown, start=skipped):
        # System prompt gets a larger budget — it is the point of the seat.
        m_limit = (100000 if args.full else 1500) \
            if m.get("role") == "system" else limit
        print(render_message(i, m, m_limit))
    names = ", ".join(str((t.get("function") or {}).get("name", "?"))
                      for t in tools)
    print(f"-- tools({len(tools)}): {names}")
    print(f"-- reply:  puppet.sh reply {res['id']} --text \"...\"")
    print(f"           puppet.sh reply {res['id']} "
          "--tool NAME '{\"arg\":1}'   (repeatable; --raw for schemas)")
    return 0


def cmd_reply(args) -> int:
    if args.file:
        with open(args.file, encoding="utf-8") as f:
            payload = json.load(f)
    else:
        payload = {}
        if args.text is not None:
            payload["text"] = args.text
        if args.reasoning:
            payload["reasoning"] = args.reasoning
        tool_calls = []
        for name, raw in args.tool or []:
            try:
                parsed = json.loads(raw)
            except ValueError:
                parsed = raw  # pass through malformed args on purpose
            tool_calls.append({"name": name, "arguments": parsed})
        if tool_calls:
            payload["tool_calls"] = tool_calls
        if args.finish:
            payload["finish_reason"] = args.finish
    if not payload:
        print("nothing to send — use --text/--tool/--file", file=sys.stderr)
        return 1
    res = api(args, "POST", f"/puppet/respond/{args.id}", payload)
    if not res.get("ok"):
        print(f"reply failed: {res.get('error')}", file=sys.stderr)
        return 1
    print(f"answered {args.id}")
    return 0


def cmd_fail(args) -> int:
    res = api(args, "POST", f"/puppet/respond/{args.id}",
              {"error": args.message or "aborted by puppet operator"})
    if not res.get("ok"):
        print(f"fail failed: {res.get('error')}", file=sys.stderr)
        return 1
    print(f"failed {args.id} (gateway sees a provider error)")
    return 0


def cmd_history(args) -> int:
    res = api(args, "GET", "/puppet/history")
    items = res.get("history") or []
    if not items:
        print("(empty)")
        return 0
    for it in items[-20:]:
        resp = it.get("response") or {}
        what = ("error: " + str(resp.get("error"))) if resp.get("error") else \
            (f"tools={[t.get('name') for t in resp.get('tool_calls') or []]}"
             if resp.get("tool_calls") else
             f"text={content_preview(resp.get('text'), 80)!r}")
        print(f"{it['id']}  {it['status']:8s} held={it['heldSec']}s  {what}")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n", 1)[0])
    ap.add_argument("--broker", default="",
                    help="broker URL (default: $DENEB_PUPPET_URL)")
    sub = ap.add_subparsers(dest="cmd", required=True)

    sp = sub.add_parser("serve", help="run the broker server")
    sp.add_argument("--host", default="127.0.0.1")
    sp.add_argument("--port", type=int, default=18793)
    sp.add_argument("--model", default="agent-seat")
    sp.add_argument("--journal", default="")
    sp.set_defaults(fn=serve)

    pp = sub.add_parser("pending", help="list/wait for held requests")
    pp.add_argument("--wait", type=float, default=0)
    pp.set_defaults(fn=cmd_pending)

    sh = sub.add_parser("show", help="show one held request")
    sh.add_argument("id")
    sh.add_argument("--full", action="store_true")
    sh.add_argument("--raw", action="store_true",
                    help="dump the full request JSON")
    sh.set_defaults(fn=cmd_show)

    rp = sub.add_parser("reply", help="answer a held request")
    rp.add_argument("id")
    rp.add_argument("--text", default=None)
    rp.add_argument("--reasoning", default="")
    rp.add_argument("--tool", nargs=2, action="append",
                    metavar=("NAME", "ARGS_JSON"))
    rp.add_argument("--finish", default="",
                    help="override finish_reason (stop|tool_calls|length)")
    rp.add_argument("--file", default="",
                    help="full response payload JSON file")
    rp.set_defaults(fn=cmd_reply)

    fl = sub.add_parser("fail", help="abort a held request with an error")
    fl.add_argument("id")
    fl.add_argument("--message", default="")
    fl.set_defaults(fn=cmd_fail)

    hi = sub.add_parser("history", help="recent completed exchanges")
    hi.set_defaults(fn=cmd_history)

    args = ap.parse_args()
    return args.fn(args)


if __name__ == "__main__":
    sys.exit(main())

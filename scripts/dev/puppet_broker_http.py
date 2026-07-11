"""OpenAI-compatible HTTP server for the puppet broker."""
from __future__ import annotations

import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from puppet_broker_state import (
    COMPLETE_HOLD_MAX,
    FINISHED_KEEP,
    KEEPALIVE_INTERVAL,
    SEAT_MODELS,
    STREAM_HOLD_MAX,
    Broker,
    HeldRequest,
    split_delta,
    usage_payload,
)

# ---------------------------------------------------------------------------
# HTTP handler
# ---------------------------------------------------------------------------

BROKER: Broker | None = None


class Handler(BaseHTTPRequestHandler):
    # HTTP/1.1 + chunked framing for the SSE stream. With HTTP/1.0
    # close-delimited bodies, a broker crash mid-hold reads as a CLEAN EOF on
    # the gateway side: sse.go sees no scanner error, openai_stream.go emits
    # message_stop, and the turn completes as a silent empty success. Chunked
    # framing turns the same death into an unexpected-EOF stream error.
    protocol_version = "HTTP/1.1"
    # Bound idle keep-alive reads so pooled gateway connections release their
    # handler threads. Held requests block on an Event, not on socket reads,
    # and are unaffected.
    timeout = 130

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

    def _write_chunk(self, data: bytes) -> bool:
        try:
            self.wfile.write(b"%x\r\n" % len(data) + data + b"\r\n")
            self.wfile.flush()
            return True
        except OSError:
            return False

    def _end_chunks(self) -> None:
        try:
            self.wfile.write(b"0\r\n\r\n")
            self.wfile.flush()
        except OSError:
            pass

    # -- routing ------------------------------------------------------------

    def do_GET(self):  # noqa: N802 (BaseHTTPRequestHandler API)
        path, _, query = self.path.partition("?")
        params = dict(p.split("=", 1) for p in query.split("&") if "=" in p)
        if path in ("/v1/models", "/models"):
            catalog = dict.fromkeys((BROKER.model,) + SEAT_MODELS)
            self._json(200, {"object": "list", "data": [
                {"id": m, "object": "model", "max_model_len": 200000}
                for m in catalog
            ]})
        elif path == "/puppet/health":
            self._json(200, {"status": "ready", **BROKER.state()})
        elif path == "/puppet/state":
            self._json(200, BROKER.state())
        elif path == "/puppet/pending":
            try:
                wait = float(params.get("wait", "0") or 0)
            except ValueError:
                self._json(400, {"error": "invalid wait parameter"})
                return
            items = (BROKER.wait_pending(wait) if wait > 0
                     else BROKER.pending())
            self._json(200, {"pending": items})
        elif path.startswith("/puppet/request/"):
            rid = path.rsplit("/", 1)[-1]
            entry = BROKER.get_any(rid)
            if entry is None:
                self._json(404, {"error": f"unknown request id {rid!r} "
                                 f"(finished ones stay for {FINISHED_KEEP})"})
            else:
                self._json(200, {**entry.summary(),
                                 "status": entry.status,
                                 "response": entry.response,
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
        try:
            if kind == "stream":
                self._hold_stream(entry)
            else:
                self._hold_complete(entry)
        finally:
            # Safety net: if the hold path died before finishing the entry
            # (e.g. an unexpected exception while writing to a socket the
            # gateway already closed), don't leave a ghost in the pending
            # list. No-op when the entry was already finished.
            BROKER.finish(entry, "gone")

    def _hold_stream(self, entry: HeldRequest) -> None:
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        # One stream per connection — don't let keep-alive reuse it.
        self.close_connection = True
        # Open the stream right away; the next write may be 10s out and the
        # gateway should see the response headers immediately.
        if not self._write_chunk(b": seat\n\n"):
            BROKER.finish(entry, "gone")
            return

        deadline = time.time() + STREAM_HOLD_MAX
        while time.time() < deadline:
            if entry.event.wait(timeout=KEEPALIVE_INTERVAL):
                break
            # SSE comment line — ignored by the gateway's parser (sse.go)
            # but keeps bytes flowing under its 10-minute client timeout,
            # and detects a gateway-side cancel (write fails → gone).
            if not self._write_chunk(b": hold\n\n"):
                BROKER.finish(entry, "gone")
                return
        resp = BROKER.final_response(
            entry, f"puppet hold timeout ({int(STREAM_HOLD_MAX)}s)")
        self._emit_sse_response(entry, resp)

    def _emit_sse_response(self, entry: HeldRequest, resp: dict) -> None:
        if resp.get("error"):
            # `event: error` is required — a bare data:{"error":...} parses
            # as an empty chunk and is silently skipped (openai_stream.go).
            payload = json.dumps({"type": "puppet_abort",
                                  "message": str(resp["error"])},
                                 ensure_ascii=False)
            ok = self._write_chunk(f"event: error\ndata: {payload}\n\n"
                                   .encode("utf-8"))
            self._end_chunks()
            BROKER.finish(entry, "failed" if ok else "gone")
            return

        rid = f"chatcmpl-puppet-{entry.id}"
        # Echo the REQUEST's model (per-role seat alias) so gateway-side
        # accounting attributes the exchange to the calling role.
        base = {"id": rid, "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": entry.payload.get("model") or BROKER.model}

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
            for piece in split_delta(str(resp["reasoning"])):
                out.append(chunk({"reasoning_content": piece}))
        if resp.get("text") is not None:
            for piece in split_delta(str(resp.get("text") or "")):
                out.append(chunk({"content": piece}))
        tool_calls = resp.get("tool_calls") or []
        for i, tc in enumerate(tool_calls):
            args = tc.get("arguments", {})
            if not isinstance(args, str):
                args = json.dumps(args, ensure_ascii=False)
            pieces = split_delta(args)
            out.append(chunk({"tool_calls": [{
                "index": i,
                "id": tc.get("id") or f"call_{entry.id}_{i}",
                "type": "function",
                "function": {"name": str(tc.get("name", "")),
                             "arguments": pieces[0]},
            }]}))
            # Long arguments continue as bare fragments keyed by index —
            # exactly how real providers stream them.
            for piece in pieces[1:]:
                out.append(chunk({"tool_calls": [{
                    "index": i, "function": {"arguments": piece}}]}))
        finish = resp.get("finish_reason") or \
            ("tool_calls" if tool_calls else "stop")
        usage = usage_payload(entry.payload.get("messages"), resp)
        out.append(chunk({}, finish=finish, usage=usage))
        out.append(b"data: [DONE]\n\n")

        ok = all(self._write_chunk(b) for b in out)
        self._end_chunks()
        BROKER.finish(entry, "done" if ok else "gone")

    def _hold_complete(self, entry: HeldRequest) -> None:
        # Non-stream callers (e.g. title generation via Complete()) can't
        # receive keepalives; COMPLETE_HOLD_MAX keeps the answer inside the
        # gateway's 5-minute turn deadline.
        entry.event.wait(timeout=COMPLETE_HOLD_MAX)
        resp = BROKER.final_response(
            entry, f"puppet hold timeout ({int(COMPLETE_HOLD_MAX)}s)")
        try:
            if resp.get("error"):
                # 400 = permanent for the gateway's retry logic (no storm).
                self._json(400, {"error": {"message": str(resp["error"]),
                                           "type": "puppet_abort"}})
                BROKER.finish(entry, "failed")
                return
            text = str(resp.get("text") or "")
            self._json(200, {
                "id": f"chatcmpl-puppet-{entry.id}",
                "object": "chat.completion",
                "model": entry.payload.get("model") or BROKER.model,
                "choices": [{"index": 0, "finish_reason": "stop",
                             "message": {"role": "assistant",
                                         "content": text}}],
                "usage": usage_payload(entry.payload.get("messages"), text),
            })
            BROKER.finish(entry, "done")
        except OSError:
            # The gateway hung up (turn deadline) before we answered.
            BROKER.finish(entry, "gone")


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

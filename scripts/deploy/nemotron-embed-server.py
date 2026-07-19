#!/usr/bin/env python3
"""Nemotron-3-Embed adapter — Deneb embedding contract over the vLLM container.

Topology (2026-07-18 NVFP4 cutover):

    gateway (embedding.Client) ──/embed {texts,kind}──▶ THIS adapter :8002
                                                           │ prefixes "query: "/"passage: "
                                                           ▼
    eugr spark-vllm container (NVFP4, prebuilt sm_121a) ── /v1/embeddings :8003

Why an adapter at all: the gateway speaks the BGE-era /health + /embed contract
(and its semantic caches key on /health model:dimensions), while the NVFP4
checkpoint is vLLM-only and speaks OpenAI /v1/embeddings. The adapter is
stdlib-only (no venv — pip vLLM JIT-compiles fp4 kernels on GB10 and trips
earlyoom; the eugr image ships prebuilt wheels, so the model lives in the
container and this file stays a dumb translator).

kind: "query" → "query: " prefix (asymmetric retrieval role); anything else →
"passage: " — bulk indexing is the default caller and must never get query
framing.
"""

import argparse
import json
import sys
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODEL_ID = "nvidia/Nemotron-3-Embed-1B-NVFP4"
MODEL_LABEL = "Nemotron-3-Embed-1B-NVFP4"

# Upper bound for server-side truncation; the effective value is clamped to the
# backend's live --max-model-len (probed via /v1/models), because vLLM 400s the
# whole batch when truncate_prompt_tokens exceeds it. Trusting unit files to be
# restarted in lockstep is what broke overnight 2026-07-18→19: the adapter
# shipped 8192 while the old backend (smaller limit) kept running, and every
# 5-minute warm loop failed until the backend restart.
TRUNCATE_CAP = 8192


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8002)
    parser.add_argument("--backend", default="http://127.0.0.1:8003", help="vLLM OpenAI server base URL")
    parser.add_argument("--timeout", type=float, default=120.0)
    return parser.parse_args()


class Adapter(BaseHTTPRequestHandler):
    args: argparse.Namespace
    dims_lock = threading.Lock()
    dims = 0  # probed lazily from the first backend response
    trunc_lock = threading.Lock()
    trunc_tokens = 0  # probed lazily from the backend's max_model_len

    def log_message(self, fmt, *a):  # quiet: health polls every 30s
        pass

    def _json(self, code: int, payload: dict) -> None:
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _truncate_tokens(self) -> int:
        # Probe the backend's live max_model_len and clamp; cache the answer.
        # A probe failure falls back to the cap without caching, so a backend
        # that is briefly down doesn't pin a guess.
        with self.trunc_lock:
            if Adapter.trunc_tokens > 0:
                return Adapter.trunc_tokens
        try:
            req = urllib.request.Request(self.args.backend.rstrip("/") + "/v1/models")
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.load(resp)
            limits = [int(m["max_model_len"]) for m in data.get("data") or [] if m.get("max_model_len")]
            value = min([TRUNCATE_CAP] + limits)
        except Exception:  # noqa: BLE001 — degrade to the cap, stay serving
            return TRUNCATE_CAP
        with self.trunc_lock:
            Adapter.trunc_tokens = value
        return value

    def _backend_embed(self, texts: list[str]) -> list[list[float]]:
        # truncate_prompt_tokens: vLLM 400s on inputs beyond --max-model-len
        # instead of truncating like the BGE server did — diary warm batches
        # carry entries that long (observed 2026-07-18). Server-side truncation
        # restores the old lossy-but-never-failing contract. The value is
        # clamped to the backend's live limit (see _truncate_tokens); on a
        # limit-mismatch 400 (backend restarted with a smaller --max-model-len
        # under us) the cached limit is dropped and the batch retried once.
        for attempt in (0, 1):
            body = json.dumps({
                "model": MODEL_ID,
                "input": texts,
                "truncate_prompt_tokens": self._truncate_tokens(),
            }).encode()
            req = urllib.request.Request(
                self.args.backend.rstrip("/") + "/v1/embeddings",
                data=body,
                headers={"Content-Type": "application/json"},
            )
            try:
                with urllib.request.urlopen(req, timeout=self.args.timeout) as resp:
                    data = json.load(resp)
            except urllib.error.HTTPError as exc:
                detail = exc.read().decode(errors="replace")[:500]
                if attempt == 0 and exc.code == 400 and "truncate_prompt_tokens" in detail:
                    with self.trunc_lock:
                        Adapter.trunc_tokens = 0
                    continue
                raise RuntimeError(f"backend {exc.code}: {detail}") from exc
            rows = sorted(data["data"], key=lambda e: e["index"])
            return [row["embedding"] for row in rows]
        raise RuntimeError("unreachable")  # loop always returns or raises

    def do_GET(self):
        if self.path != "/health":
            self._json(404, {"error": "not found"})
            return
        # The gateway fingerprint (model:dimensions) comes from here — only
        # report ok once the backend actually answers, and learn dims from a
        # one-token probe so the fingerprint is truthful, not hardcoded.
        try:
            with self.dims_lock:
                if Adapter.dims <= 0:
                    Adapter.dims = len(self._backend_embed(["passage: ping"])[0])
            self._json(200, {"status": "ok", "model": MODEL_LABEL, "dimensions": Adapter.dims})
        except Exception as exc:  # noqa: BLE001 — health must degrade, not crash
            self._json(503, {"status": "backend unavailable", "error": str(exc)[:200]})

    def do_POST(self):
        if self.path != "/embed":
            self._json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            req = json.loads(self.rfile.read(length))
            texts = req.get("texts") or []
            kind = req.get("kind") or ""
            prefix = "query: " if kind == "query" else "passage: "
            vecs = self._backend_embed([prefix + t for t in texts])
            if vecs:
                with self.dims_lock:
                    Adapter.dims = len(vecs[0])
            self._json(200, {
                "embeddings": vecs,
                "model": MODEL_LABEL,
                "count": len(vecs),
                "dimensions": Adapter.dims,
            })
        except Exception as exc:  # noqa: BLE001 — surface as HTTP 502, keep serving
            self._json(502, {"error": str(exc)[:300]})


def main() -> None:
    args = parse_args()
    Adapter.args = args
    server = ThreadingHTTPServer((args.host, args.port), Adapter)
    print(f"nemotron adapter on {args.host}:{args.port} -> {args.backend}", file=sys.stderr, flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()

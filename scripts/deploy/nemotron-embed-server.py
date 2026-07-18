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
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODEL_ID = "nvidia/Nemotron-3-Embed-1B-NVFP4"
MODEL_LABEL = "Nemotron-3-Embed-1B-NVFP4"


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

    def log_message(self, fmt, *a):  # quiet: health polls every 30s
        pass

    def _json(self, code: int, payload: dict) -> None:
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _backend_embed(self, texts: list[str]) -> list[list[float]]:
        # truncate_prompt_tokens: vLLM 400s on inputs beyond --max-model-len
        # (4096) instead of truncating like the BGE server did — diary warm
        # batches carry entries that long (observed 2026-07-18). Server-side
        # truncation restores the old lossy-but-never-failing contract.
        body = json.dumps({
            "model": MODEL_ID,
            "input": texts,
            "truncate_prompt_tokens": 4096,
        }).encode()
        req = urllib.request.Request(
            self.args.backend.rstrip("/") + "/v1/embeddings",
            data=body,
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(req, timeout=self.args.timeout) as resp:
            data = json.load(resp)
        rows = sorted(data["data"], key=lambda e: e["index"])
        return [row["embedding"] for row in rows]

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

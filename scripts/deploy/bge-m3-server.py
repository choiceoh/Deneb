#!/usr/bin/env python3
"""BGE-M3 embedding server for Deneb recall + compaction.

Lightweight FastAPI server wrapping BGE-M3 GGUF (Q5_K_M) via llama-cpp-python.
Runs on the DGX Spark GB10 GPU (n_gpu_layers=99, sm_121) via a CUDA-built
llama-cpp wheel — see scripts/deploy/bge-m3-build-cuda.sh for the reproducible
build. The Go gateway (internal/ai/embedding, default :8001) uses it for wiki/
file semantic recall and MMR extractive compaction; it degrades to BM25 if down.

Usage:
    python3 scripts/deploy/bge-m3-server.py [--port 8001] [--gpu-layers 99]
"""

import argparse
import ctypes
import logging
import os
import queue
import signal
import sys
import time
from contextlib import asynccontextmanager

import numpy as np
import uvicorn
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

# ggml_log_level: keep ERROR(4)+, drop INFO(2)/WARN(3). The CUDA backend emits a
# WARN ("init: embeddings required but some input tokens were not marked as
# outputs -> overriding") on EVERY embed — benign (the MEAN-pooling path marks
# all tokens as outputs), but at ~4 lines/embed × hundreds of embeds/hour it
# floods the user journal, the same retention pressure that forced the /health
# access-log filter below. verbose=False only silences model load, not per-embed
# decode, so we filter llama.cpp's C-level log directly. llama-cpp-python 0.3.16
# doesn't export the GGML_LOG_LEVEL_* constants, so the value is inlined.
_GGML_LOG_LEVEL_ERROR = 4

# Module-level ref so the ctypes callback trampoline isn't garbage-collected
# (a freed callback would segfault llama.cpp on its next log).
_llama_log_cb = None


def _silence_llama_logs() -> None:
    """Route llama.cpp's C-level log through a level filter: ERROR+ to stderr,
    everything below dropped. No-op if the llama_log API is unavailable."""
    global _llama_log_cb
    try:
        from llama_cpp import llama_log_callback, llama_log_set
    except Exception:  # pragma: no cover - older/newer wheel without the symbol
        return

    @llama_log_callback
    def _cb(level, text, _user_data):
        if level >= _GGML_LOG_LEVEL_ERROR:
            try:
                sys.stderr.buffer.write(text if isinstance(text, bytes) else str(text).encode())
                sys.stderr.buffer.flush()
            except Exception:
                pass

    _llama_log_cb = _cb  # keep alive
    llama_log_set(_cb, ctypes.c_void_p(0))

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger("bge-m3")

# ---------------------------------------------------------------------------
# Model context pool
# ---------------------------------------------------------------------------

# llama-cpp-python's Llama context is NOT thread-safe: the /embed handler is a
# sync def, so FastAPI runs it in a threadpool and concurrent requests used to call
# embed() on the SAME context simultaneously — which segfaulted the server (SIGSEGV,
# ~5 restarts/day) and gave the gateway "connection refused" during each restart.
#
# Instead of serializing onto one context (which would kill parallelism), we keep a
# POOL of independent contexts. Each request checks one out, so it never shares a
# context with another request — that's the only thing llama.cpp forbids — while N
# requests still embed in parallel on N contexts (real concurrency on the box's many
# cores). Weights are mmap-shared across contexts, so the pool's extra RAM is just
# per-context compute buffers, not N full copies.
_pool: "queue.Queue" = queue.Queue()
_pool_size = 0
_model_path = os.path.expanduser("~/.deneb/models/bge-m3-gguf/bge-m3-Q5_K_M.gguf")
_embedding_dim = 1024  # BGE-M3 output dimension


def load_model(n_gpu_layers: int = 99, pool_size: int = 4):
    """Load `pool_size` independent BGE-M3 GGUF contexts into the pool."""
    global _pool_size

    from llama_cpp import Llama

    _silence_llama_logs()

    if not os.path.exists(_model_path):
        logger.error("model not found: %s", _model_path)
        logger.error("download: huggingface-cli download gpustack/bge-m3-GGUF bge-m3-Q5_K_M.gguf --local-dir ~/.deneb/models/bge-m3-gguf")
        sys.exit(1)

    logger.info("loading %d contexts of %s (n_gpu_layers=%d)...", pool_size, _model_path, n_gpu_layers)
    start = time.monotonic()
    for i in range(pool_size):
        model = Llama(
            model_path=_model_path,
            n_gpu_layers=n_gpu_layers,
            n_ctx=8192,  # BGE-M3 max context
            embedding=True,
            verbose=False,
            pooling_type=1,  # LLAMA_POOLING_TYPE_MEAN for sentence embeddings
        )
        _pool.put(model)
        logger.info("  context %d/%d ready", i + 1, pool_size)
    _pool_size = pool_size
    elapsed = time.monotonic() - start
    logger.info("%d contexts loaded in %.1fs (Q5_K_M, %.0f MB on disk)", pool_size, elapsed, os.path.getsize(_model_path) / 1024 / 1024)


# ---------------------------------------------------------------------------
# FastAPI app
# ---------------------------------------------------------------------------


@asynccontextmanager
async def lifespan(app: FastAPI):
    yield
    logger.info("shutting down")


app = FastAPI(title="BGE-M3 Embedding Server", lifespan=lifespan)


class EmbedRequest(BaseModel):
    texts: list[str] = Field(..., min_length=1, max_length=256)


class EmbedResponse(BaseModel):
    embeddings: list[list[float]]
    dimensions: int
    count: int


@app.get("/health")
async def health():
    if _pool_size == 0:
        raise HTTPException(503, "model not loaded")
    return {"status": "ok", "model": "bge-m3-Q5_K_M", "dimensions": _embedding_dim, "pool": _pool_size}


@app.post("/embed", response_model=EmbedResponse)
def embed(req: EmbedRequest):
    """Sync handler — FastAPI runs it in a threadpool, so up to pool_size requests
    embed in parallel, each on its own checked-out context."""
    if _pool_size == 0:
        raise HTTPException(503, "model not loaded")

    # Check out one context for the whole batch; another request gets a different
    # context (real parallelism) but never shares this one (which would segfault).
    model = _pool.get()
    try:
        start = time.monotonic()
        embeddings = []
        for text in req.texts:
            emb = model.embed(text)
            # llama-cpp-python embed() returns list[float] or list[list[float]]
            if isinstance(emb[0], list):
                embeddings.append(emb[0])
            else:
                embeddings.append(emb)

        elapsed_ms = (time.monotonic() - start) * 1000
        if elapsed_ms > 1000:
            logger.info("embed %d texts in %.0fms", len(req.texts), elapsed_ms)

        return EmbedResponse(
            embeddings=embeddings,
            dimensions=len(embeddings[0]),
            count=len(embeddings),
        )
    except Exception as e:
        logger.exception("embedding failed")
        raise HTTPException(500, str(e))
    finally:
        _pool.put(model)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


class _HealthAccessFilter(logging.Filter):
    """Drop uvicorn access-log lines for GET /health.

    The gateway polls /health every minute; those lines were ~90% of this
    unit's journal volume (3,783 of 4,176 lines over 3 prod days) and helped
    push the user journal past its retention cap. Embeds and errors still log.
    """

    def filter(self, record: logging.LogRecord) -> bool:
        return "/health" not in record.getMessage()


def main():
    parser = argparse.ArgumentParser(description="BGE-M3 embedding server (Q5_K_M GGUF)")
    parser.add_argument("--port", type=int, default=8001)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--gpu-layers", type=int, default=99, help="layers to offload to GPU (99=all)")
    parser.add_argument("--pool-size", type=int, default=int(os.getenv("BGE_M3_POOL_SIZE", "4")),
                        help="independent contexts for parallel embedding (RAM ≈ mmap-shared weights + per-context buffers)")
    args = parser.parse_args()

    load_model(args.gpu_layers, max(1, args.pool_size))

    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))

    logging.getLogger("uvicorn.access").addFilter(_HealthAccessFilter())
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()

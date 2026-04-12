#!/usr/bin/env python3
"""BGE-M3 embedding server for Deneb compaction fallback.

Lightweight FastAPI server wrapping BGE-M3 GGUF (Q5_K_M) via llama-cpp-python.
Quantized for speed on ARM (DGX Spark GB10). Used by the Go gateway for
MMR-based extractive compaction when LLM summarization is unavailable.

Usage:
    python3 scripts/deploy/bge-m3-server.py [--port 8001] [--gpu-layers 99]
"""

import argparse
import logging
import os
import signal
import sys
import time
from contextlib import asynccontextmanager

import numpy as np
import uvicorn
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger("bge-m3")

# ---------------------------------------------------------------------------
# Model singleton
# ---------------------------------------------------------------------------

_model = None
_model_path = os.path.expanduser("~/.deneb/models/bge-m3-gguf/bge-m3-Q5_K_M.gguf")
_embedding_dim = 1024  # BGE-M3 output dimension


def load_model(n_gpu_layers: int = 99):
    """Load BGE-M3 GGUF model via llama-cpp-python."""
    global _model

    from llama_cpp import Llama

    if not os.path.exists(_model_path):
        logger.error("model not found: %s", _model_path)
        logger.error("download: huggingface-cli download gpustack/bge-m3-GGUF bge-m3-Q5_K_M.gguf --local-dir ~/.deneb/models/bge-m3-gguf")
        sys.exit(1)

    logger.info("loading %s (n_gpu_layers=%d)...", _model_path, n_gpu_layers)
    start = time.monotonic()
    # Suppress "embeddings required but some input tokens were not marked as
    # outputs" warnings from llama.cpp by redirecting stderr during init.
    _model = Llama(
        model_path=_model_path,
        n_gpu_layers=n_gpu_layers,
        n_ctx=8192,  # BGE-M3 max context
        embedding=True,
        verbose=False,
        pooling_type=1,  # LLAMA_POOLING_TYPE_MEAN for sentence embeddings
    )
    elapsed = time.monotonic() - start
    logger.info("model loaded in %.1fs (Q5_K_M, %.0f MB)", elapsed, os.path.getsize(_model_path) / 1024 / 1024)


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
    if _model is None:
        raise HTTPException(503, "model not loaded")
    return {"status": "ok", "model": "bge-m3-Q5_K_M", "dimensions": _embedding_dim}


@app.post("/embed", response_model=EmbedResponse)
def embed(req: EmbedRequest):
    """Sync handler — FastAPI runs it in a threadpool, keeping the event loop free."""
    if _model is None:
        raise HTTPException(503, "model not loaded")

    try:
        start = time.monotonic()
        embeddings = []
        for text in req.texts:
            emb = _model.embed(text)
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


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(description="BGE-M3 embedding server (Q5_K_M GGUF)")
    parser.add_argument("--port", type=int, default=8001)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--gpu-layers", type=int, default=99, help="layers to offload to GPU (99=all)")
    args = parser.parse_args()

    load_model(args.gpu_layers)

    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))

    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()

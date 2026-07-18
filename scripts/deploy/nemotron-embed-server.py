#!/usr/bin/env python3
"""Nemotron-3-Embed embedding sidecar (Deneb recall/compaction semantic index).

Successor to bge-m3-server.py after the 2026-07-18 hard-goldset A/B (semantic
p@1 BGE 70.4% vs Nemotron 88.9%). Same /health + /embed HTTP contract the
gateway's embedding.Client speaks, with one addition: the request may carry
`kind` ("query" | "passage" | ""), because Nemotron is an asymmetric model
trained with distinct role prefixes. The gateway's search paths send
kind=query; indexing/refresh sends the default, which maps to passage.

GPU (GB10) via sentence-transformers/torch. dim=2048 — the gateway's semantic
caches key on the /health model:dimensions fingerprint, so pointing
DENEB_EMBEDDING_URL here re-embeds the wiki/diary automatically, and pointing
back to BGE restores the old cache untouched (rollback lever).
"""

import argparse
import os
import sys

import numpy as np
import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel
from sentence_transformers import SentenceTransformer

DEFAULT_MODEL = "nvidia/Nemotron-3-Embed-1B-BF16"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8002)
    parser.add_argument("--model", default=os.environ.get("NEMOTRON_MODEL", DEFAULT_MODEL))
    parser.add_argument("--batch-size", type=int, default=32)
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    print(f"loading {args.model} ...", file=sys.stderr, flush=True)
    model = SentenceTransformer(args.model, trust_remote_code=True, device="cuda")
    dim = model.get_sentence_embedding_dimension()
    print(f"loaded: dim={dim}", file=sys.stderr, flush=True)

    app = FastAPI()

    class EmbedRequest(BaseModel):
        texts: list[str]
        # Retrieval role for the asymmetric prefixes. Unknown/empty → passage:
        # bulk indexing is the default caller and must never get query framing.
        kind: str = ""

    @app.get("/health")
    def health():  # matches the gateway probe contract (model + dimensions)
        return {"status": "ok", "model": os.path.basename(args.model), "dimensions": dim}

    @app.post("/embed")
    def embed(req: EmbedRequest):
        prefix = "query: " if req.kind == "query" else "passage: "
        texts = [prefix + t for t in req.texts]
        vecs = model.encode(
            texts,
            normalize_embeddings=True,
            convert_to_numpy=True,
            batch_size=args.batch_size,
        )
        arr = np.asarray(vecs, dtype=np.float32)
        return {
            "embeddings": arr.tolist(),
            "model": os.path.basename(args.model),
            "count": len(texts),
            "dimensions": dim,
        }

    uvicorn.run(app, host=args.host, port=args.port, log_level="warning")


if __name__ == "__main__":
    main()

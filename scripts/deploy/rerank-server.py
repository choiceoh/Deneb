#!/usr/bin/env python3
"""Deneb rerank sidecar — cross-encoder /rerank on GB10 GPU (port 8004).

Serves the gateway's rerank client contract (internal/ai/rerank/client.go):

    POST /rerank {"query": str, "documents": [str, ...]}  →  {"scores": [float, ...]}
    GET  /health                                          →  {"status": "ok", "model": ...}

Model (--model):
  xprovence  naver/xprovence-reranker-bgem3-v2 — default. Chosen 2026-07-19 on
             the 177-case merged wiki-qa gold: P@1 83.6→87.0 (+3.4pp, no CV-fold
             regression) vs bge-reranker's 86.4 (+2.8pp, one fold regressed).
             CC BY-NC license (operator-accepted for this single-user personal
             deployment); also returns pruned contexts (unused here — score-only).
  bge        BAAI/bge-reranker-v2-m3 — Apache-2.0 fallback, same backbone/latency.

Runs from the ~/nemotron-eval/venv (torch 2.13 cu130 + transformers 4.51 +
spacy/xx_sent_ud_sm — xprovence's remote code needs transformers<5). fp16 on
CUDA; ~570M params ≈ 1.2GB VRAM; 10-doc query ≈ 140ms warm. A process-wide lock
serializes GPU calls (the gateway sends one small batch per search; concurrency
here would only fragment VRAM).
"""

import argparse
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_DOCS = 64
MAX_DOC_CHARS = 4000

_lock = threading.Lock()
_model = None
_model_name = ""
_scorer = None  # (query, [docs]) -> [float]


def load_model(kind: str):
    global _model, _model_name, _scorer
    import torch

    if kind == "xprovence":
        from transformers.dynamic_module_utils import get_class_from_dynamic_module

        name = "naver/xprovence-reranker-bgem3-v2"
        cls = get_class_from_dynamic_module("modeling_xprovence_hf.XProvence", name)
        model = cls.from_pretrained(name, torch_dtype=torch.float16).cuda().eval()

        def score(query, docs):
            # Provence batch shape: queries[i] ↔ contexts[i] where contexts[i]
            # is that query's LIST of documents. One query here → wrap both.
            out = model.process([query], [docs])
            scores = out["reranking_score"]
            if isinstance(scores, list) and scores and isinstance(scores[0], list):
                scores = scores[0]
            if not isinstance(scores, list):
                scores = [scores]
            # A doc whose every sentence got pruned scores None — that IS the
            # model saying "irrelevant", so map it to a strong negative.
            return [float(s) if s is not None else -10.0 for s in scores]

    else:  # bge
        from transformers import AutoModelForSequenceClassification, AutoTokenizer

        name = "BAAI/bge-reranker-v2-m3"
        tok = AutoTokenizer.from_pretrained(name)
        model = AutoModelForSequenceClassification.from_pretrained(name, torch_dtype=torch.float16).cuda().eval()

        def score(query, docs):
            with torch.no_grad():
                enc = tok([query] * len(docs), docs, padding=True, truncation=True, max_length=512, return_tensors="pt").to("cuda")
                return model(**enc).logits.view(-1).float().cpu().tolist()

    _model, _model_name, _scorer = model, name, score


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # journal stays quiet on per-request noise
        pass

    def _json(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._json(200, {"status": "ok", "model": _model_name})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path.rstrip("/") != "/rerank":
            self._json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            req = json.loads(self.rfile.read(length) or b"{}")
            query = str(req.get("query", "")).strip()
            docs = req.get("documents") or []
            if not query or not isinstance(docs, list) or not docs:
                self._json(400, {"error": "query and documents required"})
                return
            if len(docs) > MAX_DOCS:
                self._json(400, {"error": f"too many documents (max {MAX_DOCS})"})
                return
            docs = [str(d)[:MAX_DOC_CHARS] for d in docs]
            with _lock:
                scores = _scorer(query, docs)
            if len(scores) != len(docs):
                self._json(500, {"error": "score count mismatch"})
                return
            self._json(200, {"scores": scores})
        except Exception as e:  # noqa: BLE001 — sidecar must never crash the loop
            self._json(500, {"error": str(e)})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=8004)
    ap.add_argument("--model", choices=["xprovence", "bge"], default="xprovence")
    args = ap.parse_args()
    load_model(args.model)
    # Warm once so the first live query doesn't pay CUDA graph/alloc setup.
    _scorer("당진 프로젝트 계약 금액이 얼마지?", [
        "프로젝트/당진/계약\n당진 태양광 프로젝트 EPC 계약 금액 협상 기록. 총액과 회신 이력.",
        "업무/주간회의\n주간 회의록. 일정 점검과 부서별 보고.",
    ])
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    print(f"rerank-server ready model={_model_name} port={args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()

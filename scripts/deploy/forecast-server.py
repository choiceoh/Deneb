#!/usr/bin/env python3
"""Deneb forecast sidecar — Chronos-2 time-series forecasting on GB10 (port 8005).

Serves the gateway's forecast client contract (internal/ai/forecast/client.go):

    POST /forecast {"series": [...] | [[...], ...], "horizon": int,
                    "quantiles": [float, ...],
                    "past_covariates": {name: [...]}?,      # single series only
                    "future_covariates": {name: [...]}?}    # single series only
        → {"model", "horizon", "quantile_levels", "series": [{"quantiles": [[...]], "mean": [...]}],
           "elapsed_ms"}
    GET  /health → {"status": "ok", "model": ..., "device": ...}

Model: amazon/chronos-2 (~120M, Apache-2.0). Zero-shot — no per-series training,
no fitting state, so a forecast is one forward pass over whatever numbers the
agent already has in hand. Native support for missing values (send null), for
multiple series in one batch (shared forward pass), and for covariates — the
capability Chronos-1 lacked and the reason this is the version worth serving.

Runs from ~/venvs/chronos (torch 2.12 cu130 + chronos-forecasting 2.3.1 +
transformers 5.16). ~0.5GB weights; a 120-point context / 14-step horizon is
~45ms warm on this host. A process-wide lock serializes GPU calls — the
gateway sends one small batch per ask, and concurrency here would only
fragment VRAM the 4-node TP serving is already holding.

Model ceilings (config.json): context 8192 points, prediction
max_output_patches(64) * output_patch_size(16) = 1024 steps. The caps below sit
under both and keep one response small enough to paste into a chat turn.
"""

import argparse
import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_SERIES = 32
MAX_CONTEXT = 4096
MAX_HORIZON = 256
MIN_CONTEXT = 4
DEFAULT_QUANTILES = [0.1, 0.5, 0.9]

_lock = threading.Lock()
_pipe = None
_model_name = ""
_device = ""


def load_model(name: str, device: str):
    global _pipe, _model_name, _device
    from chronos import BaseChronosPipeline

    _pipe = BaseChronosPipeline.from_pretrained(name, device_map=device)
    _model_name, _device = name, device


def _floats(raw, field: str, *, allow_null: bool):
    """Coerce a JSON array to floats. null/None becomes NaN when allowed —
    Chronos-2 imputes missing points natively, so a gappy business series
    (a closed month, a skipped reading) does not have to be pre-filled by
    the caller with a fabricated number."""
    import math

    if not isinstance(raw, list) or not raw:
        raise ValueError(f"{field}: expected a non-empty array of numbers")
    out = []
    for v in raw:
        if v is None:
            if not allow_null:
                raise ValueError(f"{field}: null is not allowed here")
            out.append(math.nan)
            continue
        if isinstance(v, bool) or not isinstance(v, (int, float)):
            raise ValueError(f"{field}: {v!r} is not a number")
        out.append(float(v))
    return out


def _parse_series(raw):
    """Accept one series ([1,2,3]) or many ([[1,2],[3,4]]) — a single ask about
    one number and a per-customer sweep are the same forward pass."""
    if not isinstance(raw, list) or not raw:
        raise ValueError("series: expected an array of numbers, or an array of such arrays")
    many = isinstance(raw[0], list)
    batch = raw if many else [raw]
    if len(batch) > MAX_SERIES:
        raise ValueError(f"series: too many series (max {MAX_SERIES})")
    parsed = []
    for i, s in enumerate(batch):
        vals = _floats(s, f"series[{i}]", allow_null=True)
        if len(vals) < MIN_CONTEXT:
            raise ValueError(f"series[{i}]: need at least {MIN_CONTEXT} points, got {len(vals)}")
        parsed.append(vals[-MAX_CONTEXT:])  # keep the most recent window
    return parsed, many


def _parse_covariates(raw, field: str, expect_len: int):
    if raw is None:
        return None
    if not isinstance(raw, dict) or not raw:
        raise ValueError(f"{field}: expected an object of name -> array")
    out = {}
    for name, values in raw.items():
        vals = _floats(values, f"{field}.{name}", allow_null=True)
        if len(vals) != expect_len:
            raise ValueError(f"{field}.{name}: expected {expect_len} values, got {len(vals)}")
        out[str(name)] = vals
    return out


def forecast(series, horizon, quantile_levels, past_cov, future_cov):
    import numpy as np

    if past_cov or future_cov:
        # Covariate form: one dict per series. Only the single-series case is
        # served — the batch form requires every series to carry the same
        # covariate keys, which no caller has needed yet.
        item = {"target": np.asarray(series[0], dtype=np.float32)}
        if past_cov:
            item["past_covariates"] = {k: np.asarray(v, dtype=np.float32) for k, v in past_cov.items()}
        if future_cov:
            item["future_covariates"] = {k: np.asarray(v, dtype=np.float32) for k, v in future_cov.items()}
        inputs = [item]
    else:
        inputs = [np.asarray(s, dtype=np.float32) for s in series]

    quantiles, mean = _pipe.predict_quantiles(
        inputs, prediction_length=horizon, quantile_levels=quantile_levels
    )
    out = []
    for q, m in zip(quantiles, mean):
        # Both come back shaped (n_targets, horizon, ...); every input here is
        # univariate, so target 0 is the answer.
        qa = np.asarray(q, dtype=np.float64)[0]
        ma = np.asarray(m, dtype=np.float64)[0]
        out.append({
            "quantiles": [[round(v, 6) for v in step] for step in qa.tolist()],
            "mean": [round(v, 6) for v in np.ravel(ma).tolist()],
        })
    return out


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
            self._json(200, {"status": "ok", "model": _model_name, "device": _device})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path.rstrip("/") != "/forecast":
            self._json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            req = json.loads(self.rfile.read(length) or b"{}")

            series, _ = _parse_series(req.get("series"))
            horizon = int(req.get("horizon") or 12)
            if not 1 <= horizon <= MAX_HORIZON:
                raise ValueError(f"horizon: must be 1..{MAX_HORIZON}")

            levels = req.get("quantiles") or DEFAULT_QUANTILES
            levels = sorted({round(float(q), 4) for q in _floats(levels, "quantiles", allow_null=False)})
            if any(not 0.0 < q < 1.0 for q in levels) or len(levels) > 9:
                raise ValueError("quantiles: up to 9 levels, each strictly between 0 and 1")

            past_cov = _parse_covariates(req.get("past_covariates"), "past_covariates", len(series[0]))
            future_cov = _parse_covariates(req.get("future_covariates"), "future_covariates", horizon)
            if (past_cov or future_cov) and len(series) > 1:
                raise ValueError("covariates are supported for a single series only")
        except (ValueError, TypeError) as e:
            self._json(400, {"error": str(e)})
            return

        try:
            t0 = time.time()
            with _lock:
                out = forecast(series, horizon, levels, past_cov, future_cov)
            self._json(200, {
                "model": _model_name,
                "horizon": horizon,
                "quantile_levels": levels,
                "series": out,
                "elapsed_ms": int((time.time() - t0) * 1000),
            })
        except Exception as e:  # noqa: BLE001 — sidecar must never crash the loop
            self._json(500, {"error": str(e)})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=8005)
    ap.add_argument("--model", default="amazon/chronos-2")
    ap.add_argument("--device", default="cuda")
    args = ap.parse_args()
    load_model(args.model, args.device)
    # Warm once so the first live ask does not pay CUDA graph/alloc setup.
    forecast([[float(i % 7) + 10.0 for i in range(64)]], 8, DEFAULT_QUANTILES, None, None)
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    print(f"forecast-server ready model={_model_name} device={_device} port={args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()

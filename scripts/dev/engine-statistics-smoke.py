#!/usr/bin/env python3
"""Exercise engine scrape -> history -> authenticated RPC using synthetic counters.

Runs an isolated dev gateway and loopback fixture, then stops both. This is a
functional contract check, never a measurement of model performance.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[2]


def fixture_metrics(tick: int) -> str:
    values = {
        'st:runtime_info{build="synthetic",boot="fixture",k="7",precision="w4"}': 1,
        "vllm:num_requests_running": 1,
        "vllm:time_to_first_token_seconds_sum": tick,
        "vllm:time_to_first_token_seconds_count": tick,
        "vllm:time_per_output_token_seconds_count": tick * 60,
        "vllm:time_per_output_token_seconds_sum": tick,
        'st:step_seconds_count{kind="decode"}': tick * 20,
        'st:step_seconds_sum{kind="decode"}': tick,
        "vllm:generation_tokens_total": tick * 60,
        "vllm:spec_decode_num_draft_tokens_total": tick * 140,
        "vllm:spec_decode_num_accepted_tokens_total": tick * 40,
        "st:prefill_computed_tokens_total": tick * 1000,
        "st:prefill_compute_seconds_total": tick,
        'st:decode_stage_seconds_total{stage="propose"}': tick * 0.1,
        "st:decode_stage_samples_total": tick,
        'st:spec_accepted_per_step_total{accepted="2"}': tick * 20,
        'st:response_tokens_sum{part="reasoning"}': tick * 1024,
        'st:response_tokens_count{part="reasoning"}': tick,
        'st:response_finished_total{reason="length"}': tick,
    }
    labels = 'cache="cold",context="8192",sequences="1",timing="sync_wall"'
    for name, amount in {
        "steps": 20,
        "seconds": 1,
        "tokens": 60,
        "drafted": 140,
        "accepted": 40,
    }.items():
        values[f"st:condition_{name}_total{{{labels}}}"] = tick * amount
    for name in (
        "time_to_first_token_seconds",
        "request_queue_time_seconds",
        "e2e_request_latency_seconds",
    ):
        for upper, count in (("0.1", 0), ("1", tick), ("+Inf", tick)):
            values[f'vllm:{name}_bucket{{le="{upper}"}}'] = count
    return "".join(f"{name} {value}\n" for name, value in values.items())


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--hold-seconds",
        type=int,
        default=0,
        help="Keep the verified fixture alive for native UI inspection (0..600 seconds)",
    )
    args = parser.parse_args()
    if not 0 <= args.hold_seconds <= 600:
        parser.error("--hold-seconds must be 0..600")
    started = time.monotonic()

    class Fixture(BaseHTTPRequestHandler):
        def do_GET(self):
            body = (
                fixture_metrics(int(time.monotonic() - started) + 1)
                if self.path == "/metrics"
                else json.dumps({"data": [{"id": "synthetic-engine"}]})
            )
            self.send_response(200)
            self.end_headers()
            self.wfile.write(body.encode())

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    instance = f"engine-stats-smoke-{os.getpid()}"
    env = {
        **os.environ,
        "DENEB_INSTANCE": instance,
        "DENEB_ENGINE_METRICS_URL": f"http://127.0.0.1:{server.server_port}/metrics",
    }
    command = [str(ROOT / "scripts/dev/live-test.sh")]
    try:
        subprocess.run([*command, "start"], cwd=ROOT, env=env, check=True)
        token = Path(f"/tmp/deneb-{instance}-dev-state/client_token").read_text().strip()
        # Resolve the same isolated port through the launcher library.
        port = subprocess.check_output(
            ["bash", "-c", 'source scripts/dev/lib-server.sh; echo "$DEVLIB_LIVE_PORT"'],
            cwd=ROOT,
            env=env,
            text=True,
        ).strip()
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            request = Request(
                f"http://127.0.0.1:{port}/api/v1/miniapp/rpc",
                data=json.dumps(
                    {
                        "type": "req",
                        "id": "engine-smoke",
                        "method": "miniapp.engine.status",
                        "params": {},
                    }
                ).encode(),
                headers={"Content-Type": "application/json", "X-Deneb-Client-Token": token},
            )
            with urlopen(request, timeout=20) as response:
                payload = json.load(response)["payload"]
            windows = payload["diagnostics"]["windows"] or []
            if windows and windows[0]["conditions"]:
                measures = {m["key"]: m for m in windows[0]["metrics"]}
                assert measures["steps"]["value"] == 20
                assert measures["tokens_step"]["value"] == 3
                assert measures["prefill"]["value"] == 1000
                assert windows[0]["latency"][1]["available"]
                report = {
                    "scope": "synthetic functional contract; no GPU performance measurement",
                    "status": "PASS",
                    "diagnostics": payload["diagnostics"],
                }
                args.output.parent.mkdir(parents=True, exist_ok=True)
                args.output.write_text(json.dumps(report, indent=2) + "\n")
                print("engine scrape/history/RPC contract PASS", flush=True)
                print(f"instance={instance} url=http://127.0.0.1:{port}", flush=True)
                if args.hold_seconds:
                    time.sleep(args.hold_seconds)
                return
            time.sleep(2)
        raise RuntimeError("sampler did not produce a diagnostic interval")
    finally:
        subprocess.run([*command, "stop"], cwd=ROOT, env=env, check=False)
        server.shutdown()
        worker.join(timeout=5)
        server.server_close()


if __name__ == "__main__":
    main()

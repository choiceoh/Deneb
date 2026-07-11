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
    GET  /puppet/request/<id>   full request payload + meta — live or recently
                                finished (the broker keeps the last FINISHED_KEEP
                                full payloads so an answered request can still
                                be re-inspected post-mortem)
    POST /puppet/respond/<id>   {"text","reasoning","tool_calls",...} or {"error"}
    GET  /puppet/history        recent completed exchanges

Client mode (operator CLI; broker URL from --broker or DENEB_PUPPET_URL):
    python3 puppet_broker.py pending [--wait N]
    python3 puppet_broker.py show ID [--full | --raw | --outline
                                      | --tool NAME | --system]
    python3 puppet_broker.py reply ID [--text T] [--reasoning R]
                                      [--tool NAME ARGS_JSON]... [--finish FR]
                                      [--file PAYLOAD_JSON]
    python3 puppet_broker.py fail ID [--message M]
    python3 puppet_broker.py history

Timeout choreography (why keepalives + chunked framing exist):
  - gateway LLM HTTP client: 10 min hard cap, >=5 min per-request context
    (internal/ai/llm/client.go) — SSE comment lines keep bytes flowing.
  - gateway stream-idle watchdog (agentsys/agent/executor_stream.go, default
    3 min): counts REAL progress events only — comments/pings deliberately do
    not reset it, so a held request would get a silent duplicate retry at
    180s. puppet.sh therefore starts the gateway with
    DENEB_STREAM_IDLE_TIMEOUT_MS=-1 (watchdog off; holds stay bounded by the
    turn deadline and STREAM_HOLD_MAX).
  - miniapp.chat.send turn deadline: 5 min for the WHOLE turn
    (internal/runtime/server DefaultTurnDeadline). When it fires the gateway
    drops the connection; the next keepalive write fails and the request is
    marked gone.
  - The SSE response is chunked (HTTP/1.1): if the broker dies mid-hold the
    gateway reads an unexpected EOF and surfaces a stream ERROR. With plain
    HTTP/1.0 close-delimited framing the same death reads as a clean EOF and
    the turn completes as a silent EMPTY success — the worst failure mode for
    a testing tool.
  - Non-stream holds (Complete() callers like title generation) cannot
    receive keepalives, so they are capped at COMPLETE_HOLD_MAX inside the
    5-minute turn deadline — answering after the gateway hung up is useless.

Stdlib only — no external dependencies (matches mock_native_client.py).
"""
from __future__ import annotations

import argparse
import sys

from puppet_broker_cli import cmd_fail, cmd_history, cmd_pending, cmd_reply, cmd_show
from puppet_broker_http import serve

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

    sh = sub.add_parser("show", help="show one held or finished request")
    sh.add_argument("id")
    sh.add_argument("--full", action="store_true")
    sh.add_argument("--raw", action="store_true",
                    help="dump the full request JSON")
    sh.add_argument("--outline", action="store_true",
                    help="per-message sizes + system-prompt section map")
    sh.add_argument("--tool", default="",
                    help="print one tool's full schema and exit")
    sh.add_argument("--system", action="store_true",
                    help="dump the full system prompt text")
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

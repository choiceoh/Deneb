#!/usr/bin/env python3
"""Stdio MCP proxy: single-symbol explore → node (gateway parity).

`codegraph explore GatewayHub` camelCase-splits into Hub/GatewayTab noise.
The runtime agent already reroutes that call to `node` and falls back on a
miss. IDE MCP (Cursor/ZCode/Claude/Codex/Trae) had no such wrap — this
proxy sits in front of `codegraph serve --mcp` so every coding tool gets
the same precision. Multi-token / NL / lowercase area words pass through.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import threading
from dataclasses import dataclass, field
from typing import Any

NODE_MISS = "not found in the codebase"
SYMBOL_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{2,}$")
EXPLORE_NAMES = {"explore", "codegraph_explore"}


def single_symbol_query(query: str) -> str:
    q = (query or "").strip()
    if not SYMBOL_RE.fullmatch(q):
        return ""
    if not any(c.isupper() or c == "_" for c in q):
        return ""
    return q


def sibling_node(name: str) -> str:
    if name.endswith("explore"):
        return name[: -len("explore")] + "node"
    return "codegraph_node"


def reroute_note(symbol: str) -> str:
    return (
        f'[codegraph: "{symbol}"는 단일 심볼이라 explore 대신 node로 정밀 조회했다'
        "(서브토큰 노이즈 제거). 주변 영역·여러 심볼·흐름을 보려면 공백으로 구분한 "
        "여러 토큰이나 질문 문장으로 explore를 다시 불러라.]\n\n"
    )


def _call_name_and_args(msg: dict) -> tuple[str, dict, str]:
    params = msg.get("params") if isinstance(msg.get("params"), dict) else {}
    name = str(params.get("name") or "")
    raw = params.get("arguments")
    key = "arguments"
    if not isinstance(raw, dict):
        raw = params.get("input")
        key = "input"
    return name, raw if isinstance(raw, dict) else {}, key


def explore_symbol(msg: dict) -> str:
    if msg.get("method") != "tools/call":
        return ""
    name, args, _ = _call_name_and_args(msg)
    if name not in EXPLORE_NAMES:
        return ""
    return single_symbol_query(str(args.get("query") or args.get("symbol") or ""))


def node_request(msg: dict, symbol: str, new_id: Any) -> dict:
    name, _, args_key = _call_name_and_args(msg)
    params = dict(msg.get("params") or {})
    params["name"] = sibling_node(name)
    params[args_key] = {"symbol": symbol, "includeCode": True}
    return {**msg, "id": new_id, "params": params}


def result_text(msg: dict) -> str:
    result = msg.get("result")
    if isinstance(result, str):
        return result
    if isinstance(result, dict):
        parts: list[str] = []
        for item in result.get("content") or []:
            if isinstance(item, dict) and item.get("type") == "text":
                parts.append(str(item.get("text") or ""))
        if parts:
            return "\n".join(parts)
        if isinstance(result.get("message"), str):
            return result["message"]
    if isinstance(msg.get("error"), dict):
        return str(msg["error"].get("message") or "")
    return ""


def is_node_miss(msg: dict) -> bool:
    if msg.get("error"):
        return True
    text = result_text(msg)
    return not text.strip() or NODE_MISS in text


def annotate_result(msg: dict, symbol: str) -> dict:
    note = reroute_note(symbol)
    out = dict(msg)
    result = msg.get("result")
    if isinstance(result, str):
        out["result"] = note + result
        return out
    if isinstance(result, dict):
        copied = dict(result)
        content = list(copied.get("content") or [])
        if content and isinstance(content[0], dict) and content[0].get("type") == "text":
            first = dict(content[0])
            first["text"] = note + str(first.get("text") or "")
            content[0] = first
            copied["content"] = content
        else:
            copied["content"] = [{"type": "text", "text": note}] + content
        out["result"] = copied
    return out


def restore_id(msg: dict, client_id: Any) -> dict:
    out = dict(msg)
    out["id"] = client_id
    return out


@dataclass
class ExploreReroute:
    """Hold an explore call, try node, fall back to the original explore."""

    _seq: int = 10**9
    pending: dict[Any, dict] = field(default_factory=dict)

    def _alloc(self) -> str:
        self._seq += 1
        return f"deneb-cg-{self._seq}"

    def on_client(self, msg: dict) -> list[dict]:
        if not isinstance(msg, dict) or "method" not in msg:
            return [msg]
        symbol = explore_symbol(msg)
        if not symbol or "id" not in msg:
            return [msg]
        server_id = self._alloc()
        self.pending[server_id] = {
            "client_id": msg["id"],
            "symbol": symbol,
            "original": msg,
            "stage": "node",
        }
        return [node_request(msg, symbol, server_id)]

    def on_server(self, msg: dict) -> tuple[list[dict], list[dict]]:
        """Return (to_client, to_server)."""
        if not isinstance(msg, dict):
            return [msg], []
        pending = self.pending.get(msg.get("id"))
        if pending is None:
            return [msg], []
        if pending["stage"] == "node":
            if not is_node_miss(msg):
                del self.pending[msg["id"]]
                annotated = annotate_result(restore_id(msg, pending["client_id"]), pending["symbol"])
                return [annotated], []
            explore_id = self._alloc()
            self.pending[explore_id] = {**pending, "stage": "explore"}
            del self.pending[msg["id"]]
            original = dict(pending["original"])
            original["id"] = explore_id
            return [], [original]
        del self.pending[msg["id"]]
        return [restore_id(msg, pending["client_id"])], []


def encode_message(msg: dict) -> bytes:
    body = json.dumps(msg, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    return f"Content-Length: {len(body)}\r\n\r\n".encode("ascii") + body


def read_message(stream) -> dict | None:
    header = b""
    while True:
        chunk = stream.read(1)
        if not chunk:
            return None if not header else _parse_loose(header)
        header += chunk
        if header.endswith(b"\r\n\r\n") or header.endswith(b"\n\n"):
            break
        if header.startswith(b"{") and header.endswith(b"\n") and b"Content-Length" not in header:
            return json.loads(header.decode("utf-8"))
    match = re.search(br"Content-Length:\s*(\d+)", header, re.I)
    if not match:
        return _parse_loose(header)
    raw = stream.read(int(match.group(1)))
    if not raw:
        return None
    return json.loads(raw.decode("utf-8"))


def _parse_loose(header: bytes) -> dict | None:
    text = header.decode("utf-8", errors="replace").strip()
    if not text:
        return None
    return json.loads(text)


def resolve_codegraph() -> str:
    for candidate in (
        shutil.which("codegraph"),
        os.path.expanduser("~/.local/bin/codegraph"),
        os.path.expanduser("~/.npm-global/bin/codegraph"),
    ):
        if candidate and os.path.exists(candidate):
            return candidate
    return ""


def serve(child_argv: list[str]) -> int:
    proc = subprocess.Popen(
        child_argv,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=sys.stderr,
    )
    assert proc.stdin is not None and proc.stdout is not None
    hub = ExploreReroute()
    hub_lock = threading.Lock()
    stdin_lock = threading.Lock()

    def write_child(msg: dict) -> None:
        with stdin_lock:
            proc.stdin.write(encode_message(msg))
            proc.stdin.flush()

    def client_to_server() -> None:
        try:
            while True:
                msg = read_message(sys.stdin.buffer)
                if msg is None:
                    break
                with hub_lock:
                    outgoing = hub.on_client(msg)
                for item in outgoing:
                    write_child(item)
        finally:
            try:
                proc.stdin.close()
            except OSError:
                pass

    def server_to_client() -> None:
        while True:
            msg = read_message(proc.stdout)
            if msg is None:
                break
            with hub_lock:
                to_client, to_server = hub.on_server(msg)
            for extra in to_server:
                write_child(extra)
            for item in to_client:
                sys.stdout.buffer.write(encode_message(item))
                sys.stdout.buffer.flush()

    reader = threading.Thread(target=server_to_client, daemon=True)
    reader.start()
    client_to_server()
    reader.join(timeout=2)
    if proc.poll() is None:
        proc.terminate()
    return proc.wait() or 0


def main(argv: list[str] | None = None) -> int:
    args = list(argv if argv is not None else sys.argv[1:])
    if args and args[0] == "--":
        args = args[1:]
    if not args:
        binary = resolve_codegraph()
        if not binary:
            print("codegraph not found (install: npm i -g @colbymchenry/codegraph)", file=sys.stderr)
            return 127
        args = [binary, "serve", "--mcp"]
    elif args[0] == "codegraph" or os.path.basename(args[0]) == "codegraph":
        pass
    else:
        binary = resolve_codegraph()
        if not binary:
            print("codegraph not found (install: npm i -g @colbymchenry/codegraph)", file=sys.stderr)
            return 127
        args = [binary, *args]
    return serve(args)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — MCP stdio must not crash the parent IDE
        sys.exit(1)

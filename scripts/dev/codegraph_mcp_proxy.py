#!/usr/bin/env python3
"""Stdio MCP proxy: precision wrap in front of `codegraph serve --mcp`.

Reroutes a single specific symbol (bare, dotted RPC, or one PascalCase
token inside NL) from explore → node, caps uncapped explore `maxFiles`,
and appends nearby CLAUDE/AGENTS maps. Node miss falls back to explore.
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

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from codegraph_folder_docs import folder_docs  # noqa: E402

NODE_MISS = "not found in the codebase"
SYMBOL_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{2,}$")
TOKEN_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")
DOTTED_RE = re.compile(r"^[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+$")
EXPLORE_NAMES = {"explore", "codegraph_explore"}
NODE_NAMES = {"node", "codegraph_node"}
DEFAULT_EXPLORE_MAX_FILES = 6


def single_symbol_query(query: str) -> str:
    q = (query or "").strip()
    if not SYMBOL_RE.fullmatch(q):
        return ""
    if not any(c.isupper() or c == "_" for c in q):
        return ""
    return q


def extract_explore_symbol(query: str) -> str:
    """Bare symbol, dotted RPC/tool name, or one specific token inside NL."""
    q = (query or "").strip()
    if DOTTED_RE.fullmatch(q):
        return q
    hit = single_symbol_query(q)
    if hit:
        return hit
    specific = [tok for tok in TOKEN_RE.findall(q) if single_symbol_query(tok)]
    uniq = list(dict.fromkeys(specific))
    return uniq[0] if len(uniq) == 1 else ""


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
    return extract_explore_symbol(str(args.get("query") or args.get("symbol") or ""))


def cap_explore_args(args: dict) -> dict:
    if "maxFiles" in args or "max_files" in args:
        return args
    return {**args, "maxFiles": DEFAULT_EXPLORE_MAX_FILES}


def append_text(msg: dict, extra: str) -> dict:
    if not extra:
        return msg
    out = dict(msg)
    result = msg.get("result")
    if isinstance(result, str):
        out["result"] = result + extra
        return out
    if isinstance(result, dict):
        copied = dict(result)
        content = list(copied.get("content") or [])
        if content and isinstance(content[0], dict) and content[0].get("type") == "text":
            first = dict(content[0])
            first["text"] = str(first.get("text") or "") + extra
            content[0] = first
            copied["content"] = content
        else:
            copied["content"] = content + [{"type": "text", "text": extra}]
        out["result"] = copied
    return out


def enrich_result(msg: dict, root: str, query: str) -> dict:
    if not root:
        return msg
    return append_text(msg, folder_docs(result_text(msg), root, query))


def serve_root_from_argv(argv: list[str]) -> str:
    prev = ""
    for arg in argv:
        if prev in ("-p", "--path") and arg:
            return arg
        prev = arg
    return os.getcwd()


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

    root: str = ""
    _seq: int = 10**9
    pending: dict[Any, dict] = field(default_factory=dict)
    queries: dict[Any, str] = field(default_factory=dict)

    def _alloc(self) -> str:
        self._seq += 1
        return f"deneb-cg-{self._seq}"

    def _remember(self, msg_id: Any, query: str) -> None:
        if msg_id is not None:
            self.queries[msg_id] = query

    def _finish(self, msg: dict, query: str) -> dict:
        return enrich_result(msg, self.root, query)

    def on_client(self, msg: dict) -> list[dict]:
        if not isinstance(msg, dict) or msg.get("method") != "tools/call":
            return [msg]
        name, args, key = _call_name_and_args(msg)
        query = str(args.get("query") or args.get("symbol") or "")
        if name in EXPLORE_NAMES | NODE_NAMES and "id" in msg:
            self._remember(msg["id"], query)
        symbol = explore_symbol(msg)
        if symbol and "id" in msg:
            server_id = self._alloc()
            self.pending[server_id] = {
                "client_id": msg["id"],
                "symbol": symbol,
                "original": msg,
                "stage": "node",
            }
            self._remember(server_id, symbol)
            return [node_request(msg, symbol, server_id)]
        if name in EXPLORE_NAMES:
            capped = cap_explore_args(args)
            if capped is not args:
                params = dict(msg.get("params") or {})
                params[key] = capped
                return [{**msg, "params": params}]
        return [msg]

    def on_server(self, msg: dict) -> tuple[list[dict], list[dict]]:
        """Return (to_client, to_server)."""
        if not isinstance(msg, dict):
            return [msg], []
        pending = self.pending.get(msg.get("id"))
        if pending is None:
            query = self.queries.pop(msg.get("id"), "")
            return [self._finish(msg, query)], []
        if pending["stage"] == "node":
            if not is_node_miss(msg):
                del self.pending[msg["id"]]
                annotated = annotate_result(restore_id(msg, pending["client_id"]), pending["symbol"])
                return [self._finish(annotated, pending["symbol"])], []
            explore_id = self._alloc()
            self.pending[explore_id] = {**pending, "stage": "explore"}
            self._remember(explore_id, pending["symbol"])
            del self.pending[msg["id"]]
            original = dict(pending["original"])
            original["id"] = explore_id
            return [], [original]
        del self.pending[msg["id"]]
        return [self._finish(restore_id(msg, pending["client_id"]), pending["symbol"])], []


def encode_message(msg: dict, framing: str = "ndjson") -> bytes:
    """Encode one JSON-RPC message.

    The MCP stdio transport is newline-delimited JSON; Content-Length framing
    is LSP's, and the two are not interchangeable. This encoder used to emit
    Content-Length unconditionally, which broke BOTH peers at once — the proxy
    sat between an NDJSON client and an NDJSON server and inserted a dialect
    neither spoke.

    Measured on 2026-08-27 against codegraph 1.5.0: an NDJSON initialize gets a
    normal result, the identical request wrapped in Content-Length gets
    {"code":-32700,"message":"Parse error: invalid JSON"}. On the client side
    Deneb's gateway reads stdout with a bufio.Scanner (NDJSON only) and logged
    79 "tool discovery failed ... server=codegraph" over two days, each paired
    with "mcp server sent unparseable line | bytes=18 invalid character 'C'" —
    18 bytes starting with C is exactly "Content-Length: 89".

    NDJSON is therefore the default in both directions, and Content-Length is
    used only when a peer demonstrably spoke it first.
    """
    body = json.dumps(msg, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    if framing == "content-length":
        return f"Content-Length: {len(body)}\r\n\r\n".encode("ascii") + body
    return body + b"\n"


def read_message(stream, observed: list[str] | None = None) -> dict | None:
    """Read one message, optionally recording which framing the peer used.

    observed is a one-element list used as an out-param so the caller can reply
    in the SAME dialect the peer spoke. Mirroring is what makes the proxy
    correct for both kinds of client without a flag anyone has to set.
    """
    header = b""
    while True:
        chunk = stream.read(1)
        if not chunk:
            return None if not header else _parse_loose(header)
        header += chunk
        if header.endswith(b"\r\n\r\n") or header.endswith(b"\n\n"):
            break
        if header.startswith(b"{") and header.endswith(b"\n") and b"Content-Length" not in header:
            if observed is not None:
                observed[0] = "ndjson"
            return json.loads(header.decode("utf-8"))
    match = re.search(br"Content-Length:\s*(\d+)", header, re.I)
    if not match:
        if observed is not None:
            observed[0] = "ndjson"
        return _parse_loose(header)
    if observed is not None:
        observed[0] = "content-length"
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
    hub = ExploreReroute(root=serve_root_from_argv(child_argv))
    hub_lock = threading.Lock()
    stdin_lock = threading.Lock()
    # Framing the CLIENT speaks, learned from its first message and mirrored in
    # every reply. Defaults to content-length so behavior is unchanged for the
    # LSP-framed clients this proxy already served; an NDJSON client flips it on
    # its first request, which is always initialize.
    client_framing = ["ndjson"]
    # The CHILD's framing is tracked separately — the two peers need not agree,
    # and assuming they do is how the original bug read as "works for me".
    child_framing = ["ndjson"]

    def write_child(msg: dict) -> None:
        with stdin_lock:
            proc.stdin.write(encode_message(msg, child_framing[0]))
            proc.stdin.flush()

    def client_to_server() -> None:
        try:
            while True:
                msg = read_message(sys.stdin.buffer, client_framing)
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
            msg = read_message(proc.stdout, child_framing)
            if msg is None:
                break
            with hub_lock:
                to_client, to_server = hub.on_server(msg)
            for extra in to_server:
                write_child(extra)
            for item in to_client:
                sys.stdout.buffer.write(encode_message(item, client_framing[0]))
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

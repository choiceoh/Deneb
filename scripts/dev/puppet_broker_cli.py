"""Operator-side HTTP client and renderers for the puppet broker."""
from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.request

from puppet_broker_state import PENDING_WAIT_CAP, content_preview, content_text, system_outline

# ---------------------------------------------------------------------------
# Operator CLI (client mode)
# ---------------------------------------------------------------------------

def broker_url(args) -> str:
    url = (getattr(args, "broker", "") or
           os.environ.get("DENEB_PUPPET_URL", "")).strip()
    return (url or "http://127.0.0.1:18793").rstrip("/")


def api(args, method: str, path: str, body: dict | None = None) -> dict:
    url = broker_url(args) + path
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=70.0) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        try:
            return json.loads(e.read().decode("utf-8"))
        except (ValueError, OSError):
            return {"error": f"HTTP {e.code}"}
    except (urllib.error.URLError, OSError) as e:
        print(f"broker unreachable at {broker_url(args)}: {e}",
              file=sys.stderr)
        sys.exit(2)


def cmd_pending(args) -> int:
    budget = max(float(args.wait or 0), 0.0)
    while True:
        step = min(budget, PENDING_WAIT_CAP) if budget > 0 else 0
        res = api(args, "GET", f"/puppet/pending?wait={step:g}")
        items = res.get("pending") or []
        budget -= step
        if items or budget <= 0:
            break
    if not items:
        print("(no pending requests)")
        return 1
    for it in items:
        # model = the per-role seat alias (main-seat, coding-seat, ...) —
        # this is how the operator tells WHICH role is calling.
        print(f"{it['id']}  +{it['ageSec']}s  {it['kind']}  "
              f"{it.get('model') or '?'}  "
              f"msgs={it['messages']} tools={it['tools']}  "
              f"last={it['lastRole']}: {it['lastPreview'][:120]}")
    return 0


def render_message(i: int, m: dict, limit: int) -> str:
    role = m.get("role", "?")
    lines = [f" [{i}] {role}"]
    if m.get("tool_call_id"):
        lines[0] += f" (tool_call_id={m['tool_call_id']})"
    body = content_preview(m.get("content"), limit)
    if body:
        lines.append(f"     {body}")
    for tc in m.get("tool_calls") or []:
        fn = tc.get("function") or {}
        args = str(fn.get("arguments", ""))
        if len(args) > limit:
            args = args[:limit] + "…"
        lines.append(f"     ↳ tool_call {tc.get('id')}: "
                     f"{fn.get('name')}({args})")
    return "\n".join(lines)


def previous_sys_hash(args, current_id: str, model: str) -> tuple[str, str]:
    """(prev_id, prev_hash) of the closest earlier SAME-MODEL request.

    Sources: finished exchanges (history summaries) + currently live requests.
    Lets `show` answer the seat's cache-prefix question — did the system
    prompt change since this role's previous request? — without eyeballing
    40K chars. Same-model only: each role (seat alias) has its own prompt
    pipeline, so a cross-role comparison would always read as CHANGED.
    """
    def id_num(rid) -> int:
        try:
            return int(str(rid).lstrip("r"))
        except ValueError:
            return -1

    cur = id_num(current_id)
    if cur < 0:
        return "", ""
    hist = api(args, "GET", "/puppet/history").get("history") or []
    pend = api(args, "GET", "/puppet/pending?wait=0").get("pending") or []
    best, best_hash = -1, ""
    for summ in [h.get("summary") or {} for h in hist] + pend:
        n = id_num(summ.get("id"))
        if 0 <= n < cur and n > best and summ.get("sysHash") \
                and summ.get("model") == model:
            best, best_hash = n, summ["sysHash"]
    return (f"r{best}", best_hash) if best >= 0 else ("", "")


def show_outline(msgs: list, tools: list) -> None:
    for i, m in enumerate(msgs):
        role = m.get("role", "?")
        text = content_text(m.get("content"))
        line = f" [{i}] {role}  {len(text)} chars"
        calls = [str((tc.get("function") or {}).get("name", "?"))
                 for tc in m.get("tool_calls") or []]
        if calls:
            line += "  tool_calls: " + ", ".join(calls)
        if m.get("tool_call_id"):
            line += f"  (tool_call_id={m['tool_call_id']})"
        print(line)
        if role == "system":
            for chars, headline in system_outline(text):
                print(f"    {chars:>7}  {headline}")
    rows = []
    for t in tools:
        fn = t.get("function") or {}
        rows.append(f"{fn.get('name', '?')}"
                    f"({len(json.dumps(fn, ensure_ascii=False))})")
    print(f"-- tools({len(tools)}) name(schema bytes): {', '.join(rows)}")


def cmd_show(args) -> int:
    res = api(args, "GET", f"/puppet/request/{args.id}")
    if res.get("error"):
        print(res["error"], file=sys.stderr)
        return 1
    req = res.get("request") or {}
    msgs = req.get("messages") or []
    tools = req.get("tools") or []
    status = res.get("status", "waiting")

    if args.raw:
        print(json.dumps(req, ensure_ascii=False, indent=2))
        return 0
    if args.tool:
        for t in tools:
            fn = t.get("function") or {}
            if fn.get("name") == args.tool:
                print(json.dumps(fn, ensure_ascii=False, indent=2))
                return 0
        names = ", ".join(str((t.get("function") or {}).get("name", "?"))
                          for t in tools)
        print(f"no tool {args.tool!r} in this request — tools: {names}",
              file=sys.stderr)
        return 1
    if args.system:
        for m in msgs:
            if m.get("role") == "system":
                print(content_text(m.get("content")))
                return 0
        print("(no system message in this request)", file=sys.stderr)
        return 1

    sys_hash = res.get("sysHash") or ""
    sys_note = ""
    if sys_hash:
        prev_id, prev_hash = previous_sys_hash(args, res["id"],
                                               req.get("model") or "")
        if not prev_hash:
            sys_note = f"  sys={sys_hash}"
        elif prev_hash == sys_hash:
            sys_note = f"  sys={sys_hash} (unchanged since {prev_id})"
        else:
            sys_note = f"  sys={sys_hash} (CHANGED vs {prev_id}={prev_hash})"
    head = (f"== {res['id']}  held {res['ageSec']}s  {res['kind']}  "
            f"model={req.get('model')}  messages={len(msgs)}  "
            f"tools={len(tools)}{sys_note}")
    if status != "waiting":
        head += f"  [{status}]"
    print(head)

    if args.outline:
        show_outline(msgs, tools)
        return 0

    limit = 100000 if args.full else 600
    shown = msgs if args.full else msgs[-8:]
    skipped = len(msgs) - len(shown)
    if skipped:
        print(f" … {skipped} earlier message(s) hidden (--full to show)")
    for i, m in enumerate(shown, start=skipped):
        if m.get("role") == "system" and not args.full:
            # The system prompt is huge and rarely changes between requests;
            # the hash header answers "did it change?" — body on demand.
            print(f" [{i}] system  {len(content_text(m.get('content')))} chars"
                  f"  sha={sys_hash or '?'}"
                  "  (--system full text | --outline section map)")
            continue
        print(render_message(i, m, 100000 if args.full else limit))
    names = ", ".join(str((t.get("function") or {}).get("name", "?"))
                      for t in tools)
    print(f"-- tools({len(tools)}): {names}")
    if status == "waiting":
        print(f"-- reply:  puppet.sh reply {res['id']} --text \"...\"")
        print(f"           puppet.sh reply {res['id']} "
              "--tool NAME '{\"arg\":1}'   (repeatable; --raw for schemas)")
    else:
        resp = res.get("response") or {}
        if resp.get("error"):
            print(f"-- answered [{status}]: error: {resp['error']}")
        elif resp.get("tool_calls"):
            calls = ", ".join(str(tc.get("name", "?"))
                              for tc in resp["tool_calls"])
            print(f"-- answered [{status}]: tool_calls: {calls}")
        else:
            print(f"-- answered [{status}]: "
                  f"text={content_preview(resp.get('text'), 200)!r}")
    return 0


def cmd_reply(args) -> int:
    if args.file:
        with open(args.file, encoding="utf-8") as f:
            payload = json.load(f)
    else:
        payload = {}
        if args.text is not None:
            payload["text"] = args.text
        if args.reasoning:
            payload["reasoning"] = args.reasoning
        tool_calls = []
        for name, raw in args.tool or []:
            try:
                parsed = json.loads(raw)
            except ValueError:
                parsed = raw  # pass through malformed args on purpose
            tool_calls.append({"name": name, "arguments": parsed})
        if tool_calls:
            payload["tool_calls"] = tool_calls
        if args.finish:
            payload["finish_reason"] = args.finish
    if not payload:
        print("nothing to send — use --text/--tool/--file", file=sys.stderr)
        return 1
    res = api(args, "POST", f"/puppet/respond/{args.id}", payload)
    if not res.get("ok"):
        print(f"reply failed: {res.get('error')}", file=sys.stderr)
        return 1
    print(f"answered {args.id}")
    return 0


def cmd_fail(args) -> int:
    res = api(args, "POST", f"/puppet/respond/{args.id}",
              {"error": args.message or "aborted by puppet operator"})
    if not res.get("ok"):
        print(f"fail failed: {res.get('error')}", file=sys.stderr)
        return 1
    print(f"failed {args.id} (gateway sees a provider error)")
    return 0


def cmd_history(args) -> int:
    res = api(args, "GET", "/puppet/history")
    items = res.get("history") or []
    if not items:
        print("(empty)")
        return 0
    for it in items[-20:]:
        resp = it.get("response") or {}
        model = (it.get("summary") or {}).get("model") or "?"
        what = ("error: " + str(resp.get("error"))) if resp.get("error") else \
            (f"tools={[t.get('name') for t in resp.get('tool_calls') or []]}"
             if resp.get("tool_calls") else
             f"text={content_preview(resp.get('text'), 80)!r}")
        print(f"{it['id']}  {it['status']:8s} {model:18s} "
              f"held={it['heldSec']}s  {what}")
    return 0

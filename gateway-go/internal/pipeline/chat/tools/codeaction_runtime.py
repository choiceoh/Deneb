# codeaction_runtime.py — Deneb code_action sandbox runtime.
#
# Runs model-written Python under a PEP 578 audit hook that blocks the
# escape/exfil primitives an injected script could reach for:
#   - network (except the loopback tool bridge),
#   - subprocess / os.system / fork,
#   - filesystem writes outside the scratch dir,
#   - reads of known-secret paths,
#   - ctypes / raw memory.
# Legitimate Deneb data access goes through the `deneb` object, which POSTs to
# the in-process bridge; the read-only tool allowlist is enforced on the Go side.
#
# This is interpreter-level confinement, NOT an OS sandbox. It is sized to the
# realistic threat: an LLM (possibly steered by injected text) writing ordinary
# Python. Two more controls live on the Go side: the subprocess is launched with
# a minimal environment (the gateway's secret env vars are NOT inherited) and a
# wall-clock timeout + output cap. Memory/CPU are bounded by setrlimit below.

import sys
import os
import json
import urllib.request

_SANDBOX = os.path.realpath(os.environ["DENEB_SANDBOX_DIR"])
_BRIDGE_PORT = int(os.environ["DENEB_BRIDGE_PORT"])
_BRIDGE_URL = "http://127.0.0.1:%d/" % _BRIDGE_PORT
# Capture the bridge token, then scrub it from the environment so model code
# cannot read it back via os.environ. (Possessing it grants nothing beyond the
# read-only allowlist the Go bridge enforces, but defense in depth is cheap.)
_BRIDGE_TOKEN = os.environ.pop("DENEB_BRIDGE_TOKEN")

# Best-effort resource ceilings: bound a runaway loop / memory leak. Lowering
# the hard limit is one-way for a non-root process, so model code cannot raise
# these back up. Generous on purpose — the feature exists to process real data.
try:
    import resource

    _as_cap = 2 * 1024 * 1024 * 1024  # 2 GiB address space
    resource.setrlimit(resource.RLIMIT_AS, (_as_cap, _as_cap))
    _cpu_cap = int(os.environ.get("DENEB_CPU_SECONDS", "65"))
    resource.setrlimit(resource.RLIMIT_CPU, (_cpu_cap, _cpu_cap))
except Exception:
    pass


def _secret_roots():
    """Paths whose contents are never readable from a code_action script.

    A denylist (not an allowlist): stdlib imports must keep reading
    site-packages, so we cannot confine reads to the sandbox the way we confine
    writes. We block only the obvious secret stores."""
    home = os.path.expanduser("~")
    cand = [
        os.path.join(home, ".deneb"),
        os.path.join(home, ".ssh"),
        os.path.join(home, ".aws"),
        os.path.join(home, ".gnupg"),
        os.path.join(home, ".config", "gcloud"),
        os.path.join(home, ".netrc"),
        os.path.join(home, ".profile"),
        "/etc/shadow",
        "/etc/sudoers",
        "/proc/self/mem",
        "/proc/self/environ",
    ]
    out = []
    for p in cand:
        try:
            out.append(os.path.realpath(p))
        except Exception:
            pass
    return out


_SECRET_ROOTS = _secret_roots()
_WRITE_FLAGS = os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_APPEND | os.O_TRUNC


def _under(path, root):
    try:
        rp = os.path.realpath(path)
    except Exception:
        rp = str(path)
    return rp == root or rp.startswith(root + os.sep)


def _blocked(msg):
    raise PermissionError("code_action sandbox: " + msg)


def _audit(event, args):
    # --- network: only the loopback bridge port may be dialed ---
    if event == "socket.connect":
        addr = args[1] if len(args) > 1 else None
        if (isinstance(addr, tuple) and len(addr) >= 2
                and addr[0] == "127.0.0.1" and addr[1] == _BRIDGE_PORT):
            return
        _blocked("network is disabled (only the Deneb tool bridge is reachable)")
    if event == "socket.bind":
        _blocked("listening sockets are disabled")
    # --- process spawning ---
    if (event in ("os.system", "os.fork", "os.forkpty", "os.posix_spawn",
                  "subprocess.Popen", "pty.spawn")
            or event.startswith("os.exec") or event.startswith("os.spawn")):
        _blocked("spawning processes is disabled")
    # --- ctypes / raw memory ---
    if event.startswith("ctypes."):
        _blocked("ctypes is disabled")
    # --- imports that defeat the hook or spawn ---
    if event == "import":
        mod = args[0] if args else ""
        if mod in ("ctypes", "_ctypes", "subprocess", "multiprocessing",
                   "multiprocessing.spawn", "pty"):
            _blocked("importing %r is disabled" % mod)
        return
    # --- filesystem ---
    if event == "open":
        path = args[0] if len(args) > 0 else None
        mode = args[1] if len(args) > 1 else None
        flags = args[2] if len(args) > 2 else None
        if path is None:
            return
        is_write = (isinstance(mode, str) and any(c in mode for c in "wax+")) or \
                   (isinstance(flags, int) and (flags & _WRITE_FLAGS) != 0)
        if is_write and not _under(path, _SANDBOX):
            _blocked("file writes are limited to the scratch directory")
        for root in _SECRET_ROOTS:
            if _under(path, root):
                _blocked("reading %s is disabled" % root)
        return


sys.addaudithook(_audit)


class _Deneb:
    """Read-only access to Deneb tools via the in-process bridge.

    Each method returns the tool's text result (a str). Allowed surface:
      deneb.gmail(action, query=..., message_id=..., max=...)
          actions: inbox, search, read, thread, analyze
      deneb.calendar(action, **kw)
          actions: list, get, free_slots
      deneb.contacts(action, query)
          actions: lookup, search
      deneb.wiki(action, query=..., **kw)
          actions: search, read, index, daily, status
    Write/outbound actions (gmail send/reply, calendar create, wiki write, ...)
    are rejected by the bridge."""

    def _call(self, tool, args):
        body = json.dumps({"tool": tool, "args": args}).encode("utf-8")
        req = urllib.request.Request(
            _BRIDGE_URL, data=body, method="POST",
            headers={"Content-Type": "application/json",
                     "X-Deneb-Bridge-Token": _BRIDGE_TOKEN})
        with urllib.request.urlopen(req, timeout=60) as resp:
            payload = json.loads(resp.read().decode("utf-8"))
        if not payload.get("ok"):
            raise RuntimeError("deneb.%s error: %s" % (tool, payload.get("error")))
        return payload.get("result", "")

    def gmail(self, action, **kw):
        kw["action"] = action
        return self._call("gmail", kw)

    def calendar(self, action, **kw):
        kw["action"] = action
        return self._call("calendar", kw)

    def contacts(self, action, query, **kw):
        kw["action"] = action
        kw["query"] = query
        return self._call("contacts", kw)

    def wiki(self, action, **kw):
        kw["action"] = action
        return self._call("wiki", kw)


def _run():
    with open(os.path.join(_SANDBOX, "_main.py"), "r", encoding="utf-8") as f:
        src = f.read()
    g = {"__name__": "__main__", "__builtins__": __builtins__, "deneb": _Deneb()}
    exec(compile(src, "<code_action>", "exec"), g)  # noqa: S102 — sandboxed above


_run()

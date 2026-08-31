"""Shared isolation helpers for tests that execute repository shell scripts."""

from __future__ import annotations

import os
import subprocess
import time
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]

# The toolchain a script under test is allowed to see. These tests exist to pin
# shell behavior, so the only commands a script may resolve are the standard Unix
# ones plus whatever fake bin the test supplies — never the operator's user-space
# installs. Inheriting the host PATH broke that: deploy.sh's `command -v
# codegraph` gate found a real ~/.npm-global/bin/codegraph and spent ~0.85s per
# invocation (plus a telemetry HTTP call) building an index inside the fixture's
# throwaway $PROD_DIR, which blew the deploy tests' 10s timeout whenever the box
# was busy — and was invisible in CI, where codegraph is not installed at all. A
# fixed system PATH makes every optional-tool branch decided by the fixture.
SYSTEM_PATH = os.pathsep.join(("/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"))


def _dedent_fixture(body: str) -> str:
    r"""Strip the first non-blank line's indentation from every line carrying it.

    `textwrap.dedent` removes the prefix common to *all* lines, so one line at
    column 0 collapses the common prefix to nothing and turns the dedent into a
    silent no-op. Fixture bodies hit this constantly: written as non-raw
    triple-quoted strings, an idiom like `printf 'git args=%s\n' "$*"` embeds a
    real newline, and the format string's tail becomes a column-0 line. The
    no-op then leaves `#!/usr/bin/env bash` indented -- an inert comment rather
    than a shebang.

    Measuring from the first non-blank line keeps the shebang at column 0.
    Lines that do not carry the prefix are left untouched rather than stripped,
    which preserves payloads whose leading whitespace is deliberate: heredoc
    bodies, unindented heredoc terminators, and printf output such as
    systemctl's "   Active: active".
    """
    lines = body.split("\n")
    first = next((line for line in lines if line.strip()), "")
    prefix = first[: len(first) - len(first.lstrip(" \t"))]
    if not prefix:
        return body
    dedented = []
    for line in lines:
        if line.startswith(prefix):
            dedented.append(line[len(prefix) :])
        elif not line.strip():
            dedented.append("")
        else:
            dedented.append(line)
    return "\n".join(dedented)


def write_executable(path: Path, body: str) -> Path:
    """Write a dedented executable fixture and return its path."""
    path.parent.mkdir(parents=True, exist_ok=True)
    text = _dedent_fixture(body).lstrip("\n")
    if not text.startswith("#!"):
        raise AssertionError(
            f"fixture {path} would be written without a leading shebang: {text[:40]!r}. "
            "An indented shebang is an inert comment; the file only runs because "
            "bash re-executes ENOEXEC files itself, so it would fail under "
            "`sh -c`, `env -i`, xargs, or a direct execve."
        )
    path.write_text(text, encoding="utf-8")
    path.chmod(0o755)
    return path


def isolated_env(home: Path, fake_bin: Path | None = None, **values: str) -> dict[str, str]:
    """Build a deterministic environment while retaining standard Unix tools."""
    path = SYSTEM_PATH
    if fake_bin is not None:
        path = f"{fake_bin}{os.pathsep}{path}"
    env = {
        "HOME": str(home),
        "PATH": path,
        "LANG": "C.UTF-8",
        "LC_ALL": "C.UTF-8",
    }
    env.update(values)
    return env


def run_script(
    relative_path: str,
    *args: str,
    env: dict[str, str] | None = None,
    cwd: Path | None = None,
    input_text: str | None = None,
    timeout: float = 15,
) -> subprocess.CompletedProcess[str]:
    """Execute one checked-in script without raising on its exit status."""
    return subprocess.run(
        [str(REPO_ROOT / relative_path), *args],
        cwd=str(cwd or REPO_ROOT),
        env=env,
        input=input_text,
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
    )


def wait_for_text(path: Path, needle: str, timeout: float = 5.0) -> str:
    """Poll a fixture log until it contains `needle`, then return its contents.

    Scripts under test launch faked commands in the background (`nohup ... &`),
    so a fake's append to $FAKE_LOG races the parent script's exit. Reading the
    log once makes the assertion depend on process scheduling -- it passes only
    while the child happens to win. Polling makes the ordering explicit without
    weakening what is asserted: on success the text is returned as soon as it
    lands, and on timeout the caller still gets the log so its own assertion
    reports the real diff.
    """
    deadline = time.monotonic() + timeout
    while True:
        text = path.read_text(encoding="utf-8") if path.exists() else ""
        if needle in text or time.monotonic() >= deadline:
            return text
        time.sleep(0.01)


def extract_heredoc(path: Path, opener: str, terminator: str) -> str:
    """Return the executable body between an exact shell heredoc pair."""
    lines = path.read_text(encoding="utf-8").splitlines()
    try:
        start = lines.index(opener) + 1
        end = lines.index(terminator, start)
    except ValueError as exc:
        raise AssertionError(f"missing heredoc contract in {path}") from exc
    return "\n".join(lines[start:end]) + "\n"

"""Shared helpers for behavior tests of executable scripts.

The production tools intentionally keep their historical hyphenated filenames,
so regular Python imports cannot load all of them.  These helpers make the
scripts importable without changing their CLI entry points.
"""

from __future__ import annotations

import contextlib
import hashlib
import importlib.util
import io
import json
import os
import sys
import textwrap
from pathlib import Path
from unittest import mock

REPO_ROOT = Path(__file__).resolve().parents[2]


def load_script(relative_path: str):
    """Load one repository script under a stable, collision-free module name."""
    path = REPO_ROOT / relative_path
    digest = hashlib.sha1(str(path).encode()).hexdigest()[:12]
    module_name = f"deneb_script_test_{path.stem.replace('-', '_')}_{digest}"
    if module_name in sys.modules:
        return sys.modules[module_name]
    spec = importlib.util.spec_from_file_location(module_name, path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    return module


def invoke_main(module, args=(), *, stdin="", env=None, clear_env=False):
    """Run module.main with isolated argv/stdin and captured text streams."""
    stdout = io.StringIO()
    stderr = io.StringIO()
    environment = mock.patch.dict(os.environ, env or {}, clear=clear_env)
    with environment, mock.patch.object(sys, "argv", [module.__file__, *args]):
        with mock.patch.object(sys, "stdin", io.StringIO(stdin)):
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                try:
                    result = module.main()
                except SystemExit as exc:
                    result = exc.code if isinstance(exc.code, int) else 1
    return result, stdout.getvalue(), stderr.getvalue()


def write_files(root: Path, files: dict[str, str]) -> None:
    """Materialize a compact relative-path to text fixture mapping."""
    for relative, content in files.items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(textwrap.dedent(content).lstrip("\n"), encoding="utf-8")


class JSONResponse:
    """Minimal urlopen response/context-manager backed by a JSON value."""

    def __init__(self, value) -> None:
        self.payload = json.dumps(value).encode()

    def read(self) -> bytes:
        return self.payload

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, traceback) -> bool:
        return False

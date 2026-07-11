"""Tracked-file inventory and generated-test provenance for Health Bench 2.0."""

from __future__ import annotations

import os
import re
import subprocess
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Iterable

_TS_TEST_RE = re.compile(r"\.(?:test|spec)\.tsx?$")
_GENERATED_MARKER_RE = re.compile(
    r"(?:code\s+)?generated\s+by\s+([^;\n]+).*?(?:do\s+not\s+edit|@generated)",
    re.IGNORECASE | re.DOTALL,
)
_GENERATOR_PATH_RE = re.compile(
    r"(?P<path>[A-Za-z0-9_./-]+\.(?:go|py|mjs|js|ts|sh))"
)
_DRIFT_CHECK_RE = re.compile(
    r"(?:^|\s)(?:--?check\b|generate-check\b)|\bdrift(?:-check)?\b",
    re.IGNORECASE,
)


def git_files(root: Path) -> list[Path]:
    proc = subprocess.run(
        ["git", "-C", str(root), "ls-files", "--cached", "-z"],
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        detail = proc.stderr.decode("utf-8", errors="replace").strip()
        raise RuntimeError(detail or "git ls-files failed")
    return [
        root / raw.decode("utf-8", errors="surrogateescape")
        for raw in proc.stdout.split(b"\0")
        if raw
    ]


def classify(root: Path, path: Path) -> tuple[str, bool] | None:
    try:
        rel = path.relative_to(root)
    except ValueError:
        return None
    parts = rel.parts
    name = path.name
    if parts and parts[0] == "gateway-go" and path.suffix == ".go":
        return "go", name.endswith("_test.go")
    if len(parts) >= 2 and parts[:2] == ("client-android", "app") and path.suffix == ".kt":
        is_test = any(part.endswith("Test") for part in parts) or name.endswith(
            ("Test.kt", "Tests.kt")
        )
        return "kotlin", is_test
    if len(parts) >= 2 and parts[:2] == ("andromeda", "src") and path.suffix in {
        ".ts",
        ".tsx",
    }:
        return "typescript", bool(_TS_TEST_RE.search(name)) or "__tests__" in parts
    if parts and parts[0] == "scripts" and path.suffix == ".py":
        is_test = name.startswith("test_") or name.endswith("_test.py") or "tests" in parts
        return "python", is_test
    return None


def read_many(paths: Iterable[Path]) -> dict[Path, str]:
    unique = list(dict.fromkeys(paths))

    def read(path: Path) -> tuple[Path, str]:
        try:
            return path, path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            return path, ""

    workers = min(24, max(4, (os.cpu_count() or 4) + 4))
    with ThreadPoolExecutor(max_workers=workers) as pool:
        return dict(pool.map(read, unique))


def control_text(root: Path, files: list[Path], texts: dict[Path, str]) -> str:
    controls = [root / "Makefile"]
    controls.extend(
        path
        for path in files
        if ".github" in path.parts
        and "workflows" in path.parts
        and path.suffix in {".yml", ".yaml"}
    )
    missing = [path for path in controls if path not in texts and path.exists()]
    if missing:
        texts.update(read_many(missing))
    return "\n".join(texts.get(path, "") for path in controls)


def _control_contexts(text: str) -> list[str]:
    """Return Make recipes, YAML ``run`` blocks, and standalone commands.

    A repository-wide search is intentionally insufficient: an unrelated
    health ``--check`` elsewhere in the Makefile must not prove that a named
    generator has a drift check.
    """
    lines = text.splitlines()
    contexts: list[str] = []
    covered: set[int] = set()
    make_target = re.compile(r"^[A-Za-z0-9_./%+-]+(?:\s+[^:]*)?:")
    yaml_run = re.compile(r"^(\s*)(?:-\s+)?run\s*:")
    index = 0
    while index < len(lines):
        if make_target.match(lines[index]):
            end = index + 1
            while end < len(lines) and not make_target.match(lines[end]):
                if lines[end] and not lines[end][0].isspace() and not lines[end].startswith("#"):
                    break
                end += 1
            contexts.append("\n".join(lines[index:end]))
            covered.update(range(index, end))
            index = end
            continue
        run = yaml_run.match(lines[index])
        if run:
            base_indent = len(run.group(1))
            end = index + 1
            while end < len(lines):
                stripped = lines[end].strip()
                indent = len(lines[end]) - len(lines[end].lstrip())
                if stripped and indent <= base_indent:
                    break
                end += 1
            contexts.append("\n".join(lines[index:end]))
            covered.update(range(index, end))
            index = end
            continue
        index += 1
    contexts.extend(line for index, line in enumerate(lines) if index not in covered)
    return contexts


def _generator_has_drift_check(controls: str, generator_rel: str) -> bool:
    return any(
        generator_rel in context and _DRIFT_CHECK_RE.search(context)
        for context in _control_contexts(controls)
    )


def generated_with_provenance(root: Path, rel: str, text: str, controls: str) -> bool:
    head = "\n".join(text.splitlines()[:10])
    marker = _GENERATED_MARKER_RE.search(head)
    if not marker:
        return False
    path_match = _GENERATOR_PATH_RE.search(marker.group(1))
    if not path_match:
        return False
    raw = path_match.group("path").lstrip("./")
    candidates = [root / raw, (root / rel).parent / raw]
    generator = next((candidate for candidate in candidates if candidate.is_file()), None)
    if generator is None:
        return False
    generator_rel = generator.relative_to(root).as_posix()
    return _generator_has_drift_check(controls, generator_rel)


__all__ = [
    "classify",
    "control_text",
    "generated_with_provenance",
    "git_files",
    "read_many",
]

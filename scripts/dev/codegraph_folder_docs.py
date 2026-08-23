#!/usr/bin/env python3
"""Attach nearby CLAUDE/AGENTS maps to CodeGraph explore/node text.

Mirrors the runtime gateway's folder-doc enrichment so IDE MCP results
carry the folder's intent next to the graph. Fail-open: missing root,
paths, or reads leave the original text unchanged.
"""

from __future__ import annotations

import os
import re

SOURCE_PATH_RE = re.compile(
    r"[A-Za-z0-9_][\w./-]*\.(?:go|kt|kts|ts|tsx|js|jsx|rs|py|java|swift|c|cc|cpp|h|hpp)"
)
MAX_DOCS = 3
PER_DOC_BYTES = 1000


def source_paths(text: str) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for match in SOURCE_PATH_RE.findall(text or ""):
        if "/" not in match or ".." in match or match in seen:
            continue
        seen.add(match)
        out.append(match)
    return out


def _safe_join(root: str, rel: str) -> str:
    if not rel or ".." in rel:
        return ""
    full = os.path.normpath(os.path.join(root, rel.replace("/", os.sep)))
    root_abs = os.path.abspath(root)
    if full != root_abs and not full.startswith(root_abs + os.sep):
        return ""
    return full


def _ancestors(rel_file: str) -> list[str]:
    directory = os.path.dirname(rel_file.replace("\\", "/")).strip("/")
    parts = [p for p in directory.split("/") if p]
    dirs = ["/".join(parts[:i]) for i in range(len(parts), 0, -1)]
    dirs.append("")
    return dirs


def _cap_head(text: str, limit: int) -> str:
    raw = text.encode("utf-8")
    if len(raw) <= limit:
        return text
    cut = raw[:limit].decode("utf-8", errors="ignore").rstrip()
    return cut + "\n… (생략)\n"


def _best_sections(body: str, query: str, limit: int) -> str:
    tokens = {t.lower() for t in re.findall(r"[A-Za-z_][A-Za-z0-9_]{2,}", query or "")}
    chunks = re.split(r"(?m)(?=^#{1,3} )", body)
    if tokens:
        scored = []
        for chunk in chunks:
            hay = chunk.lower()
            score = sum(1 for t in tokens if t in hay)
            if score:
                scored.append((score, chunk))
        if scored:
            scored.sort(key=lambda item: -item[0])
            return _cap_head("".join(chunk for _, chunk in scored[:3]), limit)
    return _cap_head(body, limit)


def folder_docs(text: str, root: str, query: str = "") -> str:
    """Return a markdown appendix, or empty when nothing applies."""
    if not root or not os.path.isdir(root):
        return ""
    paths = source_paths(text)
    if not paths:
        return ""
    seen: set[str] = set()
    docs: list[tuple[str, str]] = []
    for path in paths:
        for directory in _ancestors(path):
            for name in ("CLAUDE.md", "AGENTS.md"):
                rel = f"{directory}/{name}" if directory else name
                if rel in seen:
                    continue
                seen.add(rel)
                full = _safe_join(root, rel)
                if not full or not os.path.isfile(full):
                    continue
                try:
                    with open(full, encoding="utf-8") as handle:
                        body = handle.read()
                except OSError:
                    continue
                if not body.strip():
                    continue
                docs.append((rel, _best_sections(body, query, PER_DOC_BYTES)))
                if len(docs) >= MAX_DOCS:
                    break
            if len(docs) >= MAX_DOCS:
                break
        if len(docs) >= MAX_DOCS:
            break
    if not docs:
        return ""
    lines = ["", "## 폴더 맥락 (적용 문서)", "검색 결과 경로에 적용되는 규칙이다. rules는 제약이다."]
    for rel, content in docs:
        lines.extend(["", f"### {rel} (rules)", content.rstrip()])
    return "\n".join(lines) + "\n"

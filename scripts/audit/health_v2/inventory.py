"""Deterministic Go architecture inventory for Health Bench 2.0.

The inventory deliberately uses only high-confidence repository evidence:
production Go files, their internal imports, package-level public contracts,
nearby architecture guides, and recent first-parent Git history.  It excludes
tests and generated files so adding scaffolding cannot improve architecture
scores.
"""

from __future__ import annotations

import re
import subprocess
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path

from . import history as _history
from .history import ChangeCommit, HistoryFacts, collect_history

# Compatibility aliases keep fixture-level tests and local audit tooling stable
# while history ownership lives in its dedicated module.
MIN_HISTORY_COMMITS = _history.MIN_HISTORY_COMMITS


def _collect_history(repo_root: Path) -> HistoryFacts:
    previous_minimum = _history.MIN_HISTORY_COMMITS
    _history.MIN_HISTORY_COMMITS = MIN_HISTORY_COMMITS
    try:
        return _history.collect_history(repo_root)
    finally:
        _history.MIN_HISTORY_COMMITS = previous_minimum


PRUNE_DIRS = {
    ".git",
    ".gradle",
    ".idea",
    ".venv",
    "__pycache__",
    "build",
    "coverage",
    "dist",
    "node_modules",
    "target",
    "testdata",
    "third_party",
    "vendor",
}
GUIDE_NAMES = {"AGENTS.md", "ARCHITECTURE.md", "CLAUDE.md", "README.md"}
_PACKAGE_RE = re.compile(r"(?m)^package\s+([A-Za-z_]\w*)\s*$")
_IMPORT_BLOCK_RE = re.compile(r"(?ms)^import\s*\((.*?)^\s*\)")
_IMPORT_SINGLE_RE = re.compile(
    r'(?m)^import\s+(?:[A-Za-z_.]\w*\s+)?"([^"\n]+)"\s*$'
)
_QUOTED_IMPORT_RE = re.compile(r'"([^"\n]+)"')
_EXPORTED_FUNC_RE = re.compile(
    r"(?m)^func\s+(?:\([^\n)]*\)\s*)?([A-Z][A-Za-z0-9_]*)\s*(?:\[|\()"
)
_EXPORTED_NAMED_RE = re.compile(
    r"(?m)^(?:type|const|var)\s+([A-Z][A-Za-z0-9_]*)\b"
)
_DECLARED_TYPE_RE = re.compile(
    r"(?m)^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+(?:\[[^\n]*\]\s*)?"
)
_GROUP_DECL_RE = re.compile(r"(?m)^(?:const|var)\s*\(")
_GROUP_EXPORTED_RE = re.compile(r"(?m)^\s+([A-Z][A-Za-z0-9_]*)\b")
_DYNAMIC_TYPE_RE = re.compile(
    r"(?:\bany\b|\binterface\s*\{|\bjson\s*\.\s*RawMessage\b|"
    r"\bmap\s*\[\s*string\s*\]\s*(?:any\b|interface\s*\{))"
)
_DEPENDENCY_BAG_RE = re.compile(r"(?:Deps|Config|Options)$")


@dataclass(frozen=True)
class PackageFacts:
    """Static facts for one current production Go package."""

    key: str
    path: str
    package_name: str
    source_loc: int
    source_files: int
    imports: tuple[str, ...]
    exported_declarations: int
    dynamic_exported_contracts: int
    max_dependency_bag_fields: int
    package_doc_chars: int
    guide_path: str | None
    guide_strength: float

    @property
    def has_package_doc(self) -> bool:
        # A one-line restatement of the package name is not useful navigation.
        return self.package_doc_chars >= 40

    @property
    def has_architecture_guide(self) -> bool:
        return self.guide_path is not None and self.guide_strength >= 0.5


@dataclass(frozen=True)
class RepositoryInventory:
    root: Path
    module_path: str
    packages: dict[str, PackageFacts]
    graph: dict[str, frozenset[str]]
    reverse_graph: dict[str, frozenset[str]]
    component_graph: dict[str, frozenset[str]]
    component_sccs: tuple[tuple[str, ...], ...]
    history: HistoryFacts
    source_files: int
    source_loc: int


@dataclass
class _PackageBuilder:
    key: str
    path: str
    package_name: str = ""
    source_loc: int = 0
    source_files: int = 0
    imports: set[str] | None = None
    exported_declarations: int = 0
    dynamic_exported_contracts: int = 0
    max_dependency_bag_fields: int = 0
    package_doc_chars: int = 0

    def __post_init__(self) -> None:
        if self.imports is None:
            self.imports = set()


def collect(root: str | Path) -> RepositoryInventory:
    """Collect reusable architecture facts for ``root``.

    Git history collection runs concurrently with source scanning because the
    filesystem and Git object database are independent bottlenecks on WSL.
    """

    repo_root = Path(root).resolve()
    gateway_root = repo_root / "gateway-go"
    internal_root = gateway_root / "internal"
    module_path = _module_path(gateway_root / "go.mod")

    with ThreadPoolExecutor(max_workers=1) as pool:
        history_future = pool.submit(collect_history, repo_root)
        builders, guide_dirs = _scan_sources(
            repo_root=repo_root,
            internal_root=internal_root,
            module_path=module_path,
        )
        history = history_future.result()

    current_keys = set(builders)
    packages: dict[str, PackageFacts] = {}
    for key in sorted(builders):
        item = builders[key]
        guide, guide_strength = _nearest_guide(item.path, guide_dirs)
        imports = tuple(sorted(target for target in item.imports or () if target in current_keys))
        packages[key] = PackageFacts(
            key=key,
            path=item.path,
            package_name=item.package_name,
            source_loc=item.source_loc,
            source_files=item.source_files,
            imports=imports,
            exported_declarations=item.exported_declarations,
            dynamic_exported_contracts=item.dynamic_exported_contracts,
            max_dependency_bag_fields=item.max_dependency_bag_fields,
            package_doc_chars=item.package_doc_chars,
            guide_path=guide,
            guide_strength=guide_strength,
        )

    graph = {key: frozenset(packages[key].imports) for key in sorted(packages)}
    reverse: dict[str, set[str]] = {key: set() for key in packages}
    for source, targets in graph.items():
        for target in targets:
            reverse[target].add(source)
    reverse_graph = {key: frozenset(sorted(reverse[key])) for key in sorted(reverse)}

    component_graph = _collapse_graph(graph)
    component_sccs = tuple(
        tuple(sorted(component))
        for component in sorted(
            (item for item in _strongly_connected_components(component_graph) if len(item) > 1),
            key=lambda item: (-len(item), sorted(item)),
        )
    )
    return RepositoryInventory(
        root=repo_root,
        module_path=module_path,
        packages=packages,
        graph=graph,
        reverse_graph=reverse_graph,
        component_graph=component_graph,
        component_sccs=component_sccs,
        history=history,
        source_files=sum(item.source_files for item in packages.values()),
        source_loc=sum(item.source_loc for item in packages.values()),
    )


def component_for(package_key: str) -> str:
    """Return the stable two-level component owning an internal package."""

    parts = package_key.split("/")
    if parts and parts[0] == "internal":
        parts = parts[1:]
    return "/".join(parts[:2])


def _module_path(go_mod: Path) -> str:
    try:
        for line in go_mod.read_text(encoding="utf-8", errors="replace").splitlines():
            fields = line.split()
            if len(fields) == 2 and fields[0] == "module":
                return fields[1]
    except OSError:
        pass
    return ""


def _scan_sources(
    *, repo_root: Path, internal_root: Path, module_path: str
) -> tuple[dict[str, _PackageBuilder], dict[str, str]]:
    builders: dict[str, _PackageBuilder] = {}
    guide_dirs: dict[str, str] = {}
    if not internal_root.is_dir():
        return builders, guide_dirs

    proc = subprocess.run(
        ["git", "-C", str(repo_root), "ls-files", "-z", "--", "gateway-go/internal"],
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        return builders, guide_dirs
    tracked = sorted(
        Path(raw.decode("utf-8", errors="surrogateescape"))
        for raw in proc.stdout.split(b"\0")
        if raw
    )
    for relative in tracked:
        if any(part in PRUNE_DIRS for part in relative.parts):
            continue
        path = repo_root / relative
        directory = path.parent
        rel_dir = directory.relative_to(repo_root).as_posix()
        filename = path.name
        if filename in GUIDE_NAMES:
            try:
                guide_text = path.read_text(encoding="utf-8", errors="replace")
            except OSError:
                guide_text = ""
            if _substantive_guide(guide_text):
                guide_dirs[rel_dir] = relative.as_posix()
        if not filename.endswith(".go") or filename.endswith(("_test.go", "_gen.go")):
            continue
        try:
            raw = path.read_bytes()
        except OSError:
            continue
        if _is_generated(raw, filename):
            continue
        text = raw.decode("utf-8", errors="replace")
        package_match = _PACKAGE_RE.search(text)
        if package_match is None:
            continue
        package_name = package_match.group(1)
        key = directory.relative_to(internal_root.parent).as_posix()
        item = builders.setdefault(
            key,
            _PackageBuilder(key=key, path=rel_dir, package_name=package_name),
        )
        item.source_files += 1
        item.source_loc += _line_count(raw)
        item.package_name = item.package_name or package_name

        commentless = _remove_comments(text)
        code = _mask_literals(commentless)
        item.imports.update(_internal_imports(commentless, module_path))
        exported, dynamic = _exported_contract_counts(code)
        item.exported_declarations += exported
        item.dynamic_exported_contracts += dynamic
        item.max_dependency_bag_fields = max(
            item.max_dependency_bag_fields,
            _max_dependency_bag_fields(code),
        )
        item.package_doc_chars = max(
            item.package_doc_chars,
            _package_doc_chars(text, package_match.start(), package_name),
        )
    return builders, guide_dirs


def _is_generated(raw: bytes, filename: str) -> bool:
    if filename.endswith("_gen.go"):
        return True
    head = b"\n".join(raw.splitlines()[:5]).upper()
    return b"DO NOT EDIT" in head or b"@GENERATED" in head


def _line_count(raw: bytes) -> int:
    if not raw:
        return 0
    return raw.count(b"\n") + (0 if raw.endswith(b"\n") else 1)


def _internal_imports(text: str, module_path: str) -> set[str]:
    if not module_path:
        return set()
    imports: set[str] = set()
    for block in _IMPORT_BLOCK_RE.findall(text):
        imports.update(_QUOTED_IMPORT_RE.findall(block))
    imports.update(_IMPORT_SINGLE_RE.findall(text))
    prefix = module_path.rstrip("/") + "/internal/"
    return {"internal/" + item[len(prefix) :] for item in imports if item.startswith(prefix)}


def _remove_comments(text: str) -> str:
    """Replace Go comments with spaces while preserving strings and newlines."""

    out = list(text)
    i = 0
    state = "code"
    while i < len(text):
        ch = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""
        if state == "code":
            if ch == "/" and nxt == "/":
                out[i] = out[i + 1] = " "
                i += 2
                state = "line-comment"
                continue
            if ch == "/" and nxt == "*":
                out[i] = out[i + 1] = " "
                i += 2
                state = "block-comment"
                continue
            if ch == '"':
                state = "string"
            elif ch == "'":
                state = "rune"
            elif ch == "`":
                state = "raw"
        elif state == "line-comment":
            if ch == "\n":
                state = "code"
            else:
                out[i] = " "
        elif state == "block-comment":
            if ch == "*" and nxt == "/":
                out[i] = out[i + 1] = " "
                i += 2
                state = "code"
                continue
            if ch != "\n":
                out[i] = " "
        elif state in {"string", "rune"}:
            quote = '"' if state == "string" else "'"
            if ch == "\\":
                i += 2
                continue
            if ch == quote:
                state = "code"
        elif state == "raw" and ch == "`":
            state = "code"
        i += 1
    return "".join(out)


def _mask_literals(text: str) -> str:
    """Mask Go string/rune literals so braces in data cannot confuse scans."""

    out = list(text)
    i = 0
    state = "code"
    while i < len(text):
        ch = text[i]
        if state == "code":
            if ch == '"':
                out[i] = " "
                state = "string"
            elif ch == "'":
                out[i] = " "
                state = "rune"
            elif ch == "`":
                out[i] = " "
                state = "raw"
        elif state in {"string", "rune"}:
            quote = '"' if state == "string" else "'"
            if ch == "\\":
                out[i] = " "
                if i + 1 < len(text):
                    out[i + 1] = " "
                i += 2
                continue
            out[i] = " "
            if ch == quote:
                state = "code"
        elif state == "raw":
            out[i] = "\n" if ch == "\n" else " "
            if ch == "`":
                state = "code"
        i += 1
    return "".join(out)


def _package_doc_chars(text: str, package_pos: int, package_name: str) -> int:
    prefix = text[:package_pos]
    line_matches = list(re.finditer(r"(?m)^//\s?(.*)$", prefix))
    best = ""
    for index, match in enumerate(line_matches):
        if not re.match(rf"Package\s+{re.escape(package_name)}\b", match.group(1)):
            continue
        lines = [match.group(1)]
        end = match.end()
        for following in line_matches[index + 1 :]:
            gap = prefix[end : following.start()]
            if gap.strip():
                break
            lines.append(following.group(1))
            end = following.end()
        best = max(best, " ".join(lines), key=len)
    for block in re.findall(r"(?s)/\*(.*?)\*/", prefix):
        cleaned = " ".join(part.strip(" *\t") for part in block.splitlines()).strip()
        if re.match(rf"Package\s+{re.escape(package_name)}\b", cleaned):
            best = max(best, cleaned, key=len)
    return len(" ".join(best.split()))


def _exported_contract_counts(code: str) -> tuple[int, int]:
    exported = 0
    dynamic = 0

    for match in _EXPORTED_FUNC_RE.finditer(code):
        exported += 1
        end = _function_signature_end(code, match.start())
        signature = code[match.start() : end]
        dynamic += bool(_DYNAMIC_TYPE_RE.search(signature))

    for match in _DECLARED_TYPE_RE.finditer(code):
        name = match.group(1)
        if not name[0].isupper():
            continue
        exported += 1
        end = _type_declaration_end(code, match.end())
        declaration = code[match.start() : end]
        dynamic += bool(_DYNAMIC_TYPE_RE.search(declaration))

    # Direct const/var declarations not already counted as type/function APIs.
    for match in _EXPORTED_NAMED_RE.finditer(code):
        if code.startswith("type", match.start()):
            continue
        exported += 1
        line_end = code.find("\n", match.end())
        if line_end < 0:
            line_end = len(code)
        dynamic += bool(_DYNAMIC_TYPE_RE.search(code[match.start() : line_end]))

    for match in _GROUP_DECL_RE.finditer(code):
        open_pos = code.find("(", match.start(), match.end() + 1)
        close_pos = _balanced_end(code, open_pos, "(", ")") if open_pos >= 0 else -1
        if close_pos <= open_pos:
            continue
        group_exported, group_dynamic = _group_export_counts(
            code[open_pos + 1 : close_pos - 1]
        )
        exported += group_exported
        dynamic += group_dynamic
    return exported, dynamic


def _group_export_counts(body: str) -> tuple[int, int]:
    """Count top-level exported names and dynamic types in a declaration group."""

    count = 0
    dynamic = 0
    paren_depth = brace_depth = bracket_depth = 0
    for line in body.splitlines():
        if paren_depth == 0 and brace_depth == 0 and bracket_depth == 0:
            if _GROUP_EXPORTED_RE.match(line):
                count += 1
                dynamic += bool(_DYNAMIC_TYPE_RE.search(line))
        paren_depth += line.count("(") - line.count(")")
        brace_depth += line.count("{") - line.count("}")
        bracket_depth += line.count("[") - line.count("]")
    return count, dynamic


def _function_signature_end(code: str, start: int) -> int:
    paren = bracket = 0
    for pos in range(start, min(len(code), start + 5000)):
        ch = code[pos]
        if ch == "(":
            paren += 1
        elif ch == ")":
            paren = max(0, paren - 1)
        elif ch == "[":
            bracket += 1
        elif ch == "]":
            bracket = max(0, bracket - 1)
        elif ch == "{" and paren == 0 and bracket == 0:
            return pos
    newline = code.find("\n", start)
    return len(code) if newline < 0 else newline


def _type_declaration_end(code: str, after_name: int) -> int:
    newline = code.find("\n", after_name)
    line_end = len(code) if newline < 0 else newline
    open_pos = code.find("{", after_name, line_end + 1)
    if open_pos < 0:
        return line_end
    close_pos = _balanced_end(code, open_pos, "{", "}")
    return close_pos if close_pos > open_pos else line_end


def _balanced_end(code: str, start: int, opening: str, closing: str) -> int:
    depth = 0
    for pos in range(start, len(code)):
        if code[pos] == opening:
            depth += 1
        elif code[pos] == closing:
            depth -= 1
            if depth == 0:
                return pos + 1
    return -1


def _max_dependency_bag_fields(code: str) -> int:
    maximum = 0
    for match in _DECLARED_TYPE_RE.finditer(code):
        name = match.group(1)
        if not _DEPENDENCY_BAG_RE.search(name):
            continue
        line_end = code.find("\n", match.end())
        if line_end < 0:
            line_end = len(code)
        suffix = code[match.end() : line_end]
        struct_pos = suffix.find("struct")
        if struct_pos < 0:
            continue
        open_pos = code.find("{", match.end() + struct_pos, line_end + 1)
        if open_pos < 0:
            continue
        close_pos = _balanced_end(code, open_pos, "{", "}")
        if close_pos <= open_pos:
            continue
        maximum = max(maximum, _struct_field_count(code[open_pos + 1 : close_pos - 1]))
    return maximum


def _struct_field_count(body: str) -> int:
    count = 0
    brace_depth = 0
    paren_depth = 0
    for line in body.splitlines():
        at_top = brace_depth == 0 and paren_depth == 0
        stripped = line.strip()
        if at_top and stripped and not stripped.startswith(("}", ")")):
            # Named or embedded fields. Method declarations do not occur in structs.
            if re.match(
                r"^(?:[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*\s+|"
                r"\*?[A-Za-z_][A-Za-z0-9_.]*(?:\[[^]]+\])?\s*$)",
                stripped,
            ):
                count += 1
        brace_depth += line.count("{") - line.count("}")
        paren_depth += line.count("(") - line.count(")")
    return count


def _substantive_guide(text: str) -> bool:
    """Reject placeholder guides that cannot orient a maintainer."""
    if len("".join(text.split())) < 300:
        return False
    if len(re.findall(r"(?m)^#{1,4}\s+\S", text)) < 2:
        return False
    signals = re.findall(
        r"\b(?:entry|purpose|responsib|depend|invariant|test|verify|contract|"
        r"boundary|architecture|owner|change)\w*\b|"
        r"(?:진입|책임|의존|불변|검증|계약|경계|구조|변경)",
        text,
        flags=re.IGNORECASE,
    )
    return len({signal.casefold() for signal in signals}) >= 3


def _nearest_guide(package_path: str, guide_dirs: dict[str, str]) -> tuple[str | None, float]:
    path = Path(package_path)
    boundary = Path("gateway-go/internal")
    distance = 0
    while path != boundary.parent:
        key = path.as_posix()
        if key in guide_dirs:
            strengths = (1.0, 0.8, 0.6, 0.4)
            strength = strengths[distance] if distance < len(strengths) else 0.25
            return guide_dirs[key], strength
        if path == boundary:
            break
        path = path.parent
        distance += 1
    return None, 0.0


def _collapse_graph(graph: dict[str, frozenset[str]]) -> dict[str, frozenset[str]]:
    collapsed: dict[str, set[str]] = defaultdict(set)
    for source, targets in graph.items():
        source_component = component_for(source)
        collapsed[source_component]
        for target in targets:
            target_component = component_for(target)
            if source_component != target_component:
                collapsed[source_component].add(target_component)
    return {key: frozenset(sorted(collapsed[key])) for key in sorted(collapsed)}


def _strongly_connected_components(
    graph: dict[str, frozenset[str]],
) -> list[tuple[str, ...]]:
    index = 0
    stack: list[str] = []
    on_stack: set[str] = set()
    indexes: dict[str, int] = {}
    lowlinks: dict[str, int] = {}
    result: list[tuple[str, ...]] = []

    def visit(node: str) -> None:
        nonlocal index
        indexes[node] = index
        lowlinks[node] = index
        index += 1
        stack.append(node)
        on_stack.add(node)
        for target in sorted(graph.get(node, ())):
            if target not in indexes:
                visit(target)
                lowlinks[node] = min(lowlinks[node], lowlinks[target])
            elif target in on_stack:
                lowlinks[node] = min(lowlinks[node], indexes[target])
        if lowlinks[node] != indexes[node]:
            return
        component: list[str] = []
        while stack:
            current = stack.pop()
            on_stack.remove(current)
            component.append(current)
            if current == node:
                break
        result.append(tuple(sorted(component)))

    for node in sorted(graph):
        if node not in indexes:
            visit(node)
    return result

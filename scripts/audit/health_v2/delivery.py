"""CI parity and executable-evidence scoring for Health Bench 2.0."""

from __future__ import annotations

import ast
import fnmatch
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

from .model import Evidence, Finding, Pillar, PRODUCT_LANE_IMPACT, stable_id


@dataclass(frozen=True)
class _Capability:
    lane: str
    name: str
    present: bool
    importance: float
    evidence: str
    remediation: str


def _tracked_files(root: Path) -> frozenset[str] | None:
    try:
        process = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z"],
            capture_output=True,
            check=False,
            timeout=8,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    if process.returncode != 0:
        return None
    return frozenset(
        raw.decode("utf-8", errors="surrogateescape")
        for raw in process.stdout.split(b"\0")
        if raw
    )


def _without_shell_comment(line: str) -> str:
    quote = ""
    for index, char in enumerate(line):
        if char in {'"', "'"}:
            quote = "" if quote == char else char if not quote else quote
        if char == "#" and not quote and (index == 0 or line[index - 1].isspace()):
            return line[:index].rstrip()
    return line.rstrip()


def _nonblocking_yaml_ranges(lines: list[str]) -> list[tuple[int, int]]:
    """Locate statically disabled or non-blocking GitHub job/step blocks.

    Dynamic expressions remain potentially executable and therefore stay in
    scope; only literal opt-outs can safely be rejected as gate evidence.
    """
    ranges: list[tuple[int, int]] = []
    opt_out = re.compile(
        r"^(\s*)(?P<list>-\s*)?(?:"
        r"if:\s*(?:false|\$\{\{\s*false\s*\}\})|"
        r"continue-on-error:\s*(?:true|\$\{\{\s*true\s*\}\}))\s*$",
        re.IGNORECASE,
    )
    for index, raw in enumerate(lines):
        match = opt_out.match(_without_shell_comment(raw))
        if not match:
            continue
        condition_indent = len(match.group(1))
        if match.group("list"):
            parent_indent = condition_indent
            start = index
        else:
            start = index
            parent_indent = max(0, condition_indent - 1)
            for previous in range(index - 1, -1, -1):
                candidate = lines[previous]
                if not candidate.strip():
                    continue
                indent = len(candidate) - len(candidate.lstrip())
                if indent < condition_indent:
                    start = previous
                    parent_indent = indent
                    break
        end = len(lines)
        for following in range(index + 1, len(lines)):
            candidate = lines[following]
            if not candidate.strip():
                continue
            indent = len(candidate) - len(candidate.lstrip())
            if indent < parent_indent or (
                indent == parent_indent and following != start
            ):
                end = following
                break
        ranges.append((start, end))
    return ranges


def _workflow_gate_scope(text: str) -> bool:
    """Whether the workflow can gate a pull request or a push to main."""
    lines = [_without_shell_comment(line) for line in text.splitlines()]
    event_start = next(
        (
            index
            for index, line in enumerate(lines)
            if re.match(r"^(?:on|['\"]on['\"]):(?:\s|$)", line)
        ),
        None,
    )
    if event_start is None:
        return False
    first = lines[event_start].split(":", 1)[1].strip()
    if first:
        events = {item.casefold() for item in re.findall(r"[A-Za-z_]+", first)}
        if "pull_request" in events:
            return _event_gates_main([first])
        if "push" not in events:
            return False
        return _event_gates_main([first])
    event_lines: list[str] = []
    for line in lines[event_start + 1 :]:
        if line.strip() and not line[0].isspace():
            break
        event_lines.append(line)
    pull_request = _event_section(event_lines, "pull_request")
    if pull_request is not None and _event_gates_main(pull_request):
        return True
    push = _event_section(event_lines, "push")
    return push is not None and _event_gates_main(push)


def _event_section(lines: list[str], event: str) -> list[str] | None:
    start = next(
        (
            index
            for index, line in enumerate(lines)
            if re.match(rf"^(\s+){re.escape(event)}\s*:", line)
        ),
        None,
    )
    if start is None:
        return None
    indent = len(lines[start]) - len(lines[start].lstrip())
    section = [lines[start]]
    for line in lines[start + 1 :]:
        if line.strip() and len(line) - len(line.lstrip()) <= indent:
            break
        section.append(line)
    return section


def _branch_patterns(lines: list[str], key: str) -> list[str] | None:
    inline = re.search(
        rf"(?:^|[{{,\s]){re.escape(key)}\s*:\s*\[([^]]*)\]",
        "\n".join(lines),
    )
    if inline:
        return [
            item.strip().strip("'\"")
            for item in inline.group(1).split(",")
            if item.strip().strip("'\"")
        ]
    for index, line in enumerate(lines):
        match = re.match(rf"^(\s*){re.escape(key)}\s*:\s*(.*?)\s*$", line)
        if not match:
            continue
        if match.group(2):
            return [match.group(2).strip().strip("'\"")]
        indent = len(match.group(1))
        values: list[str] = []
        for child in lines[index + 1 :]:
            if child.strip() and len(child) - len(child.lstrip()) <= indent:
                break
            item = re.match(r"^\s*-\s*(.*?)\s*$", child)
            if item and item.group(1):
                values.append(item.group(1).strip().strip("'\""))
        return values
    return None


def _main_branch_enabled(lines: list[str]) -> bool:
    included = _branch_patterns(lines, "branches")
    ignored = _branch_patterns(lines, "branches-ignore")
    if included is not None:
        enabled = False
        for pattern in included:
            negative = pattern.startswith("!")
            candidate = pattern[1:] if negative else pattern
            if fnmatch.fnmatchcase("main", candidate):
                enabled = not negative
        return enabled
    if ignored is not None:
        return not any(fnmatch.fnmatchcase("main", pattern) for pattern in ignored)
    return True


def _event_gates_main(lines: list[str]) -> bool:
    if not _main_branch_enabled(lines):
        return False
    paths = _branch_patterns(lines, "paths")
    if paths is None:
        return True
    positive = [pattern for pattern in paths if not pattern.startswith("!")]
    return any(
        not (
            pattern.lstrip("./").startswith(("docs/", ".github/"))
            or pattern.casefold().endswith((".md", ".mdx"))
        )
        for pattern in positive
    )


def _nonblocking_command(command: str) -> bool:
    return bool(
        re.search(
            r"(?:\|\|\s*(?:true|:)(?:\s|$)|;\s*(?:true|:)\s*$|"
            r"\bset\s+\+e\b|\bexit\s+0\b)",
            command,
        )
    )


def _gate_shell_line(command: str) -> str:
    stripped = command.strip()
    if not stripped or re.match(r"^(?:if\s+false\b|false\s*&&|true\s*\|\|)", stripped):
        return ""
    try:
        tokens = shlex.split(stripped)
    except ValueError:
        return ""
    if not tokens:
        return ""
    index = 0
    if tokens[index] == "env":
        index += 1
    while index < len(tokens) and re.match(r"^[A-Za-z_]\w*=", tokens[index]):
        index += 1
    if index < len(tokens) and tokens[index] == "command":
        index += 1
    if index >= len(tokens):
        return ""
    executable = Path(tokens[index]).name
    if executable in {"echo", "printf", "export", "cat"}:
        return ""
    if executable in {"bash", "sh"} and "-c" in tokens[index + 1 :]:
        command_index = tokens.index("-c", index + 1) + 1
        return _gate_shell_line(tokens[command_index]) if command_index < len(tokens) else ""
    if executable.startswith("python") and "-c" in tokens[index + 1 :]:
        return executable
    return " ".join(tokens[index:])


def _gate_shell_block(lines: list[str]) -> list[str]:
    commands: list[str] = []
    logical_lines: list[str] = []
    pending = ""
    for line in lines:
        piece = line.rstrip()
        if piece.endswith("\\"):
            pending += piece[:-1] + " "
            continue
        logical_lines.append(pending + piece)
        pending = ""
    if pending:
        logical_lines.append(pending)
    literal_false_depth = 0
    for line in logical_lines:
        stripped = line.strip()
        if re.match(r"^if\s+false\b", stripped):
            if not re.search(r"\bfi\s*$", stripped):
                literal_false_depth += 1
            continue
        if literal_false_depth:
            if re.match(r"^if\b.*\bthen\b", stripped):
                literal_false_depth += 1
            if re.match(r"^fi(?:\s*;)?$", stripped):
                literal_false_depth -= 1
            continue
        normalized = _gate_shell_line(line)
        if normalized:
            commands.append(normalized)
    return commands


def _executable_yaml(text: str) -> str:
    """Return only run/uses values, excluding names and comments."""
    lines = text.splitlines()
    nonblocking = _nonblocking_yaml_ranges(lines)
    commands: list[str] = []
    index = 0
    field = re.compile(r"^(\s*)(?:-\s*)?(run|uses):\s*(.*)$")
    while index < len(lines):
        match = field.match(lines[index])
        if not match:
            index += 1
            continue
        if any(start <= index < end for start, end in nonblocking):
            index += 1
            continue
        indent = len(match.group(1))
        kind = match.group(2)
        value = _without_shell_comment(match.group(3)).strip()
        index += 1
        if value in {"|", ">", "|-", ">-", "|+", ">+"}:
            block: list[str] = []
            while index < len(lines):
                raw = lines[index]
                if raw.strip() and len(raw) - len(raw.lstrip()) <= indent:
                    break
                cleaned = _without_shell_comment(raw.strip())
                if cleaned:
                    block.append(cleaned)
                index += 1
            if not _nonblocking_command("\n".join(block)):
                commands.extend(_gate_shell_block(block))
        elif value and not _nonblocking_command(value):
            normalized = value if kind == "uses" else _gate_shell_line(value)
            if normalized:
                commands.append(normalized)
    return "\n".join(commands)


def _expand_local_references(
    root: Path, executable: str, tracked: frozenset[str]
) -> str:
    expanded = [executable]
    seen: set[Path] = set()
    pending = [match.group(1) for match in re.finditer(r"(?:^|\s)(\./[^\s]+)", executable)]
    while pending:
        raw = pending.pop()
        target = (root / raw.removeprefix("./")).resolve()
        candidates = [target]
        if target.is_dir():
            candidates = [target / "action.yml", target / "action.yaml"]
        local = next(
            (
                path
                for path in candidates
                if path.is_file()
                and root.resolve() in path.parents
                and path.relative_to(root.resolve()).as_posix() in tracked
            ),
            None,
        )
        if local is None or local in seen or root.resolve() not in local.parents:
            continue
        seen.add(local)
        nested = _executable_yaml(local.read_text(encoding="utf-8", errors="replace"))
        expanded.append(nested)
        pending.extend(match.group(1) for match in re.finditer(r"(?:^|\s)(\./[^\s]+)", nested))
    return "\n".join(expanded)


def _make_recipes(
    root: Path, executable: str, tracked: frozenset[str]
) -> str:
    if "Makefile" not in tracked:
        return executable
    try:
        lines = (root / "Makefile").read_text(encoding="utf-8", errors="replace").splitlines()
    except OSError:
        return executable
    recipes: dict[str, list[str]] = {}
    current: list[str] = []
    for line in lines:
        target = re.match(r"^([A-Za-z0-9_./-]+)\s*:(?:\s|$)", line)
        if target:
            current = recipes.setdefault(target.group(1), [])
        elif line.startswith("\t") and current is not None:
            recipe = line.lstrip("\t")
            if recipe.lstrip().startswith("-"):
                continue
            cleaned = _without_shell_comment(recipe.lstrip().lstrip("@+"))
            if not _nonblocking_command(cleaned):
                normalized = _gate_shell_line(cleaned)
                if normalized:
                    current.append(normalized)
        elif line and not line[0].isspace():
            current = []
    expanded = [executable]
    pending = re.findall(r"(?m)\bmake\s+([A-Za-z0-9_./-]+)", executable)
    seen: set[str] = set()
    while pending:
        target = pending.pop()
        if target in seen:
            continue
        seen.add(target)
        recipe = "\n".join(line for line in recipes.get(target, ()) if line)
        if recipe:
            expanded.append(recipe)
            pending.extend(re.findall(r"(?m)\b(?:\$\(MAKE\)|make)\s+([A-Za-z0-9_./-]+)", recipe))
    return "\n".join(expanded)


def _read_workflows(
    root: Path, tracked: frozenset[str] | None = None
) -> dict[str, str]:
    tracked = _tracked_files(root) if tracked is None else tracked
    if tracked is None:
        return {}
    workflows: dict[str, str] = {}
    paths = sorted(
        root / relative
        for relative in tracked
        if Path(relative).parent == Path(".github/workflows")
        and Path(relative).suffix in {".yml", ".yaml"}
    )
    for path in paths:
        if not path.is_file():
            continue
        try:
            raw = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            # A tracked workflow may disappear between inventory and read in a
            # concurrently changing checkout. It cannot count as gate evidence.
            continue
        if not _workflow_gate_scope(raw):
            continue
        executable = _expand_local_references(
            root, _executable_yaml(raw), tracked
        )
        workflows[path.name] = _make_recipes(root, executable, tracked)
    return workflows


def _has(text: str, *patterns: str) -> bool:
    return any(re.search(pattern, text, re.IGNORECASE | re.MULTILINE) for pattern in patterns)


def _python_needs_lock(root: Path, tracked: frozenset[str]) -> bool:
    """Whether tracked Python imports third-party packages.

    A stdlib-only audit must not be penalized for lacking a fake lock file. A
    lock becomes quality evidence only when third-party imports actually exist.
    """
    files = sorted(
        root / relative
        for relative in tracked
        if relative.startswith("scripts/")
        and relative.endswith(".py")
        and (root / relative).is_file()
    )
    local_modules = {path.stem for path in files}
    local_modules.update(
        part
        for relative in tracked
        if relative.startswith("scripts/") and (root / relative).is_file()
        for part in Path(relative).parts[1:-1]
    )
    for path in files:
        try:
            tree = ast.parse(path.read_text(encoding="utf-8", errors="replace"))
        except (OSError, SyntaxError):
            continue
        for node in ast.walk(tree):
            module = ""
            if isinstance(node, ast.Import) and node.names:
                module = node.names[0].name.split(".", 1)[0]
            elif isinstance(node, ast.ImportFrom) and node.module:
                module = node.module.split(".", 1)[0]
            if module and module not in sys.stdlib_module_names and module not in local_modules:
                return True
    return False


def _capabilities(
    root: Path,
    workflows: dict[str, str],
    tracked: frozenset[str] | None = None,
) -> list[_Capability]:
    tracked = _tracked_files(root) if tracked is None else tracked
    if tracked is None:
        return []

    def present(relative: str) -> bool:
        return relative in tracked and (root / relative).is_file()

    all_ci = "\n".join(workflows.values())
    caps: list[_Capability] = []

    def add(
        lane: str,
        name: str,
        present: bool,
        importance: float,
        evidence: str,
        remediation: str,
    ) -> None:
        caps.append(_Capability(lane, name, present, importance, evidence, remediation))

    # Formatting, result artifacts, and universal coverage quotas are useful
    # conveniences but not direct failure-prevention evidence, so they do not
    # affect this pillar. Each lane carries only capabilities relevant to its
    # role in Deneb.
    add("go", "static-analysis", _has(all_ci, r"go vet") and _has(all_ci, r"golangci"), 2, "vet + lint", "Run both vet and the pinned Go linter.")
    add("go", "build", _has(all_ci, r"go build"), 2, "production build", "Build all packages and the gateway binary.")
    add("go", "fresh-unit", _has(all_ci, r"go test[^\n]*-count=1"), 3, "uncached unit tests", "Run uncached Go tests.")
    add("go", "integration", _has(all_ci, r"tags=integration", r"gateway e2e"), 2, "gateway integration", "Exercise the gateway process boundary.")
    add("go", "race", _has(all_ci, r"go test[^\n]*-race"), 1, "race detector", "Run the race detector for the concurrent gateway.")
    add("go", "dependency-lock", present("gateway-go/go.sum"), 1, "go.sum", "Commit the Go dependency lock.")

    full_desktop = _has(all_ci, r"gradlew\s+desktopTest[^\n]*$") and "--tests" not in all_ci
    add("kotlin", "static-analysis", _has(all_ci, r"detekt"), 2, "detekt", "Run Kotlin bug-focused static analysis.")
    add("kotlin", "build", _has(all_ci, r"compileAndroid|assemble"), 2, "Android compile", "Compile every supported native target.")
    add("kotlin", "full-unit", full_desktop, 3, "full desktopTest", "Run the full deterministic client test suite or explicit complete shards.")
    add("kotlin", "integration", _has(all_ci, r"gradlew[^\n]*(?:integrationTest|connected.*Test)"), 2, "client integration", "Exercise a native client-to-gateway or Android boundary.")
    add("kotlin", "toolchain-lock", present("client-android/app/gradle/wrapper/gradle-wrapper.properties"), 1, "Gradle wrapper", "Commit and verify the Gradle wrapper.")

    add("typescript", "static-analysis", _has(all_ci, r"typecheck") and _has(all_ci, r"pnpm run lint"), 2, "typecheck + lint", "Run TypeScript and semantic lint checks.")
    add("typescript", "build", _has(all_ci, r"pnpm run build"), 2, "production build", "Build the desktop client.")
    add("typescript", "fresh-unit", _has(all_ci, r"pnpm run test"), 3, "desktop unit tests", "Run desktop tests without cached result reuse.")
    add("typescript", "integration", _has(all_ci, r"\b(?:playwright|cypress)\b|(?:pnpm|npx)[^\n]*integration"), 2, "desktop integration", "Exercise a user-visible client flow or gateway contract.")
    add("typescript", "dependency-lock", present("andromeda/pnpm-lock.yaml"), 1, "pnpm-lock.yaml", "Commit the pnpm lock.")

    python_test_files = [
        relative
        for relative in tracked
        if relative.startswith("scripts/")
        and (
            Path(relative).name.startswith("test_")
            or Path(relative).name.endswith("_test.py")
            or "tests" in Path(relative).parts
        )
        and (root / relative).is_file()
    ]
    python_full = _has(all_ci, r"pytest(?:\s|$)") or any(
        "-p" not in line or bool(re.search(r"test[_*]?\\?\*", line))
        for line in all_ci.splitlines()
        if "unittest discover" in line
    )
    add("python", "static-analysis", _has(all_ci, r"ruff check|mypy|pyright"), 2, "Python static analysis", "Run static analysis for operational Python.")
    add("python", "full-unit", python_full and bool(python_test_files), 4, "all Python tests", "Discover every Python test suite in CI.")
    if _python_needs_lock(root, tracked):
        add("python", "dependency-lock", any(present(name) for name in ("uv.lock", "poetry.lock", "requirements.lock")), 2, "third-party dependency lock", "Pin imported third-party packages and the interpreter.")

    shell_files = [
        root / relative
        for relative in tracked
        if relative.startswith("scripts/")
        and relative.endswith(".sh")
        and (root / relative).is_file()
    ]
    critical_shell = [
        path
        for path in shell_files
        if "deploy" in path.parts
        or re.search(
            r"(?:deploy|install|release|publish|provision|firewall|rotate|topology|migrat|backup|restore|start|stop|health|smoke|ci[-_])",
            path.name,
            re.IGNORECASE,
        )
    ]
    if critical_shell:
        add("shell", "static-analysis", _has(all_ci, r"shellcheck"), 2, "ShellCheck", "Run ShellCheck across operational scripts.")
        add("shell", "behavior", _has(all_ci, r"bats|shellspec|test_topology_parity_shell"), 3, "shell behavior tests", "Run deterministic tests for deployment and topology scripts.")

    if present("andromeda/src-tauri/Cargo.toml"):
        add("rust", "static-analysis", _has(all_ci, r"cargo clippy"), 2, "cargo clippy", "Run clippy with warnings denied.")
        add("rust", "build", _has(all_ci, r"cargo (?:build|check)"), 2, "cargo build/check", "Compile the Tauri crate.")
        add("rust", "fresh-unit", _has(all_ci, r"cargo test"), 2, "cargo test", "Run Rust behavior tests.")
        add("rust", "dependency-lock", present("andromeda/src-tauri/Cargo.lock"), 1, "Cargo.lock", "Commit the Cargo lock.")
    return caps


def evaluate(root: Path) -> tuple[list[Pillar], list[Evidence]]:
    tracked = _tracked_files(root)
    if tracked is None:
        return [], [
            Evidence(
                "ci-workflows",
                "unavailable",
                "git tracked-file inventory unavailable",
                required=True,
            )
        ]
    workflows = _read_workflows(root, tracked)
    if not workflows:
        return [], [Evidence("ci-workflows", "unavailable", "no workflow files found", required=True)]
    caps = _capabilities(root, workflows, tracked)
    by_lane: dict[str, list[_Capability]] = {}
    for capability in caps:
        by_lane.setdefault(capability.lane, []).append(capability)
    unknown_lanes = sorted(
        lane for lane in by_lane if PRODUCT_LANE_IMPACT.get(lane, 0.0) <= 0
    )
    if unknown_lanes:
        raise ValueError(f"delivery capabilities lack product impact: {', '.join(unknown_lanes)}")
    lane_scores = {
        lane: 100.0
        * sum(item.importance for item in items if item.present)
        / sum(item.importance for item in items)
        for lane, items in sorted(by_lane.items())
    }
    active_impact = {
        lane: PRODUCT_LANE_IMPACT.get(lane, 0.0) for lane in lane_scores
    }
    impact_total = sum(active_impact.values())
    normalized_impact = {
        lane: impact / impact_total if impact_total else 1.0 / len(lane_scores)
        for lane, impact in active_impact.items()
    }
    score = sum(lane_scores[lane] * normalized_impact[lane] for lane in lane_scores)
    findings: list[Finding] = []
    for lane, items in sorted(by_lane.items(), key=lambda item: (lane_scores[item[0]], item[0])):
        missing = [item for item in items if not item.present]
        if not missing:
            continue
        impact = normalized_impact[lane]
        if impact >= 0.20 and lane_scores[lane] < 60:
            severity = "high"
        elif impact >= 0.10 and lane_scores[lane] < 75:
            severity = "medium"
        else:
            severity = "low"
        names = ", ".join(item.name for item in missing)
        present_weight = sum(item.importance for item in items if item.present)
        total_weight = sum(item.importance for item in items)
        findings.append(
            Finding(
                id=stable_id("ci-parity", lane),
                pillar="delivery-confidence",
                severity=severity,
                path=".github/workflows",
                evidence=f"{lane}: {present_weight:g}/{total_weight:g} prevention weight; missing {names}",
                why="A product-relevant code lane can change without the failure-prevention evidence its role requires.",
                remediation=" ".join(item.remediation for item in missing[:3]),
                verify="make health-v2 && inspect the lane's required CI checks",
                priority=min(100.0, (100.0 - lane_scores[lane]) * (0.15 + 1.7 * impact)),
            )
        )
    pillar = Pillar(
        id="delivery-confidence",
        title="Delivery confidence by product impact",
        weight=8,
        score=score,
        intent="Require role-appropriate executable evidence weighted by each language lane's product impact.",
        metrics={
            "lane_scores": {key: round(value, 1) for key, value in lane_scores.items()},
            "lane_impact": {key: round(value, 3) for key, value in normalized_impact.items()},
            "capabilities_present": sum(item.present for item in caps),
            "capabilities_total": len(caps),
            "scored_capabilities": {
                lane: {item.name: item.importance for item in items}
                for lane, items in sorted(by_lane.items())
            },
        },
        findings=findings,
    )
    evidence = [
        Evidence("ci-workflows", "measured", f"inspected {len(workflows)} workflow files", required=True),
        Evidence("executed-quality-gates", "unavailable", "fast profile inspects gate coverage but does not execute language toolchains"),
    ]
    return [pillar], evidence

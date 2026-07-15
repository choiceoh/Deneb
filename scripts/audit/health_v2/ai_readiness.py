"""Evidence that an AI maintainer can locate, change, and verify Go code safely."""

from __future__ import annotations

import math
import re
import shlex
import subprocess
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path

from .inventory import PackageFacts, RepositoryInventory, _mask_literals, _remove_comments
from .model import PRODUCT_LANE_IMPACT, Finding, Pillar, stable_id


VERIFY = (
    "Run `python3 scripts/audit/codebase-health-v2.py --json` and confirm the "
    "cross-checked AI-maintenance signal is present."
)
MEASURED_PRODUCT_LANES = ("go",)
ACTIVE_PRODUCT_LANES = ("go", "kotlin", "typescript", "python")
LANE_COVERAGE = sum(PRODUCT_LANE_IMPACT[x] for x in MEASURED_PRODUCT_LANES) / sum(
    PRODUCT_LANE_IMPACT[x] for x in ACTIVE_PRODUCT_LANES)

_DECL_RE = re.compile(
    r"(?m)^(?:func\s+(?:\([^\n)]*\)\s*)?|type\s+|var\s+|const\s+)"
    r"([A-Za-z_][A-Za-z0-9_]*)"
)
_FILE_REF_RE = re.compile(r"(?<![A-Za-z0-9_])(?:gateway-go/)?[A-Za-z0-9_./-]+\.go\b")
_WORD_RE = re.compile(r"\b[A-Za-z_][A-Za-z0-9_]*\b")
_USED_SYMBOL_RE = re.compile(r"\b([A-Za-z_][A-Za-z0-9_]*)\s*(?=[({])")
_COMMAND_RE = re.compile(
    r"(?m)^\s*(?:\$\s*)?((?:cd\s+[^\n]+?\s*&&\s*)?"
    r"(?:go\s+test|make|python\d*|\./[A-Za-z0-9_./-]+|scripts/[A-Za-z0-9_./-]+|"
    r"pnpm|gradle|\./gradlew)\b[^\n`]*)"
)
_INLINE_COMMAND_RE = re.compile(
    r"`((?:cd\s+[^`]+?\s*&&\s*)?(?:go\s+test|make|python\d*|\./|scripts/|"
    r"pnpm|gradle|\./gradlew)[^`]*)`"
)
_DEPENDENCY_RE = re.compile(
    r"\b(?:depend|dependency|import|layer|boundary|direction|port)\w*\b|"
    r"(?:의존|방향|계층|경계|포트)",
    re.IGNORECASE,
)
_INVARIANT_RE = re.compile(
    r"\b(?:invariant|must|never|only|forbid|order|single\s+source)\w*\b|"
    r"(?:불변|금지|반드시|항상|유일|단일|순서)",
    re.IGNORECASE,
)
# Local change scope section: AI-facing change-boundary docs (neighbors,
# forbidden surfaces, focused verify). Complements git multipackage confinement.
_LOCAL_SCOPE_HEADING_RE = re.compile(
    r"(?im)^#{2,3}\s+(?:Local change scope|로컬 변경 범위)\b"
)
_SCOPE_NEIGHBOR_RE = re.compile(
    r"(?:함께 바꿔|neighbor|neighbours|ok to change|함께 변경)"
    r".{0,200}?(?:internal/|gateway-go/internal/|`[A-Za-z0-9_./*-]+`)|"
    r"(?:internal/|gateway-go/internal/|`[A-Za-z0-9_./*-]+`)"
    r".{0,200}?(?:함께 바꿔|neighbor|neighbours|ok to change|함께 변경)",
    re.IGNORECASE | re.DOTALL,
)
_SCOPE_FORBIDDEN_RE = re.compile(
    r"(?:건드리지\s*말|금지|forbidden|do not (?:edit|touch|import)|import하지\s*말)",
    re.IGNORECASE,
)
_GENERATED_RE = re.compile(r"(?:DO NOT EDIT|@generated)", re.IGNORECASE)
_SOURCE_RE = re.compile(r"(?mi)^//\s*Source:\s*([^\s]+)")
_REGENERATE_RE = re.compile(r"(?mi)^//\s*Regenerate:\s*(.+)$")


@dataclass(frozen=True)
class PackageReadiness:
    package: str
    path: str
    risk: float
    package_doc: bool
    guide_path: str | None
    guide_strength: float
    guide_quality: float
    entrypoint_score: float
    verification_score: float
    dependency_direction: float
    invariant_content: float
    source_test_score: float
    local_scope_score: float
    generated_linkage_score: float
    generated_applicable: bool
    change_confinement: float
    valid_entry_files: tuple[str, ...]
    valid_entry_symbols: tuple[str, ...]
    valid_commands: tuple[str, ...]
    test_files: tuple[str, ...]
    referenced_test_symbols: tuple[str, ...]
    generated_files: tuple[str, ...]
    linked_generated_files: tuple[str, ...]


@dataclass(frozen=True)
class AIReadinessFacts:
    available: bool
    detail: str
    packages: dict[str, PackageReadiness]
    high_risk_packages: tuple[str, ...]


def collect(repo: RepositoryInventory, high_risk_count: int = 12) -> AIReadinessFacts:
    """Cross-check local guidance against tracked source, tests, and generators."""

    risks = {key: _risk(repo, item) for key, item in repo.packages.items()}
    ranked = sorted(repo.packages, key=lambda key: (-risks[key], key))
    high_risk = tuple(ranked[:high_risk_count])
    tracked = _tracked_files(repo.root)
    if tracked is None:
        return AIReadinessFacts(
            available=False,
            detail="git tracked-file inventory unavailable",
            packages={
                key: _empty_package(repo.packages[key], risks[key]) for key in sorted(repo.packages)
            },
            high_risk_packages=high_risk,
        )

    tracked_set = {path.as_posix() for path in tracked}
    production: dict[str, list[Path]] = {key: [] for key in repo.packages}
    tests: dict[str, list[Path]] = {key: [] for key in repo.packages}
    generated: dict[str, list[Path]] = {key: [] for key in repo.packages}
    for relative in tracked:
        key = _go_package(relative)
        if key not in repo.packages:
            continue
        if relative.name.endswith("_test.go"):
            tests[key].append(relative)
        elif relative.name.endswith(".go"):
            if relative.name.endswith("_gen.go"):
                generated[key].append(relative)
            else:
                production[key].append(relative)

    selected_packages = set(high_risk)
    selected_packages.update(
        key for key, item in repo.packages.items() if item.guide_path is not None
    )
    selected_paths: set[Path] = {Path("Makefile")}
    for key in selected_packages:
        selected_paths.update(production[key])
    for key in high_risk:
        selected_paths.update(tests[key])
        selected_paths.update(generated[key])
    selected_paths.update(
        Path(item.guide_path) for item in repo.packages.values() if item.guide_path
    )
    texts = _read_many(repo.root, selected_paths)
    make_targets = set(
        re.findall(r"(?m)^([A-Za-z0-9_.%/-]+)\s*:(?!=)", texts.get("Makefile", ""))
    )
    for key in selected_packages:
        detected = [
            path
            for path in production[key]
            if _GENERATED_RE.search(
                "\n".join(texts.get(path.as_posix(), "").splitlines()[:10])
            )
        ]
        if detected:
            production[key] = [path for path in production[key] if path not in detected]
            generated[key].extend(path for path in detected if path not in generated[key])

    results: dict[str, PackageReadiness] = {}
    for key in sorted(repo.packages):
        item = repo.packages[key]
        source_paths = production[key]
        source_symbols = _source_symbols(source_paths, texts)
        guide = _guide_signals(
            repo=repo,
            package=item,
            source_paths=source_paths,
            source_symbols=source_symbols,
            texts=texts,
            tracked=tracked_set,
            make_targets=make_targets,
        )
        source_test = _source_test_signals(
            test_paths=tests[key],
            source_symbols=source_symbols,
            texts=texts,
            measured=key in high_risk,
        )
        generated_score, generated_links = _generated_signals(
            root=repo.root,
            paths=generated[key],
            texts=texts,
            tracked=tracked_set,
            make_targets=make_targets,
            measured=key in high_risk,
        )
        touches = repo.history.package_touches.get(key, 0) if repo.history.available else 0
        multi = repo.history.multipackage_touches.get(key, 0) if repo.history.available else 0
        confinement = max(0.0, 1.0 - multi / touches) if touches else 0.5
        # Rubric: AI must compose a local change scope. Git multipackage
        # confinement is one signal; an explicit Local change scope section with
        # real neighbor paths, forbidden surfaces, and a focused verify command
        # is confinement-equivalent (mere guide length still does not score).
        guide_text = texts.get(item.guide_path, "") if item.guide_path else ""
        boundary_doc = _change_boundary_doc_score(guide_text, guide[7])
        local_scope = 0.5 * item.guide_strength + 0.5 * max(confinement, boundary_doc)
        results[key] = PackageReadiness(
            package=key,
            path=item.path,
            risk=risks[key],
            package_doc=item.has_package_doc,
            guide_path=item.guide_path,
            guide_strength=item.guide_strength,
            guide_quality=guide[0],
            entrypoint_score=guide[1],
            verification_score=guide[2],
            dependency_direction=guide[3],
            invariant_content=guide[4],
            source_test_score=source_test[0],
            local_scope_score=local_scope,
            generated_linkage_score=generated_score,
            generated_applicable=bool(generated[key]),
            change_confinement=confinement,
            valid_entry_files=guide[5],
            valid_entry_symbols=guide[6],
            valid_commands=guide[7],
            test_files=tuple(path.as_posix() for path in tests[key]),
            referenced_test_symbols=source_test[1],
            generated_files=tuple(path.as_posix() for path in generated[key]),
            linked_generated_files=generated_links,
        )
    return AIReadinessFacts(
        available=True,
        detail=(
            f"{len(results)} packages cross-checked; {len(high_risk)} high-risk packages "
            "validated against source, tests, guides, and generators"
        ),
        packages=results,
        high_risk_packages=high_risk,
    )


def _risk(repo: RepositoryInventory, package: PackageFacts) -> float:
    touches = repo.history.package_touches.get(package.key, 0) if repo.history.available else 0
    fanin = len(repo.reverse_graph[package.key])
    return math.sqrt(max(1, package.source_loc)) * (1 + touches) * (1 + fanin)


def _tracked_files(root: Path) -> list[Path] | None:
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
    return sorted(
        Path(raw.decode("utf-8", errors="surrogateescape"))
        for raw in process.stdout.split(b"\0")
        if raw
    )


def _go_package(path: Path) -> str | None:
    parts = path.parts
    if len(parts) < 4 or parts[:2] != ("gateway-go", "internal"):
        return None
    return Path(*parts[1:-1]).as_posix()


def _read_many(root: Path, paths: set[Path]) -> dict[str, str]:
    def read(relative: Path) -> tuple[str, str]:
        try:
            text = (root / relative).read_text(encoding="utf-8", errors="replace")
        except OSError:
            text = ""
        return relative.as_posix(), text

    with ThreadPoolExecutor(max_workers=16) as pool:
        return dict(pool.map(read, sorted(paths)))


def _source_symbols(paths: list[Path], texts: dict[str, str]) -> set[str]:
    symbols: set[str] = set()
    for path in paths:
        symbols.update(_DECL_RE.findall(texts.get(path.as_posix(), "")))
    return symbols


def _change_boundary_doc_score(guide_text: str, valid_commands: tuple[str, ...]) -> float:
    """Score an explicit Local change scope section (0..1).

    Requires a dedicated heading plus neighbor package paths, forbidden
    surfaces, and at least one validated verify command — matching the
    AI-change-readiness rubric without rewarding empty prose.
    """
    if not guide_text or not _LOCAL_SCOPE_HEADING_RE.search(guide_text):
        return 0.0
    match = _LOCAL_SCOPE_HEADING_RE.search(guide_text)
    assert match is not None
    rest = guide_text[match.end() :]
    next_heading = re.search(r"(?m)^#{2,3}\s+\S", rest)
    section = rest[: next_heading.start()] if next_heading else rest
    has_neighbor = bool(_SCOPE_NEIGHBOR_RE.search(section))
    has_forbidden = bool(_SCOPE_FORBIDDEN_RE.search(section))
    has_verify = bool(valid_commands)
    credits = int(has_neighbor) + int(has_forbidden) + int(has_verify)
    if credits >= 3:
        return 1.0
    if credits == 2:
        return 0.7
    if credits == 1:
        return 0.35
    return 0.0


def _guide_signals(
    *,
    repo: RepositoryInventory,
    package: PackageFacts,
    source_paths: list[Path],
    source_symbols: set[str],
    texts: dict[str, str],
    tracked: set[str],
    make_targets: set[str],
) -> tuple[float, float, float, float, float, tuple[str, ...], tuple[str, ...], tuple[str, ...]]:
    if not package.guide_path:
        return 0.0, 0.0, 0.0, 0.0, 0.0, (), (), ()
    guide_text = texts.get(package.guide_path, "")
    guide_parent = Path(package.guide_path).parent
    source_set = {path.as_posix() for path in source_paths}
    valid_files: set[str] = set()
    for raw in _FILE_REF_RE.findall(guide_text):
        reference = Path(raw.strip("`'\".,;:()[]"))
        candidates = [reference]
        if not reference.as_posix().startswith("gateway-go/"):
            candidates.extend([guide_parent / reference, Path(package.path) / reference.name])
        valid_files.update(
            candidate.as_posix() for candidate in candidates if candidate.as_posix() in source_set
        )
    mentioned = set(_WORD_RE.findall(guide_text)) & source_symbols
    entry_symbols = {name for name in mentioned if name[0].isupper()}
    file_credit = float(bool(valid_files))
    symbol_credit = float(bool(entry_symbols))
    entrypoint = 0.5 * file_credit + 0.5 * symbol_credit
    commands = sorted(set(_COMMAND_RE.findall(guide_text) + _INLINE_COMMAND_RE.findall(guide_text)))
    valid_commands = tuple(
        command
        for command in commands
        if _command_score(repo.root, command, tracked, make_targets, package.key) > 0
    )
    verification = max(
        (
            _command_score(repo.root, command, tracked, make_targets, package.key)
            for command in commands
        ),
        default=0.0,
    )
    dependency = float(bool(_DEPENDENCY_RE.search(guide_text))) * entrypoint
    invariant = float(bool(_INVARIANT_RE.search(guide_text))) * entrypoint
    content_quality = 0.65 * entrypoint + 0.35 * verification
    quality = package.guide_strength * content_quality
    return (
        quality,
        entrypoint,
        verification,
        dependency,
        invariant,
        tuple(sorted(valid_files)),
        tuple(sorted(entry_symbols)),
        valid_commands,
    )


def _command_score(
    root: Path, command: str, tracked: set[str], make_targets: set[str],
    package_key: str,
) -> float:
    cleaned = command.strip().lstrip("$ ")
    cwd = root
    if "&&" in cleaned and cleaned.startswith("cd "):
        prefix, cleaned = cleaned.split("&&", 1)
        try:
            target = shlex.split(prefix)[1]
        except (IndexError, ValueError):
            return 0.0
        cwd = (root / target).resolve()
        cleaned = cleaned.strip()
    try:
        parts = shlex.split(cleaned)
    except ValueError:
        return 0.0
    if not parts:
        return 0.0
    if parts[0] == "make" and len(parts) >= 2:
        target = parts[1]
        if target not in make_targets:
            return 0.0
        if not any(token in target for token in ("test", "check", "verify", "lint", "vet")):
            return 0.0
        return 0.4
    if parts[:2] == ["go", "test"]:
        targets = [part for part in parts[2:] if not part.startswith("-")]
        if not targets:
            return 0.0
        if targets == ["./..."]:
            return 0.3
        expected = package_key
        for target in targets:
            if target.startswith("./") and not (cwd / target[2:]).exists():
                return 0.0
        normalized = [target.removeprefix("./").removeprefix("gateway-go/") for target in targets]
        if expected in normalized:
            return 1.0
        if any(target.endswith("/...") and expected.startswith(target[:-4]) for target in normalized):
            return 0.7
        return 0.0
    if parts[0].startswith("python") and len(parts) >= 2:
        script = parts[1]
        if script == "-m":
            return 0.5
        candidate = Path(script.lstrip("./"))
        return 0.4 if candidate.as_posix() in tracked else 0.0
    if parts[0].startswith(("./", "scripts/")):
        candidate = Path(parts[0].lstrip("./"))
        return 0.4 if candidate.as_posix() in tracked else 0.0
    if parts[0] in {"pnpm", "gradle", "./gradlew"}:
        return 0.5 if len(parts) > 1 else 0.0
    return 0.0


def _source_test_signals(
    *,
    test_paths: list[Path],
    source_symbols: set[str],
    texts: dict[str, str],
    measured: bool,
) -> tuple[float, tuple[str, ...]]:
    if not measured or not test_paths:
        return 0.0, ()
    useful_tests = [
        path
        for path in test_paths
        if not _GENERATED_RE.search("\n".join(texts.get(path.as_posix(), "").splitlines()[:10]))
    ]
    if not useful_tests:
        return 0.0, ()
    test_text = "\n".join(texts.get(path.as_posix(), "") for path in useful_tests)
    code = _mask_literals(_remove_comments(test_text))
    referenced = tuple(sorted(set(_USED_SYMBOL_RE.findall(code)) & source_symbols))
    symbol_credit = min(1.0, len(referenced) / 3.0)
    return symbol_credit, referenced


def _generated_signals(
    *,
    root: Path,
    paths: list[Path],
    texts: dict[str, str],
    tracked: set[str],
    make_targets: set[str],
    measured: bool,
) -> tuple[float, tuple[str, ...]]:
    if not paths:
        return 1.0, ()
    if not measured:
        return 0.0, ()
    linked: list[str] = []
    scores: list[float] = []
    for path in paths:
        text = texts.get(path.as_posix(), "")
        source_match = _SOURCE_RE.search(text)
        command_match = _REGENERATE_RE.search(text)
        source_ok = False
        if source_match:
            raw_source = Path(source_match.group(1).lstrip("./"))
            source_ok = any(
                candidate.as_posix() in tracked
                for candidate in (raw_source, path.parent / raw_source)
            )
        command_ok = bool(
            command_match
            and _regeneration_command_valid(command_match.group(1), tracked, make_targets)
        )
        scores.append(0.5 * source_ok + 0.5 * command_ok)
        if source_ok and command_ok:
            linked.append(path.as_posix())
    return sum(scores) / len(scores), tuple(sorted(linked))


def _regeneration_command_valid(
    command: str, tracked: set[str], make_targets: set[str]
) -> bool:
    try:
        parts = shlex.split(command.strip())
    except ValueError:
        return False
    if not parts:
        return False
    if parts[0] == "make" and len(parts) >= 2:
        return parts[1] in make_targets
    if parts[0].startswith(("./", "scripts/")):
        return Path(parts[0].lstrip("./")).as_posix() in tracked
    return False


def evaluate_navigability(
    repo: RepositoryInventory, facts: AIReadinessFacts
) -> Pillar:
    pillar_id = "ai-navigability"
    signals = list(facts.packages.values())
    if not facts.available or not signals:
        return _unavailable_pillar(
            pillar_id, "AI navigability", 12, facts.detail,
            "Make ownership, entry points, invariants, and verification locally discoverable.",
        )
    doc = _coverage(signals, lambda item: float(item.package_doc))
    guide = _coverage(signals, lambda item: item.guide_quality)
    entrypoint = _coverage(signals, lambda item: item.entrypoint_score)
    verification = _coverage(signals, lambda item: item.verification_score)
    architecture = _coverage(
        signals, lambda item: 0.5 * (item.dependency_direction + item.invariant_content)
    )
    # Package comments are supporting evidence only. Every scored documentation
    # signal is gated by a real source reference or executable command.
    go_score = 0.40 * guide + 0.30 * entrypoint + 0.20 * verification + 0.10 * architecture
    # Unmeasured lanes reduce confidence, not the measured repository score.
    score = go_score
    ranked = sorted(signals, key=lambda item: (-item.risk, item.package))[:15]
    findings: list[Finding] = []
    for index, item in enumerate(ranked):
        gaps: list[str] = []
        if not item.package_doc:
            gaps.append("supporting Package purpose comment missing")
        if item.guide_quality < 0.50:
            gaps.append(
                f"local guide quality {item.guide_quality:.2f} "
                f"(path={item.guide_path or 'none'}, depth={item.guide_strength:.2f})"
            )
        if item.entrypoint_score < 0.50:
            gaps.append(
                f"validated entrypoint {item.entrypoint_score:.2f} "
                f"(files={list(item.valid_entry_files)}, symbols={list(item.valid_entry_symbols)})"
            )
        if item.verification_score < 0.75:
            gaps.append(
                f"focused verification {item.verification_score:.2f} "
                f"(commands={list(item.valid_commands)})"
            )
        if item.dependency_direction < 1.0:
            gaps.append("dependency direction not tied to verified source")
        if item.invariant_content < 1.0:
            gaps.append("invariant not tied to verified source")
        if not gaps:
            continue
        scored_gaps = sum(not gap.startswith("supporting") for gap in gaps)
        if scored_gaps == 0:
            continue
        severity = (
            "high" if index < 8 and scored_gaps >= 2
            else "medium" if scored_gaps
            else "info"
        )
        findings.append(_gap(
            pillar_id, "ai-navigation-gaps", item, severity, "; ".join(gaps),
            "Verified local evidence including an actual source symbol is insufficient for a safe change.",
            "Add a nearby guide that names existing entry files/exported symbols, states "
            "source-bound dependency/invariant rules, and provides a focused valid command.",
            92 - index,
        ))
    return Pillar(
        id=pillar_id, title="AI navigability", weight=12, score=score,
        intent="Make ownership, entry points, invariants, and verification locally discoverable.",
        metrics={
            "scope": (
                "gateway-go/internal; Kotlin/TypeScript/Python AI-specific evidence is "
                "not yet measured and lowers report confidence rather than this score"
            ),
            "measured_product_lanes": list(MEASURED_PRODUCT_LANES),
            "unmeasured_product_lanes": [
                lane for lane in ACTIVE_PRODUCT_LANES if lane not in MEASURED_PRODUCT_LANES
            ],
            "active_lane_product_impact_coverage": round(LANE_COVERAGE, 3),
            "excluded_support_lanes": ["shell", "rust"],
            "excluded_support_lane_reason": "No package-level AI guidance contract is defined.",
            "measured_go_score": round(go_score, 1),
            "supporting_package_purpose": round(doc, 1),
            "risk_weighted_local_guide_quality": round(guide, 1),
            "risk_weighted_valid_entrypoints": round(entrypoint, 1),
            "risk_weighted_focused_verification": round(verification, 1),
            "risk_weighted_specific_verification": round(verification, 1),
            "risk_weighted_source_bound_architecture": round(architecture, 1),
            "navigation_hotspots": [_signal_metrics(item) for item in ranked],
        },
        findings=findings,
    )


def evaluate_change_readiness(
    repo: RepositoryInventory, facts: AIReadinessFacts
) -> Pillar:
    pillar_id = "ai-change-readiness"
    if not facts.available:
        return _unavailable_pillar(
            pillar_id, "AI change readiness", 8, facts.detail,
            "Make high-risk changes easy to locate, scope, regenerate, and verify.",
        )
    signals = [facts.packages[key] for key in facts.high_risk_packages]
    generated = [item for item in signals if item.generated_applicable]
    subscores = {
        "source_to_test_discoverability": _coverage(signals, lambda item: item.source_test_score),
        "local_change_scope": _coverage(signals, lambda item: item.local_scope_score),
        "entrypoint_registry_index": _coverage(signals, lambda item: item.entrypoint_score),
        "generated_source_of_truth": (
            _coverage(generated, lambda item: item.generated_linkage_score)
            if generated else 100.0
        ),
        "verification_specificity": _coverage(signals, lambda item: item.verification_score),
    }
    go_score = _bounded_average(list(subscores.values()))
    score = go_score
    findings: list[Finding] = []
    for index, item in enumerate(signals):
        gaps: list[str] = []
        if item.source_test_score < 0.70:
            gaps.append(
                f"source→test {item.source_test_score:.2f} "
                f"(tests={list(item.test_files)}, symbols={list(item.referenced_test_symbols)})"
            )
        if item.local_scope_score < 0.60:
            gaps.append(
                f"local scope {item.local_scope_score:.2f} "
                f"(guide depth={item.guide_strength:.2f}, confinement={item.change_confinement:.2f})"
            )
        if item.entrypoint_score < 0.75:
            gaps.append(f"entrypoint/registry index {item.entrypoint_score:.2f}")
        if item.generated_applicable and item.generated_linkage_score < 1.0:
            gaps.append(
                f"generated SOT {item.generated_linkage_score:.2f} "
                f"(generated={list(item.generated_files)}, linked={list(item.linked_generated_files)})"
            )
        if item.verification_score < 0.75:
            gaps.append(
                f"focused verification {item.verification_score:.2f} "
                f"(commands={list(item.valid_commands)})"
            )
        if not gaps:
            continue
        severity = "high" if index < 6 and len(gaps) >= 3 else "medium"
        findings.append(_gap(
            pillar_id, "ai-change-readiness-gaps", item, severity, "; ".join(gaps),
            "The high-risk package cannot be located, scoped, regenerated, and verified in one local workflow.",
            "Link real tests and entry symbols, localize the change boundary, preserve generated "
            "source-of-truth headers, and document a focused executable check.",
            94 - index,
        ))
    return Pillar(
        id=pillar_id, title="AI change readiness", weight=8, score=score,
        intent="Make high-risk changes easy to locate, scope, regenerate, and verify.",
        metrics={
            "scope": "12 highest-risk gateway-go/internal packages",
            "measured_product_lanes": list(MEASURED_PRODUCT_LANES),
            "unmeasured_product_lanes": [
                lane for lane in ACTIVE_PRODUCT_LANES if lane not in MEASURED_PRODUCT_LANES
            ],
            "active_lane_product_impact_coverage": round(LANE_COVERAGE, 3),
            "excluded_support_lanes": ["shell", "rust"],
            "excluded_support_lane_reason": "No package-level AI guidance contract is defined.",
            "measured_go_score": round(go_score, 1),
            "high_risk_packages": list(facts.high_risk_packages),
            "subscores": {key: round(value, 1) for key, value in subscores.items()},
            "package_readiness": [_signal_metrics(item) for item in signals],
        },
        findings=findings,
    )


def _coverage(signals: list[PackageReadiness], value) -> float:
    denominator = sum(item.risk for item in signals)
    if denominator <= 0:
        return 0.0
    return 100.0 * sum(item.risk * value(item) for item in signals) / denominator


def _bounded_average(values: list[float]) -> float:
    if not values:
        return 0.0
    return min(sum(values) / len(values), min(values) + 30.0)


def _signal_metrics(item: PackageReadiness) -> dict[str, object]:
    return {
        "package": item.package,
        "risk": round(item.risk, 1),
        "guide": item.guide_path,
        "guide_quality": round(item.guide_quality, 3),
        "entrypoint": round(item.entrypoint_score, 3),
        "verification": round(item.verification_score, 3),
        "source_test": round(item.source_test_score, 3),
        "local_scope": round(item.local_scope_score, 3),
        "generated_linkage": round(item.generated_linkage_score, 3),
    }


def _gap(
    pillar: str, rule: str, item: PackageReadiness, severity: str,
    evidence: str, why: str, remediation: str, priority: float,
) -> Finding:
    return Finding(
        id=stable_id(rule, item.path), pillar=pillar, severity=severity, path=item.path,
        evidence=evidence, why=why, remediation=remediation, verify=VERIFY,
        priority=priority,
    )


def _unavailable_pillar(
    pillar: str, title: str, weight: float, detail: str, intent: str,
) -> Pillar:
    finding = Finding(
        id=stable_id("ai-readiness-unavailable", pillar), pillar=pillar,
        severity="high", path=".git", evidence=detail,
        why="AI-maintenance evidence cannot be cross-checked without tracked source inventory.",
        remediation="Provide a complete Git checkout and rerun the benchmark.", verify=VERIFY,
        priority=100,
    )
    return Pillar(
        id=pillar, title=title, weight=weight, score=0, intent=intent,
        metrics={"measured": False, "reason": detail}, findings=[finding],
    )


def _empty_package(package: PackageFacts, risk: float) -> PackageReadiness:
    return PackageReadiness(
        package=package.key, path=package.path, risk=risk,
        package_doc=package.has_package_doc, guide_path=package.guide_path,
        guide_strength=package.guide_strength, guide_quality=0.0,
        entrypoint_score=0.0, verification_score=0.0,
        dependency_direction=0.0, invariant_content=0.0,
        source_test_score=0.0, local_scope_score=0.0,
        generated_linkage_score=0.0, generated_applicable=False,
        change_confinement=0.0, valid_entry_files=(), valid_entry_symbols=(),
        valid_commands=(), test_files=(), referenced_test_symbols=(),
        generated_files=(), linked_generated_files=(),
    )

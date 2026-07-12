"""Test-effectiveness and test-maintainability scoring orchestration."""

from __future__ import annotations

import ast
import re
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable

from .model import (
    Evidence,
    Finding,
    Pillar,
    PRODUCT_LANE_IMPACT,
    geometric_mean,
    stable_id,
)
from .testing_inventory import (
    classify as _classify,
    control_text as _control_text,
    generated_with_provenance as _generated_with_provenance,
    git_files as _git_files,
    read_many as _read_many,
)
from .testing_parsing import (
    Case as _Case,
    RISK_RULES as _RISK_RULES,
    case_has_intent as _case_has_intent,
    hazards as _hazards,
    has_observable_oracle as _has_observable_oracle,
    normalized_subject as _normalized_subject,
    risk_source_text as _risk_source_text,
    source_symbols as _source_symbols,
    test_cases as _test_cases,
    test_stem as _test_stem,
    unit_key as _unit_key,
)

EFFECTIVENESS_ID = "test-effectiveness"
MAINTAINABILITY_ID = "test-maintainability"
TEST_SIZE_SOFT = 700
TOP_N = 12


@dataclass
class _TestFile:
    language: str
    path: Path
    rel: str
    text: str
    unit: str
    line_count: int
    generated: bool = False
    cases: list[_Case] = field(default_factory=list)
    target_units: tuple[str, ...] = ()
    case_target_units: list[tuple[str, ...]] = field(default_factory=list)
    localized: list[bool] = field(default_factory=list)
    intentful: list[bool] = field(default_factory=list)
    hazards: tuple[str, ...] = ()


def _python_imports(text: str) -> set[str]:
    """Return absolute modules imported by a Python test.

    Python behavior tests in this repository intentionally live in the central
    ``scripts/audit`` and ``scripts/dev`` suites.  Their production subjects
    can therefore be in a child package or another scripts directory rather
    than beside the test file.  Imports provide a stronger link than physical
    proximity, while syntax errors simply leave the conservative default unit.
    """
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return set()
    modules: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            modules.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            modules.add(node.module)
    return modules


_KOTLIN_IMPORT_RE = re.compile(
    r"(?m)^\s*import\s+"
    r"([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)+)"
    r"(?:\s+as\s+([A-Za-z_]\w*))?\s*$"
)


def _kotlin_import_targets(
    text: str,
    *,
    source_units: set[str],
    normalized_symbols_by_unit: dict[str, set[str]],
) -> tuple[tuple[str, str], ...]:
    """Resolve explicitly imported and callable Kotlin production subjects.

    Kotlin multiplatform tests frequently live in a broad ``commonTest``
    package while exercising a production type or top-level function imported
    from another package. Package locality alone misses that relationship. An
    import receives credit only when its owning production unit exists and the
    imported top-level subject is a declared symbol; per-case call evidence is
    checked separately so an unused or comment-only import cannot launder a
    test into an unrelated production unit.
    """
    targets: set[tuple[str, str]] = set()
    for match in _KOTLIN_IMPORT_RE.finditer(text):
        qualified, alias = match.groups()
        parts = qualified.split(".")
        # An explicit import must leave at least one subject segment after the
        # package. Wildcard imports are deliberately not credited.
        for end in range(len(parts) - 1, 0, -1):
            unit = f"kotlin:{'.'.join(parts[:end])}"
            if unit not in source_units:
                continue
            subject = _normalized_subject(parts[end])
            if subject not in normalized_symbols_by_unit.get(unit, set()):
                break
            targets.add((unit, alias or parts[-1]))
            break
    return tuple(sorted(targets))


def _kotlin_called_subjects(text: str) -> set[str]:
    code = _risk_source_text("kotlin", text)
    return set(
        re.findall(
            r"\b([A-Za-z_]\w*)\b\s*(?:<[^>\n]+>\s*)?(?:\(|\.)",
            code,
        )
    )


def _test_target_units(
    item: _TestFile,
    *,
    root: Path,
    source_by_unit: dict[str, list[tuple[Path, str]]],
    stems_by_unit: dict[str, set[str]],
) -> tuple[str, ...]:
    """Map a test to production units using explicit, deterministic links."""
    targets = {item.unit}
    if item.language != "python":
        return (item.unit,)

    stem = _test_stem(item.language, item.path)
    stem_matches = {
        unit
        for unit, subjects in stems_by_unit.items()
        if unit.startswith("python:")
        and any(
            len(subject) >= 5 and (stem == subject or stem.startswith(subject))
            for subject in subjects
        )
    }
    if len(stem_matches) == 1:
        targets.update(stem_matches)

    source_units = set(source_by_unit)
    for module in _python_imports(item.text):
        parts = module.split(".")
        for base in (item.path.parent, root):
            for end in range(len(parts), 0, -1):
                candidate = base.joinpath(*parts[:end])
                try:
                    relative = candidate.relative_to(root).as_posix()
                except ValueError:
                    continue
                unit = f"python:{relative}"
                if unit in source_units:
                    targets.add(unit)
                    break

    return tuple(sorted(targets))


def _signal_finding(
    *,
    pillar: str,
    signal: str,
    score: float,
    language: str,
    path: str,
    evidence: str,
    why: str,
    remediation: str,
    verify: str,
    related: Iterable[str] = (),
) -> Finding:
    impact = PRODUCT_LANE_IMPACT[language]
    if score < 50 and impact >= 0.20:
        severity = "high"
    elif score < 75 and impact >= 0.10:
        severity = "medium"
    else:
        severity = "low"
    return Finding(
        # Counts and worst paths belong in evidence, not the ratchet ID.
        id=stable_id(f"test-{signal}", pillar, language),
        pillar=pillar,
        severity=severity,
        path=path,
        evidence=f"worst lane {language}={score:.1f}; {evidence}",
        why=why,
        remediation=remediation,
        verify=verify,
        priority=min(100.0, (100.0 - score) * (0.4 + impact)),
        related_paths=tuple(dict.fromkeys(related))[:TOP_N],
    )


def _language_average(values: dict[str, float]) -> float:
    if not values:
        return 0.0
    unknown = sorted(
        language
        for language in values
        if PRODUCT_LANE_IMPACT.get(language, 0.0) <= 0
    )
    if unknown:
        raise ValueError(f"test signals lack product impact: {', '.join(unknown)}")
    total = sum(PRODUCT_LANE_IMPACT[language] for language in values)
    return sum(
        score * PRODUCT_LANE_IMPACT[language] / total
        for language, score in values.items()
    )


def _worst_language(values: dict[str, float]) -> tuple[str, float]:
    if not values:
        raise ValueError("cannot identify a worst language without lane evidence")
    return min(values.items(), key=lambda item: (item[1], item[0]))


def _empty_result(detail: str) -> tuple[list[Pillar], list[Evidence]]:
    evidence = Evidence("static-test-inventory", "unavailable", detail, required=True)
    return [
        Pillar(
            id=EFFECTIVENESS_ID,
            title="Test effectiveness",
            weight=14,
            score=0,
            intent="Tests prove risky behavior through independent, observable contracts.",
        ),
        Pillar(
            id=MAINTAINABILITY_ID,
            title="Test maintainability",
            weight=8,
            score=0,
            intent="Tests remain compact, intentional, isolated, and easy to navigate.",
        ),
    ], [evidence]


def evaluate(root: Path) -> tuple[list[Pillar], list[Evidence]]:
    """Evaluate static tests under ``root`` without executing project code."""
    root = root.resolve()
    try:
        # Avoid a stat per path: git already returns file entries, and reads
        # safely turn a concurrently deleted path into an empty string.
        files = _git_files(root)
    except RuntimeError as exc:
        return _empty_result(str(exc))

    classified = {
        path: item
        for path in files
        if (item := _classify(root, path)) is not None
    }
    texts = _read_many(classified)
    controls = _control_text(root, files, texts)

    source_by_unit: dict[str, list[tuple[Path, str]]] = defaultdict(list)
    symbols_by_unit: dict[str, set[str]] = defaultdict(set)
    stems_by_unit: dict[str, set[str]] = defaultdict(set)
    tests: list[_TestFile] = []
    unproven_generated: list[str] = []

    # Python harnesses such as test_deploy_shell.py can map to deploy.sh even
    # though shell production is not part of the Python source lane.
    sibling_stems: dict[Path, set[str]] = defaultdict(set)
    for path in files:
        item = classified.get(path)
        if item is not None and item[1]:
            continue
        sibling_stems[path.parent].add(_normalized_subject(path.stem))

    for path, (language, is_test) in classified.items():
        text = texts.get(path, "")
        unit = _unit_key(language, path, text, root)
        rel = path.relative_to(root).as_posix()
        if not is_test:
            source_by_unit[unit].append((path, text))
            symbols_by_unit[unit].update(_source_symbols(language, text))
            stems_by_unit[unit].add(_normalized_subject(path.stem))
            continue
        head = "\n".join(text.splitlines()[:10])
        has_marker = bool(re.search(r"DO\s+NOT\s+EDIT|@generated", head, re.IGNORECASE))
        generated = _generated_with_provenance(root, rel, text, controls)
        if has_marker and not generated:
            unproven_generated.append(rel)
        item = _TestFile(
            language=language,
            path=path,
            rel=rel,
            text=text,
            unit=unit,
            line_count=text.count("\n") + (1 if text else 0),
            generated=generated,
        )
        parsed_cases = _test_cases(language, text)
        if generated:
            # A provenance-backed generated suite can carry one real runtime
            # contract shape, but generated row volume must never manufacture
            # effectiveness. Maintainability excludes the file entirely below.
            selected = next((case for case in parsed_cases if case.oracle), None)
            if selected is None and parsed_cases and _has_observable_oracle(language, text):
                first = parsed_cases[0]
                selected = _Case(
                    name=first.name,
                    body=first.body,
                    shape=first.shape,
                    oracle=True,
                    risk_signals=first.risk_signals,
                )
            item.cases = [selected] if selected is not None else []
        else:
            item.cases = parsed_cases
            item.hazards = _hazards(language, text)
        tests.append(item)

    effectiveness_tests = [item for item in tests if item.cases]
    maintainability_tests = [item for item in tests if not item.generated]
    normalized_symbols_by_unit = {
        unit: {
            normalized
            for symbol in symbols
            if (normalized := _normalized_subject(symbol))
        }
        for unit, symbols in symbols_by_unit.items()
    }
    for item in effectiveness_tests:
        base_targets = _test_target_units(
            item,
            root=root,
            source_by_unit=source_by_unit,
            stems_by_unit=stems_by_unit,
        )
        kotlin_imports = (
            _kotlin_import_targets(
                item.text,
                source_units=set(source_by_unit),
                normalized_symbols_by_unit=normalized_symbols_by_unit,
            )
            if item.language == "kotlin"
            else ()
        )
        kotlin_file_calls = (
            _kotlin_called_subjects(item.text) if item.language == "kotlin" else set()
        )
        for case in item.cases:
            case_targets = set(base_targets)
            kotlin_case_calls = (
                _kotlin_called_subjects(case.body)
                if item.language == "kotlin"
                else set()
            )
            case_targets.update(
                unit
                for unit, reference in kotlin_imports
                if reference in kotlin_case_calls
            )
            ordered_targets = tuple(sorted(case_targets))
            item.case_target_units.append(ordered_targets)
            source_stems = {
                subject
                for unit in ordered_targets
                for subject in stems_by_unit.get(unit, set())
            }
            stem = _test_stem(item.language, item.path)
            subject_stems = source_stems | sibling_stems[item.path.parent]
            file_local = any(
                len(subject) >= 5 and (stem == subject or stem.startswith(subject))
                for subject in subject_stems
            )
            normalized_symbols = {
                symbol
                for unit in ordered_targets
                for symbol in normalized_symbols_by_unit.get(unit, set())
            }
            normalized_name = _normalized_subject(case.name)
            symbol_local = any(symbol in normalized_name for symbol in normalized_symbols)
            generated_subject_call = item.generated and any(
                symbol in kotlin_file_calls
                for unit in ordered_targets
                for symbol in symbols_by_unit.get(unit, set())
            )
            item.localized.append(file_local or symbol_local or generated_subject_call)
            item.intentful.append(_case_has_intent(case.name))
        item.target_units = tuple(
            sorted(
                {
                    unit
                    for targets in item.case_target_units
                    for unit in targets
                }
                or set(base_targets)
            )
        )

    # Production lanes define denominators. Deleting tests or defeating the
    # lightweight parser must make a lane score zero, not make it disappear.
    languages = sorted({unit.split(":", 1)[0] for unit in source_by_unit})
    case_counts_by_language = {
        language: sum(
            len(item.cases)
            for item in effectiveness_tests
            if item.language == language
        )
        for language in languages
    }
    oracle_by_language: dict[str, float] = {}
    locality_by_language: dict[str, float] = {}
    intent_by_language: dict[str, float] = {}
    uniqueness_by_language: dict[str, float] = {}
    duplicate_units: list[tuple[int, int, str]] = []
    shape_groups_by_language: dict[
        str, dict[tuple[str, str], list[tuple[_TestFile, int]]]
    ] = {}

    for language in languages:
        effective_files = [
            item
            for item in effectiveness_tests
            if item.language == language and item.cases
        ]
        effective_groups: dict[
            tuple[str, str], list[tuple[_TestFile, int]]
        ] = defaultdict(list)
        for item in effective_files:
            for index, case in enumerate(item.cases):
                effective_groups[(item.unit, case.shape)].append((item, index))
        oracle_unique = localized_unique = 0
        for members in effective_groups.values():
            oracle_unique += all(item.cases[index].oracle for item, index in members)
            localized_unique += all(item.localized[index] for item, index in members)
        effective_denominator = max(len(effective_groups), 1)
        oracle_by_language[language] = 100.0 * oracle_unique / effective_denominator
        locality_by_language[language] = (
            100.0 * localized_unique / effective_denominator
        )

        maintainable_files = [
            item
            for item in maintainability_tests
            if item.language == language and item.cases
        ]
        maintainable_groups: dict[
            tuple[str, str], list[tuple[_TestFile, int]]
        ] = defaultdict(list)
        totals_by_unit: dict[str, int] = defaultdict(int)
        for item in maintainable_files:
            for index, case in enumerate(item.cases):
                maintainable_groups[(item.unit, case.shape)].append((item, index))
                totals_by_unit[item.unit] += 1
        shape_groups_by_language[language] = maintainable_groups
        unique_count = len(maintainable_groups)
        total_count = sum(totals_by_unit.values())
        unique_by_unit = Counter(unit for unit, _ in maintainable_groups)
        for unit, unit_total in totals_by_unit.items():
            duplicates = unit_total - unique_by_unit[unit]
            if duplicates:
                representative = next(
                    item.rel for item in maintainable_files if item.unit == unit
                )
                duplicate_units.append((duplicates, unit_total, representative))
        intent_unique = 0
        for members in maintainable_groups.values():
            intent_unique += all(item.intentful[index] for item, index in members)
        denominator = max(unique_count, 1)
        intent_by_language[language] = 100.0 * intent_unique / denominator
        uniqueness_by_language[language] = 100.0 * unique_count / max(total_count, 1)

    oracle_score = _language_average(oracle_by_language)
    locality_score = _language_average(locality_by_language)

    obligations = satisfied = 0
    missing_obligations: list[tuple[str, str, str]] = []
    obligation_by_language: dict[str, list[int]] = defaultdict(lambda: [0, 0])
    cases_by_unit: dict[str, list[_Case]] = defaultdict(list)
    for item in effectiveness_tests:
        for index, case in enumerate(item.cases):
            targets = (
                item.case_target_units[index]
                if index < len(item.case_target_units)
                else item.target_units
            )
            for unit in targets:
                cases_by_unit[unit].append(case)
    for unit, sources in source_by_unit.items():
        language = unit.split(":", 1)[0]
        for rule in _RISK_RULES[language]:
            risk_sources = [
                (path, text)
                for path, text in sources
                if rule.source.search(_risk_source_text(language, text))
            ]
            required = min(3, len(risk_sources))
            if required == 0:
                continue
            matching_shapes = {
                case.shape
                for case in cases_by_unit.get(unit, [])
                if rule.name in case.risk_signals
            }
            covered = min(required, len(matching_shapes))
            obligations += required
            satisfied += covered
            obligation_by_language[language][1] += required
            obligation_by_language[language][0] += covered
            for path, _ in risk_sources[covered:required]:
                missing_obligations.append(
                    (language, rule.name, path.parent.relative_to(root).as_posix())
                )
    risk_by_language = {
        language: 100.0 * values[0] / values[1]
        for language, values in obligation_by_language.items()
        if values[1]
    }
    risk_score = _language_average(risk_by_language) if risk_by_language else 100.0
    missing_obligations.sort(
        key=lambda item: (risk_by_language.get(item[0], 100.0), item[2], item[1])
    )

    effectiveness_score = geometric_mean([risk_score, oracle_score, locality_score])
    effectiveness_findings: list[Finding] = []
    verify = "python3 scripts/audit/codebase-health-v2.py --json"
    worst_risk_language, worst_risk = _worst_language(
        risk_by_language
    ) if risk_by_language else ("go", risk_score)
    if worst_risk < 90:
        related = [
            path
            for language, _, path in missing_obligations
            if language == worst_risk_language
        ][:TOP_N]
        effectiveness_findings.append(
            _signal_finding(
                pillar=EFFECTIVENESS_ID,
                signal="risk-obligations",
                score=worst_risk,
                language=worst_risk_language,
                path=related[0] if related else "gateway-go",
                evidence=f"{satisfied}/{obligations} source-file-scaled risk obligations have distinct matching test shapes",
                why="A single test shape must not certify every failure, protocol, concurrency, persistence, or external-I/O surface in a large production package.",
                remediation="Add focused behavior tests for the listed unsatisfied obligations; prefer malformed inputs, rollback/reopen cases, cancellation, and fake transports over volume-only fixtures.",
                verify=verify,
                related=related,
            )
        )
    worst_oracle_language, worst_oracle = _worst_language(
        oracle_by_language
    )
    if worst_oracle < 90:
        weak = [
            item.rel
            for item in effectiveness_tests
            if item.language == worst_oracle_language
            and item.cases
            and not any(case.oracle for case in item.cases)
        ]
        effectiveness_findings.append(
            _signal_finding(
                pillar=EFFECTIVENESS_ID,
                signal="oracle-coverage",
                score=worst_oracle,
                language=worst_oracle_language,
                path=weak[0] if weak else "gateway-go",
                evidence=f"oracle-bearing unique shapes={oracle_score:.1f}% across {len(languages)} languages",
                why="A test without an observable oracle can accept an incorrect result.",
                remediation="Assert visible state, returned values, errors, emitted events, or persisted output in every independent behavior shape.",
                verify=verify,
                related=weak[:TOP_N],
            )
        )
    worst_locality_language, worst_locality = _worst_language(
        locality_by_language
    )
    if worst_locality < 80:
        weak = [
            item.rel
            for item in effectiveness_tests
            if item.language == worst_locality_language
            and item.cases
            and item.localized
            and sum(item.localized) / len(item.localized) < 0.5
        ]
        effectiveness_findings.append(
            _signal_finding(
                pillar=EFFECTIVENESS_ID,
                signal="subject-locality",
                score=worst_locality,
                language=worst_locality_language,
                path=weak[0] if weak else "gateway-go",
                evidence=f"production-subject-local unique shapes={locality_score:.1f}%",
                why="Tests that cannot be mapped from their filename or behavior name to a production subject are hard to discover during impact analysis and weak as executable specifications.",
                remediation="Name the test file after its production subject and start behavior cases with the relevant function, type, component, command, or state machine.",
                verify=verify,
                related=weak[:TOP_N],
            )
        )

    size_by_language = {
        language: max(
            (
                item.line_count
                for item in maintainability_tests
                if item.language == language
            ),
            default=0,
        )
        for language in languages
    }
    uniqueness_score = _language_average(uniqueness_by_language)
    intent_score = _language_average(intent_by_language)
    hazard_files = [item for item in maintainability_tests if item.hazards]
    isolation_by_language: dict[str, float] = {}
    unsafe_shapes_by_language: dict[str, int] = {}
    for language in languages:
        groups = shape_groups_by_language.get(language, {})
        unsafe = sum(
            any(bool(item.hazards) for item, _ in members)
            for members in groups.values()
        )
        unsafe_shapes_by_language[language] = unsafe
        isolation_by_language[language] = (
            100.0 * (1.0 - unsafe / len(groups)) if groups else 0.0
        )
    isolation_score = _language_average(isolation_by_language)
    maintainability_score = geometric_mean(
        [uniqueness_score, intent_score, isolation_score]
    )

    maintainability_findings: list[Finding] = []
    duplicate_units.sort(reverse=True)
    worst_uniqueness_language, worst_uniqueness = _worst_language(
        uniqueness_by_language
    )
    if worst_uniqueness < 85:
        test_languages = {item.rel: item.language for item in maintainability_tests}
        lane_duplicates = [
            row
            for row in duplicate_units
            if test_languages.get(row[2]) == worst_uniqueness_language
        ]
        related = [path for _, _, path in lane_duplicates[:TOP_N]]
        top = lane_duplicates[0] if lane_duplicates else (0, 0, "gateway-go")
        maintainability_findings.append(
            _signal_finding(
                pillar=MAINTAINABILITY_ID,
                signal="shape-uniqueness",
                score=worst_uniqueness,
                language=worst_uniqueness_language,
                path=top[2],
                evidence=f"package-local semantic-shape uniqueness={uniqueness_score:.1f}%; worst unit repeats {top[0]}/{top[1]} cases",
                why="Mechanically cloned cases multiply review and migration cost without adding an independent behavior pattern.",
                remediation="Collapse repeated cases into named table data or a generator with checked provenance; retain separate tests only when their input class or observable contract differs.",
                verify=verify,
                related=related,
            )
        )
    oversize = sorted(
        (
            (item.line_count, item.rel)
            for item in maintainability_tests
            if item.line_count > TEST_SIZE_SOFT
        ),
        reverse=True,
    )
    worst_intent_language, worst_intent = _worst_language(
        intent_by_language
    )
    if worst_intent < 75:
        weak = [
            item.rel
            for item in maintainability_tests
            if item.language == worst_intent_language
            and item.cases
            and item.intentful
            and sum(item.intentful) / len(item.intentful) < 0.5
        ]
        maintainability_findings.append(
            _signal_finding(
                pillar=MAINTAINABILITY_ID,
                signal="intent-naming",
                score=worst_intent,
                language=worst_intent_language,
                path=weak[0] if weak else "gateway-go",
                evidence=f"condition/outcome-bearing unique test shapes={intent_score:.1f}%",
                why="Subject-only names say what code ran but not the condition and outcome that define the contract, weakening failure triage and AI navigation.",
                remediation="Name each behavior as subject + condition + outcome, for example RejectsExpiredToken or preserves draft when persistence fails.",
                verify=verify,
                related=weak[:TOP_N],
            )
        )
    worst_isolation_language, worst_isolation = _worst_language(
        isolation_by_language
    )
    if worst_isolation < 95:
        safe_shapes = sum(
            len(shape_groups_by_language.get(language, {}))
            - unsafe_shapes_by_language.get(language, 0)
            for language in languages
        )
        all_shapes = sum(
            len(shape_groups_by_language.get(language, {})) for language in languages
        )
        maintainability_findings.append(
            _signal_finding(
                pillar=MAINTAINABILITY_ID,
                signal="isolation",
                score=worst_isolation,
                language=worst_isolation_language,
                path=next(
                    (
                        item.rel
                        for item in hazard_files
                        if item.language == worst_isolation_language
                    ),
                    "gateway-go",
                ),
                evidence=f"hazard-free package-unique shapes={safe_shapes}/{all_shapes} ({isolation_score:.1f}%); hazard files={len(hazard_files)}",
                why="Raw sleeps, real clocks or networks, fixed temp paths, and unrecovered process globals create order dependence, flakes, and failures that are difficult to reproduce.",
                remediation="Inject clocks and transports, use framework fake timers, t.TempDir/t.Setenv or tempfile, and restore every process-global mutation with cleanup.",
                verify=verify,
                related=[
                    item.rel
                    for item in hazard_files
                    if item.language == worst_isolation_language
                ][:TOP_N],
            )
        )

    effectiveness = Pillar(
        id=EFFECTIVENESS_ID,
        title="Test effectiveness",
        weight=14,
        score=effectiveness_score,
        intent="Tests prove risky behavior through independent, observable contracts rather than source-volume parity.",
        metrics={
            "risk_obligations": {
                "satisfied": satisfied,
                "total": obligations,
                "score": round(risk_score, 1),
                "max_per_package_risk": 3,
                "satisfaction_unit": "distinct semantic test shape",
                "by_language": {
                    language: {
                        "satisfied": values[0],
                        "total": values[1],
                        "score": round(risk_by_language[language], 1),
                    }
                    for language, values in sorted(obligation_by_language.items())
                },
            },
            "oracle_unique_shapes": {
                "score": round(oracle_score, 1),
                "by_language": {
                    key: round(value, 1) for key, value in oracle_by_language.items()
                },
            },
            "subject_locality": {
                "score": round(locality_score, 1),
                "by_language": {
                    key: round(value, 1) for key, value in locality_by_language.items()
                },
            },
            "test_cases": sum(len(item.cases) for item in effectiveness_tests),
            "test_cases_by_language": case_counts_by_language,
            "generated_contract_shapes": sum(
                len(item.cases) for item in tests if item.generated
            ),
        },
        findings=effectiveness_findings,
    )
    maintainability = Pillar(
        id=MAINTAINABILITY_ID,
        title="Test maintainability",
        weight=8,
        score=maintainability_score,
        intent="Tests remain compact, intentional, isolated, and easy for an AI or human to navigate.",
        metrics={
            "semantic_shape_uniqueness": {
                "score": round(uniqueness_score, 1),
                "by_language": {
                    key: round(value, 1) for key, value in uniqueness_by_language.items()
                },
            },
            "test_file_size_diagnostic": {
                "max_lines": max(size_by_language.values(), default=0),
                "by_language_max": dict(sorted(size_by_language.items())),
                "over_700": len(oversize),
                "scored": False,
            },
            "intent_naming": {
                "score": round(intent_score, 1),
                "by_language": {
                    key: round(value, 1) for key, value in intent_by_language.items()
                },
            },
            "isolation": {
                "score": round(isolation_score, 1),
                "hazard_files": len(hazard_files),
                "files": len(maintainability_tests),
                "unsafe_unique_shapes": sum(unsafe_shapes_by_language.values()),
                "unique_shapes": sum(
                    len(shape_groups_by_language.get(language, {}))
                    for language in languages
                ),
                "by_language": {
                    key: round(value, 1) for key, value in isolation_by_language.items()
                },
            },
            "generated_tests_excluded": sum(item.generated for item in tests),
            "unproven_generated_markers": len(unproven_generated),
        },
        findings=maintainability_findings,
    )
    evidence = [
        Evidence(
            "static-test-inventory",
            "measured",
            f"{len(effectiveness_tests)} effectiveness test files, "
            f"{sum(case_counts_by_language.values())} cases; "
            + ", ".join(
                f"{language}={case_counts_by_language[language]}"
                for language in languages
            ),
            required=True,
        ),
        Evidence(
            "generated-test-provenance",
            "measured",
            f"effectiveness_shapes={sum(len(item.cases) for item in tests if item.generated)}, "
            f"maintainability_excluded={sum(item.generated for item in tests)}, "
            f"unproven_markers={len(unproven_generated)}",
        ),
    ]
    return [effectiveness, maintainability], evidence


__all__ = ["evaluate"]

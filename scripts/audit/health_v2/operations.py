"""Complexity, failure-handling, and concurrency evidence.

These rules intentionally score risky tails and high-confidence operational
failures. Project size and low-confidence lifecycle regexes are diagnostic only.
"""

from __future__ import annotations

import math
import re
import subprocess
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path

from .model import Evidence, Finding, Pillar, descending_grade, geometric_mean, stable_id

_CONTROL_RE = re.compile(r"\b(if|for|case|select|switch)\b|&&|\|\|")
_FUNC_RE = re.compile(
    r"\bfunc\s+(?:\(([^)]*)\)\s*)?([A-Za-z_]\w*)\s*(?:\[[^\]\n]*\]\s*)?\(",
    re.MULTILINE,
)
_IGNORED_ERROR_RE = re.compile(r"(?m)^\s*_\s*=\s*[^=]")
_BG_CONTEXT_RE = re.compile(r"\bcontext\.(?:Background|TODO)\s*\(")
_FATAL_RE = re.compile(r"\b(?:panic|os\.Exit|log\.(?:Fatal|Fatalf|Fatalln))\s*\(")
_HTTP_CLIENT_RE = re.compile(r"\bhttp\.Client\s*\{([^}]*)\}", re.DOTALL)
_FMT_ERROR_CALL_RE = re.compile(r"\bfmt\.Errorf\s*\(")

# Fixed rank weights make the worst functions matter without letting additional
# easy functions dilute an existing hotspot. The first four functions account
# for most of the signal; the remaining ranks distinguish a broad risky tail
# from a single difficult path.
_RANKED_TAIL_WEIGHTS = (
    0.30,
    0.18,
    0.12,
    0.09,
    0.07,
    0.06,
    0.05,
    0.04,
    0.03,
    0.025,
    0.02,
    0.015,
)


@dataclass(frozen=True)
class _Function:
    path: str
    receiver: str
    name: str
    line: int
    cyclomatic: int
    cognitive: int


def _tracked_go(root: Path) -> list[Path]:
    proc = subprocess.run(
        ["git", "ls-files", "-z", "--", "gateway-go/**/*.go"],
        cwd=root,
        check=False,
        capture_output=True,
    )
    if proc.returncode != 0:
        return []
    paths: list[Path] = []
    for raw in proc.stdout.split(b"\0"):
        if not raw:
            continue
        rel = raw.decode("utf-8", errors="replace")
        if rel.endswith("_test.go") or rel.endswith("_gen.go") or "/vendor/" in rel:
            continue
        paths.append(root / rel)
    return sorted(paths)


def _mask_comments_and_strings(text: str) -> str:
    """Replace comments and literals while preserving line breaks and braces."""
    out: list[str] = []
    i = 0
    state = "code"
    quote = ""
    while i < len(text):
        char = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""
        if state == "code":
            if char == "/" and nxt == "/":
                out.extend("  ")
                i += 2
                state = "line_comment"
                continue
            if char == "/" and nxt == "*":
                out.extend("  ")
                i += 2
                state = "block_comment"
                continue
            if char in {'"', "'", "`"}:
                quote = char
                out.append(" ")
                i += 1
                state = "string"
                continue
            out.append(char)
        elif state == "line_comment":
            if char == "\n":
                out.append("\n")
                state = "code"
            else:
                out.append(" ")
        elif state == "block_comment":
            if char == "*" and nxt == "/":
                out.extend("  ")
                i += 2
                state = "code"
                continue
            out.append("\n" if char == "\n" else " ")
        else:
            if char == "\n" and quote != "`":
                out.append("\n")
                state = "code"
            elif char == "\\" and quote != "`" and i + 1 < len(text):
                out.extend("  ")
                i += 2
                continue
            elif char == quote:
                out.append(" ")
                state = "code"
            else:
                out.append("\n" if char == "\n" else " ")
        i += 1
    return "".join(out)


def _body(masked: str, start: int) -> tuple[str, int] | None:
    brace = masked.find("{", start)
    if brace < 0:
        return None
    # A declaration or prototype cannot cross an unrelated top-level newline.
    if ";" in masked[start:brace]:
        return None
    depth = 1
    i = brace + 1
    while i < len(masked) and depth:
        if masked[i] == "{":
            depth += 1
        elif masked[i] == "}":
            depth -= 1
        i += 1
    if depth:
        return None
    return masked[brace + 1 : i - 1], i


def _complexities(body: str) -> tuple[int, int]:
    cyclomatic = 1
    cognitive = 0
    control_depth = 0
    brace_stack: list[bool] = []
    opening_controls = {"if", "for", "switch", "select"}
    for line in body.splitlines():
        controls = list(_CONTROL_RE.finditer(line))
        opening_braces = [index for index, char in enumerate(line) if char == "{"]
        assigned_control_braces: set[int] = set()
        for match in controls:
            if match.group(0) not in opening_controls:
                continue
            opening = next(
                (
                    index
                    for index in opening_braces
                    if index > match.end() and index not in assigned_control_braces
                ),
                None,
            )
            if opening is not None:
                assigned_control_braces.add(opening)

        events: list[tuple[int, str, str]] = [
            (match.start(), "control", match.group(0)) for match in controls
        ]
        events.extend(
            (index, "open" if char == "{" else "close", char)
            for index, char in enumerate(line)
            if char in "{}"
        )
        for position, kind, token in sorted(events, key=lambda event: event[0]):
            if kind == "close":
                if brace_stack and brace_stack.pop():
                    control_depth = max(0, control_depth - 1)
                continue
            if kind == "open":
                is_control = position in assigned_control_braces
                brace_stack.append(is_control)
                if is_control:
                    control_depth += 1
                continue
            if token in {"&&", "||"}:
                cyclomatic += 1
                cognitive += 1
            elif token == "case":
                # Cases add paths that tests must cover, but a flat declarative
                # switch is one decision to understand. Charging nesting for
                # every case made registries look worse than side-effect-heavy
                # nested control flow.
                cyclomatic += 1
            elif token in {"switch", "select"}:
                cognitive += 1 + min(control_depth, 4)
            else:
                cyclomatic += 1
                cognitive += 1 + min(control_depth, 4)
    return cyclomatic, cognitive


def _receiver_type(raw: str | None) -> str:
    if not raw:
        return ""
    parts = raw.split()
    compact = "".join((parts[-1] if parts else raw).split())
    # Receiver declarations are `name Type` or just `Type`; keep only the type
    # so renaming the local receiver variable does not churn a finding ID.
    match = re.search(r"(\*?[A-Za-z_]\w*(?:\[[^]]+\])?)$", compact)
    return match.group(1) if match else compact


def _read_sources(files: list[Path]) -> dict[Path, str]:
    def read(path: Path) -> tuple[Path, str]:
        try:
            return path, path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            return path, ""

    with ThreadPoolExecutor(max_workers=16) as pool:
        return dict(pool.map(read, files))


def _functions(
    root: Path, files: list[Path], texts: dict[Path, str]
) -> tuple[list[_Function], int]:
    found: list[_Function] = []
    loc = 0
    for path in files:
        text = texts.get(path, "")
        if not text:
            continue
        loc += text.count("\n") + 1
        masked = _mask_comments_and_strings(text)
        rel = path.relative_to(root).as_posix()
        for match in _FUNC_RE.finditer(masked):
            parsed = _body(masked, match.end())
            if parsed is None:
                continue
            body, _ = parsed
            cyclomatic, cognitive = _complexities(body)
            found.append(
                _Function(
                    path=rel,
                    receiver=_receiver_type(match.group(1)),
                    name=match.group(2),
                    line=masked.count("\n", 0, match.start()) + 1,
                    cyclomatic=cyclomatic,
                    cognitive=cognitive,
                )
            )
    return found, loc


def _percentile(values: list[int], quantile: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    index = max(0, min(len(ordered) - 1, math.ceil(quantile * len(ordered)) - 1))
    return float(ordered[index])


def _ranked_tail_score(values: list[int], *, soft: float, hard: float) -> float:
    """Grade a fixed worst tail with explicit emphasis on the top ranks."""
    worst = sorted((float(value) for value in values), reverse=True)[
        : len(_RANKED_TAIL_WEIGHTS)
    ]
    penalty = sum(
        weight * (100.0 - descending_grade(value, soft, hard))
        for value, weight in zip(
            worst, _RANKED_TAIL_WEIGHTS[: len(worst)], strict=True
        )
    )
    return 100.0 - penalty


def _complexity_pillar(root: Path, funcs: list[_Function], loc: int) -> Pillar:
    kloc = max(loc / 1000.0, 0.001)
    cyclop_hotspots = sum(func.cyclomatic > 10 for func in funcs)
    cognitive_hotspots = sum(func.cognitive > 30 for func in funcs)
    cyclop_density = cyclop_hotspots / kloc
    severe_density = cognitive_hotspots / kloc
    maximum_cognitive = max((func.cognitive for func in funcs), default=0)
    maximum_cyclomatic = max((func.cyclomatic for func in funcs), default=0)
    p95 = _percentile([func.cognitive for func in funcs], 0.95)
    # The stdlib parser counts boolean branches and switch cases a little more
    # aggressively than cyclop/gocognit. These bars are calibrated against the
    # repository's pinned analyzers (roughly +18%/+23%), not against a desired
    # composite score.
    subscores = {
        "cyclomatic_ranked_tail": _ranked_tail_score(
            [func.cyclomatic for func in funcs], soft=15, hard=120
        ),
        "cognitive_ranked_tail": _ranked_tail_score(
            [func.cognitive for func in funcs], soft=30, hard=200
        ),
    }
    # Worst-rank emphasis is already built into both tails. A separate maximum
    # would count the same function twice, while a raw hotspot total would make
    # project growth look like a regression even when no path became harder.
    score = geometric_mean(max(1.0, value) for value in subscores.values())
    findings: list[Finding] = []
    for func in sorted(funcs, key=lambda item: (-item.cognitive, -item.cyclomatic, item.path))[:8]:
        if func.cognitive <= 30 and func.cyclomatic <= 10:
            continue
        severity = "high" if func.cognitive >= 80 else "medium"
        findings.append(
            Finding(
                id=stable_id("complexity-hotspot", func.path, func.receiver, func.name),
                pillar="complexity-hotspots",
                severity=severity,
                path=f"{func.path}:{func.line}",
                evidence=(
                    f"{func.receiver + '.' if func.receiver else ''}{func.name}: "
                    f"cognitive proxy {func.cognitive}, cyclomatic proxy {func.cyclomatic}"
                ),
                why="A high-branch execution path is difficult to review safely and expensive for an AI or human to modify with complete context.",
                remediation="Extract named decisions and side effects behind narrow functions; add characterization tests before moving branches.",
                verify="make health-v2 && make go-test",
                priority=min(100.0, 45.0 + 0.25 * func.cognitive + 0.15 * func.cyclomatic),
            )
        )
    return Pillar(
        id="complexity-hotspots",
        title="Complexity hotspots",
        weight=7,
        score=score,
        intent="Keep the riskiest execution paths understandable; easy functions cannot hide a few extreme ones.",
        metrics={
            "subscores": {key: round(value, 1) for key, value in subscores.items()},
            "project_size": {
                "scored": False,
                "functions": len(funcs),
                "source_kloc": round(kloc, 1),
                "reason": "inventory context only; adding files is not a quality regression",
            },
            "hotspot_count": {
                "scored": False,
                "cyclomatic_over_10": cyclop_hotspots,
                "cognitive_over_30": cognitive_hotspots,
                "cyclomatic_over_10_per_kloc": round(cyclop_density, 3),
                "cognitive_over_30_per_kloc": round(severe_density, 3),
                "reason": "raw totals scale with the project and do not grade path difficulty",
            },
            "maximum": {
                "scored": False,
                "cognitive": maximum_cognitive,
                "cyclomatic": maximum_cyclomatic,
                "reason": "the worst function is already weighted inside each ranked tail",
            },
            "p95_cognitive": {
                "scored": False,
                "value": p95,
                "reason": "diagnostic distribution context; rank tails drive the score",
            },
        },
        findings=findings,
    )


def _call_arguments(text: str, masked: str, start: int) -> str | None:
    """Return original call arguments using masked text for balanced parsing."""
    opening = masked.find("(", start)
    if opening < 0:
        return None
    depth = 1
    cursor = opening + 1
    while cursor < len(masked) and depth:
        if masked[cursor] == "(":
            depth += 1
        elif masked[cursor] == ")":
            depth -= 1
        cursor += 1
    if depth:
        return None
    return text[opening + 1 : cursor - 1]


def _panic_contract_files(
    root: Path, sources: list[Path], source_texts: dict[Path, str]
) -> set[Path]:
    """Source files whose intentional panic API is documented and tested."""
    proc = subprocess.run(
        ["git", "ls-files", "-z", "--", "gateway-go/**/*_test.go"],
        cwd=root,
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        return set()
    tested_calls: dict[Path, set[str]] = {}
    for raw in proc.stdout.split(b"\0"):
        if not raw:
            continue
        path = root / raw.decode("utf-8", errors="replace")
        try:
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        masked = _mask_comments_and_strings(text)
        functions: dict[str, str] = {}
        for match in _FUNC_RE.finditer(masked):
            parsed = _body(masked, match.end())
            if parsed is not None:
                functions[match.group(2)] = parsed[0]
        recover_helpers = {
            name for name, body in functions.items() if re.search(r"\brecover\s*\(", body)
        }
        for name, body in functions.items():
            if not name.startswith("Test"):
                continue
            calls = set(re.findall(r"\b([A-Za-z_]\w*)\s*(?:\[[^]]*\])?\s*\(", body))
            recovers = bool(re.search(r"\brecover\s*\(", body)) or bool(calls & recover_helpers)
            expects = bool(re.search(r"panic|fail.?fast|repanic", name, re.IGNORECASE)) or any(
                re.search(r"panic|fail.?fast", helper, re.IGNORECASE) for helper in calls
            )
            if recovers and expects:
                tested_calls.setdefault(path.parent, set()).update(calls)

    proven: set[Path] = set()
    doc_pattern = re.compile(
        r"fail[ -]?fast|catchable\s+panic|invariant|panics?\s+if|"
        r"panic.{0,80}(?:recovery|test runner)|probe\s+panic",
        re.IGNORECASE | re.DOTALL,
    )
    for source in sources:
        calls = tested_calls.get(source.parent)
        if not calls:
            continue
        text = source_texts.get(source, "")
        if not text:
            continue
        if not doc_pattern.search(text):
            continue
        defined = {match.group(2) for match in _FUNC_RE.finditer(_mask_comments_and_strings(text))}
        if defined & calls:
            proven.add(source)
    return proven


def _runtime_pillar(
    root: Path, files: list[Path], _loc: int, texts: dict[Path, str]
) -> Pillar:
    ignored: list[tuple[str, int, str]] = []
    background: list[tuple[str, int, str]] = []
    fatal: list[tuple[str, int, str]] = []
    unwrapped: list[tuple[str, int, str]] = []
    http_total = http_timeout = 0
    wrap_total = wrap_ok = 0
    goroutines = supervised = 0
    panic_contract_files = _panic_contract_files(root, files, texts)
    for path in files:
        text = texts.get(path, "")
        if not text:
            continue
        masked = _mask_comments_and_strings(text)
        lines = text.splitlines()
        rel = path.relative_to(root).as_posix()
        for pattern, sink in ((_IGNORED_ERROR_RE, ignored), (_BG_CONTEXT_RE, background), (_FATAL_RE, fatal)):
            for match in pattern.finditer(masked):
                line = text.count("\n", 0, match.start()) + 1
                context = " ".join(lines[max(0, line - 2) : min(len(lines), line + 1)]).lower()
                if pattern in {_BG_CONTEXT_RE, _FATAL_RE} and (
                    rel.startswith("gateway-go/cmd/")
                    or rel.startswith("gateway-go/internal/runtime/bootstrap/")
                ):
                    # Process entrypoints and the lifecycle composition root own
                    # the root context and exit status by definition.
                    continue
                if (
                    pattern is _FATAL_RE
                    and masked[match.start() : match.end()].lstrip().startswith("panic")
                    and path in panic_contract_files
                ):
                    # A documented invariant panic with an executable recovery
                    # contract is an intentional API, not an unhandled crash.
                    continue
                if pattern is _IGNORED_ERROR_RE and any(word in context for word in ("best effort", "best-effort", "intentionally ignore", "nolint:errcheck")):
                    continue
                sink.append((rel, line, lines[line - 1].strip()[:180]))
        for match in _HTTP_CLIENT_RE.finditer(masked):
            http_total += 1
            if re.search(r"\bTimeout\s*:", match.group(1)):
                http_timeout += 1
        for match in _FMT_ERROR_CALL_RE.finditer(masked):
            body = _call_arguments(text, masked, match.start())
            if body is None or not re.search(
                r"\berr\b", _mask_comments_and_strings(body)
            ):
                continue
            wrap_total += 1
            if "%w" in body:
                wrap_ok += 1
                continue
            line = text.count("\n", 0, match.start()) + 1
            raw = lines[line - 1].strip()[:180] if lines else ""
            unwrapped.append((rel, line, raw))
        for match in re.finditer(r"(?m)^\s*go\s+", masked):
            goroutines += 1
            window = masked[max(0, match.start() - 500) : min(len(masked), match.end() + 1000)]
            if re.search(r"\b(?:safego|errgroup|WaitGroup|\.Wait\(|recover\s*\(|cancel\s*\()", window):
                supervised += 1

    unwrapped_errors = wrap_total - wrap_ok
    missing_timeouts = http_total - http_timeout
    unsupervised_goroutines = goroutines - supervised
    # These are attributable defects rather than volume proxies, so even one
    # occurrence should be visible. The different hard limits encode impact:
    # a library fatal path is more severe than a lost wrapping relationship.
    subscores = {
        "error_chain_preservation": descending_grade(unwrapped_errors, 0, 14),
        "fatal_path_hygiene": descending_grade(len(fatal), 0, 4),
    }
    weights = {
        "error_chain_preservation": 0.40,
        "fatal_path_hygiene": 0.60,
    }
    score = sum(subscores[key] * weights[key] for key in subscores)
    findings: list[Finding] = []
    categories = [
        (fatal, "fatal-runtime-path", "Process termination or panic in library code bypasses recovery and graceful shutdown.", "Return a typed error to the composition root or document and test an invariant-only panic."),
    ]
    for hits, rule, why, remediation in categories:
        by_file: dict[str, list[tuple[int, str]]] = {}
        for rel, line, raw in hits:
            by_file.setdefault(rel, []).append((line, raw))
        for rel, file_hits in sorted(by_file.items(), key=lambda item: (-len(item[1]), item[0]))[:4]:
            line, raw = file_hits[0]
            findings.append(
                Finding(
                    id=stable_id(rule, rel),
                    pillar="runtime-safety",
                    severity="high",
                    path=rel,
                    evidence=f"{len(file_hits)} hit(s); first at line {line}: {raw or rule}",
                    why=why,
                    remediation=remediation,
                    verify="make health-v2 && make go-test",
                    priority=80.0,
                )
            )
    unwrapped_by_file: dict[str, list[tuple[int, str]]] = {}
    for rel, line, raw in unwrapped:
        unwrapped_by_file.setdefault(rel, []).append((line, raw))
    for rel, file_hits in sorted(
        unwrapped_by_file.items(), key=lambda item: (-len(item[1]), item[0])
    )[:4]:
        line, raw = file_hits[0]
        findings.append(
            Finding(
                id=stable_id("lost-error-chain", rel),
                pillar="runtime-safety",
                severity="medium",
                path=rel,
                evidence=f"{len(file_hits)} fmt.Errorf call(s) lose the cause; first at line {line}: {raw}",
                why="Formatting a returned error without %w prevents callers from matching or attributing the underlying failure.",
                remediation="Wrap the causal error with %w, or return a typed replacement only when hiding the cause is an explicit boundary contract.",
                verify="make health-v2 && make go-test",
                priority=min(100.0, 60.0 + 2.0 * len(file_hits)),
            )
        )
    return Pillar(
        id="runtime-safety",
        title="Runtime safety and diagnosability",
        weight=7,
        score=score,
        intent="Grade attributable runtime failures from high-confidence evidence; keep lifecycle regexes diagnostic until ownership is proven.",
        metrics={
            "fatal_paths": len(fatal),
            "error_wrap": {"preserved": wrap_ok, "applicable": wrap_total},
            "subscores": {key: round(value, 1) for key, value in subscores.items()},
            "inline_http_client_timeout": {
                "scored": False,
                "inline_clients": http_total,
                "with_client_timeout": http_timeout,
                "without_client_timeout": missing_timeouts,
                "reason": "a request context may provide the effective outbound deadline",
            },
            "ignored_result_assignments": {
                "scored": False,
                "count": len(ignored),
                "reason": "a blank assignment does not prove the expression returns an actionable error",
            },
            "root_context_constructors": {
                "scored": False,
                "count": len(background),
                "reason": "a root context is risky only after its request or worker ownership is established",
            },
            "raw_goroutine_heuristic": {
                "scored": False,
                "starts": goroutines,
                "with_nearby_owner_keyword": supervised,
                "without_nearby_owner_keyword": unsupervised_goroutines,
                "reason": "nearby wait, recovery, or cancel keywords do not prove goroutine lifecycle ownership",
            },
        },
        findings=findings,
    )


def evaluate(root: Path) -> tuple[list[Pillar], list[Evidence]]:
    files = _tracked_go(root)
    if not files:
        return [], [Evidence("go-source", "unavailable", "git returned no tracked Go source", required=True)]
    texts = _read_sources(files)
    funcs, loc = _functions(root, files, texts)
    evidence = [
        Evidence("go-source", "measured", f"{len(files)} production files, {loc} lines", required=True),
        Evidence("go-function-structure", "measured", f"parsed {len(funcs)} function bodies", required=True),
    ]
    return [
        _complexity_pillar(root, funcs, loc),
        _runtime_pillar(root, files, loc, texts),
    ], evidence

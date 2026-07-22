"""Language-aware test parsing and semantic-shape analysis helpers.

The parser is intentionally lightweight and deterministic.  It recognizes the
repository's Go, Kotlin, TypeScript, and Python test conventions without
importing project toolchains or rewarding raw test LOC.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path
from typing import Pattern


@dataclass(frozen=True)
class Case:
    name: str
    body: str
    shape: str
    oracle: bool
    risk_signals: frozenset[str]


@dataclass(frozen=True)
class RiskRule:
    name: str
    source: Pattern[str]
    evidence: Pattern[str]


RISK_RULES: dict[str, tuple[RiskRule, ...]] = {
    "go": (
        RiskRule(
            "error-path",
            re.compile(r"\b(?:error\b|errors\.(?:New|Join)\s*\(|fmt\.Errorf\s*\()"),
            re.compile(
                r"invalid|error|reject|malform|rollback|unavailable|missing|timeout|cancel",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "protocol-boundary",
            re.compile(
                r'"(?:net/http|encoding/json)"|\bhttp\.Handler|'
                r"\bjson\.(?:Marshal|Unmarshal)"
            ),
            re.compile(
                r"httptest|status(?:bad|unauth|forbid|notfound)|malform|invalid|"
                r"unknown|round.?trip|encode|decode|reject",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "concurrency",
            re.compile(r'"sync(?:/atomic)?"|\bgo\s+func\b|\bchan\s+\w|<-'),
            re.compile(
                r"waitgroup|atomic\.|go\s+func|concurr|race|parallel|cancel|deadline",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "persistence",
            re.compile(
                r"os\.(?:WriteFile|OpenFile|Create|Rename)\s*\(|"
                r'"database/sql"|bbolt'
            ),
            re.compile(
                r"tempdir|reopen|restart|reload|corrupt|rollback|persist|migrat|atomic",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "external-io",
            re.compile(r'"os/exec"|\bhttp\.(?:Client|NewRequest)|\bnet\.Dial'),
            re.compile(
                r"httptest\.newserver|roundtripper|fake|mock|stub|integration|testserver",
                re.IGNORECASE,
            ),
        ),
    ),
    "kotlin": (
        RiskRule(
            "serialization-boundary",
            re.compile(
                r"kotlinx\.serialization|\bJson\.(?:decode|encode)|"
                r"decodeFromString|encodeToString"
            ),
            re.compile(
                r"assertfails|serializationexception|invalid|unknown|null|"
                r"round.?trip|decode|encode",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "coroutine-state",
            re.compile(
                r"\b(?:Mutex|CoroutineScope|StateFlow|SharedFlow|Channel)\b|"
                r"\bsuspend\s+fun\b"
            ),
            re.compile(
                r"runtest|advanceuntilidle|timeout|cancel|concurr|state|transition|dispatcher",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "persistence",
            re.compile(
                r"\b(?:DataStore|SqlDriver)\b|java\.io\.File|"
                r"\.(?:writeText|readText)\s*\("
            ),
            re.compile(
                r"temp|reopen|restart|reload|corrupt|rollback|persist|migrat",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "external-io",
            re.compile(r"\bHttpClient\b|ktor\.client|java\.net\.URL"),
            re.compile(r"mockengine|fake|mock|stub|timeout|error|reject", re.IGNORECASE),
        ),
    ),
    "typescript": (
        RiskRule(
            "protocol-boundary",
            re.compile(r"\bfetch\s*\(|\bJSON\.parse\s*\(|\brpc\s*\("),
            re.compile(
                r"vi\.fn|msw|error|reject|invalid|status|malform|unknown",
                re.IGNORECASE,
            ),
        ),
        RiskRule(
            "async-cancellation",
            re.compile(
                r"\bAbortController\b|\bPromise\s*<|\basync\s+function\b|"
                r"\bsetTimeout\s*\("
            ),
            re.compile(r"faketimer|abort|cancel|timeout|reject|waitfor", re.IGNORECASE),
        ),
        RiskRule(
            "browser-persistence",
            re.compile(r"\b(?:localStorage|sessionStorage|indexedDB)\b"),
            re.compile(r"clear|remove|reload|migrat|persist|restore", re.IGNORECASE),
        ),
    ),
    "python": (
        RiskRule(
            "failure-path",
            re.compile(r"(?m)^\s*(?:raise|except)\b"),
            re.compile(r"assertraises|error|invalid|reject|exception", re.IGNORECASE),
        ),
        RiskRule(
            "external-process",
            re.compile(r"\b(?:subprocess|urllib|requests|socket)\b"),
            re.compile(r"mock|patch|fake|stub|temporary|error|fail", re.IGNORECASE),
        ),
        RiskRule(
            "filesystem",
            re.compile(r"\bopen\s*\(|\.(?:write_text|write_bytes)\s*\("),
            re.compile(r"temporarydirectory|tempfile|patch|tmp|error", re.IGNORECASE),
        ),
    ),
}

_ORACLE_PATTERNS: dict[str, Pattern[str]] = {
    "go": re.compile(
        r"\bt\.(?:Fatalf?|Errorf?|FailNow|Fail)\s*\(|"
        r"\b(?:assert|require)\.\w+\s*\(|\bassert[A-Z]\w*\s*\("
    ),
    "kotlin": re.compile(r"\b(?:assert\w*|fail)\s*(?:<[^>]+>)?\s*(?:\(|\{)"),
    "typescript": re.compile(r"\bexpect\s*\(|\bassert(?:\.\w+)?\s*\("),
    "python": re.compile(
        r"\bself\.assert\w+\s*\(|\bpytest\.raises\s*\(|"
        r"\bself\.fail\s*\(|^\s*assert\s+",
        re.MULTILINE,
    ),
}

_INTENT_RE = re.compile(
    r"(?:reject|error|fail|invalid|malform|empty|nil|null|missing|unknown|"
    r"timeout|cancel|concurr|race|preserv|return|emit|write|read|load|save|"
    r"update|delete|create|round.?trip|encode|decode|allow|deny|ignore|"
    r"fallback|retry|idempot|rollback|recover|close|stop|start|when|with|"
    r"without|boundary|contract|migrat|evict|expire|truncat|normalize|"
    r"parse|format|stream|render|display|clear|restore|deduplicat|"
    r"표시|거부|유지|실패|복원|저장|삭제|취소|만료)",
    re.IGNORECASE,
)

_KEYWORDS = frozenset(
    "break case catch chan class const continue default defer do else false finally "
    "for fun func go if import in interface is let map new nil null object of package "
    "private public range return select struct suspend switch throw true try type "
    "undefined val var when while async await describe it test expect".split()
)
_STRING_RE = re.compile(
    r'"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|`(?:\\.|[^`\\])*`',
    re.DOTALL,
)


def unit_key(language: str, path: Path, text: str, root: Path) -> str:
    if language == "kotlin":
        match = re.search(
            r"(?m)^\s*package\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)", text
        )
        if match:
            return f"kotlin:{match.group(1)}"
    return f"{language}:{path.parent.relative_to(root).as_posix()}"


def source_symbols(language: str, text: str) -> set[str]:
    if language == "kotlin":
        modifiers = (
            r"(?:(?:public|private|internal|protected|data|sealed|open|abstract|"
            r"actual|expect|suspend|inline|operator|tailrec|override)\s+)*"
        )
        type_symbols = {
            match.group(1)
            for match in re.finditer(
                rf"(?m)^\s*{modifiers}(?:class|object|interface)\s+"
                r"`?([^`\s(<:{{]+)`?",
                text,
            )
        }
        function_symbols = {
            match.group(1)
            for match in re.finditer(
                rf"(?m)^\s*{modifiers}fun\s+"
                r"(?:<[^>\n]+>\s*)?"
                r"(?:[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>\n]+>)?\??\s*\.\s*)?"
                r"(?:`([^`]+)`|([A-Za-z_]\w*))\s*\(",
                text,
            )
            if match.group(1)
        }
        # The backtick and ordinary-name alternatives occupy separate groups.
        function_symbols.update(
            match.group(2)
            for match in re.finditer(
                rf"(?m)^\s*{modifiers}fun\s+"
                r"(?:<[^>\n]+>\s*)?"
                r"(?:[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>\n]+>)?\??\s*\.\s*)?"
                r"(?:`([^`]+)`|([A-Za-z_]\w*))\s*\(",
                text,
            )
            if match.group(2)
        )
        return {
            symbol
            for symbol in type_symbols | function_symbols
            if len(symbol) >= 4 and symbol.lower() not in {"string", "error", "test"}
        }
    patterns = {
        "go": r"(?m)^(?:func(?:\s*\([^)]*\))?\s+|type\s+)([A-Za-z_]\w*)",
        "typescript": r"(?m)^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?(?:function|class|interface|type|const|let)\s+([A-Za-z_$][\w$]*)",
        "python": r"(?m)^\s*(?:async\s+)?(?:def|class)\s+([A-Za-z_]\w*)",
    }
    return {
        match.group(1)
        for match in re.finditer(patterns[language], text)
        if len(match.group(1)) >= 4
        and match.group(1).lower() not in {"string", "error", "test"}
    }


def _strip_lexical(text: str) -> str:
    """Replace comments and quoted content while preserving braces/newlines."""
    out = list(text)
    i = 0
    state = "code"
    quote = ""
    while i < len(text):
        ch = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""
        if state == "code":
            if ch == "/" and nxt == "/":
                out[i] = out[i + 1] = " "
                i += 2
                state = "line"
                continue
            if ch == "/" and nxt == "*":
                out[i] = out[i + 1] = " "
                i += 2
                state = "block"
                continue
            if ch == "#" and (i == 0 or text[i - 1] == "\n"):
                out[i] = " "
                i += 1
                state = "line"
                continue
            if ch in {'"', "'", "`"}:
                quote = ch
                out[i] = " "
                i += 1
                state = "quote"
                continue
            i += 1
            continue
        if state == "line":
            if ch == "\n":
                state = "code"
            else:
                out[i] = " "
            i += 1
            continue
        if state == "block":
            if ch == "*" and nxt == "/":
                out[i] = out[i + 1] = " "
                i += 2
                state = "code"
            else:
                if ch != "\n":
                    out[i] = " "
                i += 1
            continue
        if ch == "\\" and quote != "`" and i + 1 < len(text):
            out[i] = out[i + 1] = " "
            i += 2
        elif ch == quote:
            out[i] = " "
            i += 1
            state = "code"
        else:
            if ch != "\n":
                out[i] = " "
            i += 1
    return "".join(out)


def _evidence_clean(text: str, language: str) -> str:
    clean = _strip_lexical(text)
    if language == "python":
        clean = re.sub(r"(?m)#.*$", " ", clean)
    return clean


def _strip_comments_keep_literals(text: str) -> str:
    out = list(text)
    i = 0
    state = "code"
    quote = ""
    while i < len(text):
        ch = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""
        if state == "code":
            if ch == "/" and nxt == "/":
                out[i] = out[i + 1] = " "
                i += 2
                state = "line"
                continue
            if ch == "/" and nxt == "*":
                out[i] = out[i + 1] = " "
                i += 2
                state = "block"
                continue
            if ch == "#":
                out[i] = " "
                i += 1
                state = "line"
                continue
            if ch in {'"', "'", "`"}:
                quote = ch
                state = "quote"
            i += 1
            continue
        if state == "line":
            if ch == "\n":
                state = "code"
            else:
                out[i] = " "
            i += 1
            continue
        if state == "block":
            if ch == "*" and nxt == "/":
                out[i] = out[i + 1] = " "
                i += 2
                state = "code"
            else:
                if ch != "\n":
                    out[i] = " "
                i += 1
            continue
        if ch == "\\" and quote != "`" and i + 1 < len(text):
            i += 2
        elif ch == quote:
            i += 1
            state = "code"
        else:
            i += 1
    return "".join(out)


def risk_source_text(language: str, text: str) -> str:
    """Code-only risk evidence plus real Go import declarations."""
    clean = _evidence_clean(text, language)
    if language != "go":
        return clean
    commentless = _strip_comments_keep_literals(text)
    imports: list[str] = []
    imports.extend(
        re.findall(r'(?m)^\s*import\s+(?:[A-Za-z_.]\w*\s+)?"([^"\n]+)"', commentless)
    )
    for block in re.findall(r"(?ms)^\s*import\s*\((.*?)^\s*\)", commentless):
        imports.extend(re.findall(r'(?m)^\s*(?:[A-Za-z_.]\w*\s+)?"([^"\n]+)"', block))
    return clean + "\n" + "\n".join(f'"{item}"' for item in imports)


def _matching_brace(clean: str, opening: int) -> int:
    depth = 0
    for pos in range(opening, len(clean)):
        if clean[pos] == "{":
            depth += 1
        elif clean[pos] == "}":
            depth -= 1
            if depth == 0:
                return pos + 1
    return len(clean)


def _brace_functions(text: str, language: str) -> list[tuple[str, str, int]]:
    clean = _strip_lexical(text)
    if language == "go":
        pattern = re.compile(r"(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(")
    elif language == "kotlin":
        pattern = re.compile(
            r"(?m)^\s*(?:(?:public|private|internal|protected|override|suspend|inline|"
            r"operator|tailrec|open|actual|expect)\s+)*fun\s+"
            r"(?:`([^`]+)`|([A-Za-z_]\w*))\s*\("
        )
    else:
        return []
    found: list[tuple[str, str, int]] = []
    for match in pattern.finditer(clean):
        name = next((group for group in match.groups() if group), "")
        opening = clean.find("{", match.end())
        if opening < 0:
            continue
        next_match = pattern.search(clean, match.end())
        if next_match and opening > next_match.start():
            continue
        end = _matching_brace(clean, opening)
        found.append((name, text[opening + 1 : end - 1], match.start()))
    return found


def _python_functions(text: str) -> list[tuple[str, str, int]]:
    lines = text.splitlines(keepends=True)
    offsets: list[int] = []
    offset = 0
    for line in lines:
        offsets.append(offset)
        offset += len(line)
    found: list[tuple[str, str, int]] = []
    pattern = re.compile(r"^(\s*)(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(")
    for index, line in enumerate(lines):
        match = pattern.match(line)
        if not match:
            continue
        indent = len(match.group(1).replace("\t", "    "))
        end = index + 1
        while end < len(lines):
            stripped = lines[end].strip()
            current = len(lines[end]) - len(lines[end].lstrip(" \t"))
            if stripped and current <= indent:
                break
            end += 1
        found.append((match.group(2), "".join(lines[index + 1 : end]), offsets[index]))
    return found


def _ts_cases(text: str) -> list[tuple[str, str, int]]:
    clean = _strip_lexical(text)
    call = re.compile(r"\b(?:it|test)(?:\.each\s*\([^;]*?\))?\s*\(")
    result: list[tuple[str, str, int]] = []
    for match in call.finditer(clean):
        original = text[match.start() : min(len(text), match.start() + 500)]
        # Non-backtracking string literal after `(` — avoid nested backrefs that
        # ReDoS on inputs like ("\\\\a\\\\a...").
        name_match = re.search(
            r"""\(\s*(?:"((?:\\.|[^"\\])*)"|'((?:\\.|[^'\\])*)'|`((?:\\.|[^`\\])*)`)""",
            original,
            re.DOTALL,
        )
        name = "anonymous test"
        if name_match:
            name = next((g for g in name_match.groups() if g is not None), "anonymous test")
        arrow = clean.find("=>", match.end())
        if arrow < 0 or arrow - match.end() > 1200:
            continue
        opening = clean.find("{", arrow)
        if opening < 0 or opening - arrow > 160:
            continue
        end = _matching_brace(clean, opening)
        result.append((name, text[opening + 1 : end - 1], match.start()))
    return result


def _literal_class(match: re.Match[str]) -> str:
    value = match.group(0)[1:-1]
    if not value:
        return " STR_EMPTY "
    if not value.strip():
        return " STR_SPACE "
    if any(ord(char) > 127 for char in value):
        return " STR_UNICODE "
    if "://" in value:
        return " STR_URL "
    if "/" in value or "\\" in value:
        return " STR_PATH "
    return " STR "


def _semantic_shape(body: str) -> str:
    shaped = re.sub(r"/\*.*?\*/", " ", body, flags=re.DOTALL)
    shaped = re.sub(r"//[^\n]*|^\s*#[^\n]*$", " ", shaped, flags=re.MULTILINE)
    shaped = _STRING_RE.sub(_literal_class, shaped)

    def number(match: re.Match[str]) -> str:
        try:
            value = float(match.group(0))
        except ValueError:
            return " NUM "
        if value == 0:
            return " NUM_ZERO "
        if abs(value) == 1:
            return " NUM_ONE "
        return " NUM "

    shaped = re.sub(r"(?<![\w.])-?\d+(?:\.\d+)?", number, shaped)
    tokens = re.findall(
        r"[A-Za-z_$][\w$]*|STR_(?:EMPTY|SPACE|UNICODE|URL|PATH)|STR|"
        r"NUM_(?:ZERO|ONE)|NUM|===|!==|==|!=|:=|=>|<=|>=|&&|\|\||\S",
        shaped,
    )
    normalized = [
        token
        if token.lower() in _KEYWORDS or token.startswith(("STR", "NUM"))
        else "ID"
        if re.match(r"[A-Za-z_$]", token)
        else token
        for token in tokens
    ]
    return " ".join(normalized)


def test_cases(language: str, text: str) -> list[Case]:
    oracle_pattern = _ORACLE_PATTERNS[language]
    raw: list[tuple[str, str, int]]
    functions: list[tuple[str, str, int]] = []
    if language in {"go", "kotlin"}:
        functions = _brace_functions(text, language)
        if language == "go":
            raw = [item for item in functions if item[0].startswith("Test")]
        else:
            raw = []
            for name, body, pos in functions:
                prefix = text[max(0, pos - 220) : pos]
                if re.search(r"@Test(?:\([^)]*\))?\s*(?:\r?\n\s*)?$", prefix):
                    raw.append((name, body, pos))
    elif language == "typescript":
        raw = _ts_cases(text)
    else:
        functions = _python_functions(text)
        raw = [item for item in functions if item[0].startswith("test_")]

    clean_by_function = {
        name: _evidence_clean(body, language) for name, body, _ in functions
    }
    calls_by_function = {
        name: set(re.findall(r"\b([A-Za-z_]\w*)\s*\(", clean))
        for name, clean in clean_by_function.items()
    }
    oracle_helpers = {
        name for name, clean in clean_by_function.items() if oracle_pattern.search(clean)
    }
    for _ in range(4):
        expanded = {name for name, calls in calls_by_function.items() if calls & oracle_helpers}
        if expanded <= oracle_helpers:
            break
        oracle_helpers.update(expanded)

    cases: list[Case] = []
    for name, body, _ in raw:
        clean_body = clean_by_function.get(name)
        if clean_body is None:
            clean_body = _evidence_clean(body, language)
        calls = calls_by_function.get(name)
        if calls is None:
            calls = set(re.findall(r"\b([A-Za-z_]\w*)\s*\(", clean_body))
        oracle = bool(oracle_pattern.search(clean_body)) or bool(calls & oracle_helpers)

        reachable = set(calls) & clean_by_function.keys()
        frontier = set(reachable)
        for _ in range(4):
            expanded = {
                target
                for helper in frontier
                for target in calls_by_function.get(helper, set())
                if target in clean_by_function
            } - reachable
            if not expanded:
                break
            reachable.update(expanded)
            frontier = expanded
        fragments = [name, clean_body]
        fragments.extend(clean_by_function[helper] for helper in sorted(reachable))
        risk_signals = frozenset(
            rule.name
            for rule in RISK_RULES[language]
            if any(rule.evidence.search(fragment) for fragment in fragments)
        )
        cases.append(
            Case(
                name=name,
                body=body,
                shape=_semantic_shape(body),
                oracle=oracle,
                risk_signals=risk_signals,
            )
        )
    return cases


def has_observable_oracle(language: str, text: str) -> bool:
    """Report whether executable code contains a language assertion."""
    return bool(_ORACLE_PATTERNS[language].search(_evidence_clean(text, language)))


def normalized_subject(value: str) -> str:
    value = re.sub(r"^(?:test[_-]?)", "", value, flags=re.IGNORECASE)
    value = re.sub(r"(?:tests?|spec)$", "", value, flags=re.IGNORECASE)
    value = re.sub(r"(?:[_-]?shell)$", "", value, flags=re.IGNORECASE)
    return re.sub(r"[^a-z0-9]", "", value.lower())


def test_stem(language: str, path: Path) -> str:
    if language == "go":
        return normalized_subject(path.name[: -len("_test.go")])
    if language == "kotlin":
        return normalized_subject(path.stem)
    if language == "typescript":
        return normalized_subject(re.sub(r"\.(?:test|spec)$", "", path.stem))
    return normalized_subject(path.stem)


def case_has_intent(name: str) -> bool:
    parts = re.findall(
        r"[A-Z]?[a-z]+|[A-Z]+(?![a-z])|[가-힣]+|\d+", name.replace("_", " ")
    )
    return len(parts) >= 2 and bool(_INTENT_RE.search(name))


def hazards(language: str, text: str) -> tuple[str, ...]:
    found: list[str] = []
    if language == "go":
        if "time.Sleep(" in text:
            found.append("raw time.Sleep")
        if re.search(r"\bos\.Setenv\s*\(", text) and "t.Setenv(" not in text:
            found.append("process environment mutation")
        if re.search(r"\bos\.Chdir\s*\(", text) and "t.Cleanup(" not in text:
            found.append("working-directory mutation")
        if re.search(r'["\']/tmp/', text) and "t.TempDir(" not in text:
            found.append("fixed /tmp path")
        if re.search(r"\b(?:http\.Get|http\.Post|net\.Dial)\s*\(", text) and not re.search(
            r"httptest\.|RoundTripper", text
        ):
            found.append("real network call")
    elif language == "kotlin":
        if "Thread.sleep(" in text:
            found.append("raw Thread.sleep")
        if "GlobalScope." in text:
            found.append("GlobalScope")
    elif language == "typescript":
        if re.search(r"\bsetTimeout\s*\(", text) and "useFakeTimers" not in text:
            found.append("real timer")
        if "vi.useFakeTimers(" in text and not re.search(
            r"vi\.(?:useRealTimers|restoreAllMocks)\s*\(", text
        ):
            found.append("fake timer not restored")
        if "stubGlobal(" in text and not re.search(
            r"unstubAllGlobals|restoreAllMocks|afterEach", text
        ):
            found.append("global stub not restored")
    else:
        if "time.sleep(" in text and not re.search(r"mock|patch", text, re.IGNORECASE):
            found.append("raw time.sleep")
        if re.search(r"\bos\.chdir\s*\(|\bos\.environ(?:\[|\.update)", text) and not re.search(
            r"patch\.dict|addCleanup|finally", text
        ):
            found.append("process-global mutation")
        if "/tmp/" in text and not re.search(
            r"TemporaryDirectory|NamedTemporaryFile|mkdtemp", text
        ):
            found.append("fixed /tmp path")
    return tuple(found)


__all__ = [
    "Case",
    "RISK_RULES",
    "case_has_intent",
    "hazards",
    "has_observable_oracle",
    "normalized_subject",
    "risk_source_text",
    "source_symbols",
    "test_cases",
    "test_stem",
    "unit_key",
]

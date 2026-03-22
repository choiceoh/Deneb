#!/usr/bin/env python3
"""Check that changed English doc titles/labels have zh-CN glossary entries."""

import json
import os
import re
import subprocess
import sys

DOC_FILE_RE = re.compile(r"^docs/(?!zh-CN/).+\.(md|mdx)$", re.IGNORECASE)
LIST_ITEM_LINK_RE = re.compile(r"^\s*(?:[-*]|\d+\.)\s+\[([^\]]+)\]\((/[^)]+)\)")
MAX_TITLE_WORDS = 8
MAX_LABEL_WORDS = 6
MAX_TERM_LENGTH = 80


def parse_args(argv: list[str]) -> dict[str, str]:
    args = {"base": "", "head": ""}
    i = 0
    while i < len(argv):
        if argv[i] == "--base" and i + 1 < len(argv):
            args["base"] = argv[i + 1]
            i += 2
            continue
        if argv[i] == "--head" and i + 1 < len(argv):
            args["head"] = argv[i + 1]
            i += 2
            continue
        i += 1
    return args


def run_git(git_args: list[str], cwd: str | None = None) -> str:
    result = subprocess.run(
        ["git", *git_args],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise subprocess.CalledProcessError(result.returncode, "git")
    return result.stdout.strip()


def resolve_base(explicit_base: str) -> str:
    if explicit_base:
        return explicit_base

    env_base = os.environ.get("DOCS_I18N_GLOSSARY_BASE", "").strip()
    if env_base:
        return env_base

    for candidate in ("origin/main", "fork/main", "main"):
        try:
            return run_git(["merge-base", candidate, "HEAD"])
        except subprocess.CalledProcessError:
            continue
    return ""


def list_changed_docs(base: str, head: str) -> list[str]:
    git_args = ["diff", "--name-only", "--diff-filter=ACMR", base]
    if head:
        git_args.append(head)
    git_args.extend(["--", "docs"])

    output = run_git(git_args)
    return [line.strip() for line in output.split("\n") if DOC_FILE_RE.match(line.strip())]


def load_glossary_sources(glossary_path: str) -> set[str]:
    with open(glossary_path, encoding="utf-8") as f:
        entries = json.load(f)
    return {str(e.get("source", "")).strip() for e in entries if str(e.get("source", "")).strip()}


def contains_latin(text: str) -> bool:
    return bool(re.search(r"[A-Za-z]", text))


def word_count(text: str) -> int:
    return len(text.split())


def unquote_scalar(raw: str) -> str:
    value = raw.strip()
    if len(value) >= 2 and (
        (value[0] == '"' and value[-1] == '"') or (value[0] == "'" and value[-1] == "'")
    ):
        return value[1:-1].strip()
    return value


def is_glossary_candidate(term: str, max_words: int) -> bool:
    if not term:
        return False
    if not contains_latin(term):
        return False
    if "`" in term:
        return False
    if len(term) > MAX_TERM_LENGTH:
        return False
    return word_count(term) <= max_words


def read_git_file(base: str, rel_path: str) -> str:
    try:
        return run_git(["show", f"{base}:{rel_path}"])
    except subprocess.CalledProcessError:
        return ""


def extract_terms(file: str, text: str) -> dict[str, dict]:
    terms: dict[str, dict] = {}
    lines = text.split("\n")

    # Parse frontmatter title
    if lines and lines[0].strip() == "---":
        for index in range(1, len(lines)):
            line = lines[index]
            if line.strip() == "---":
                break
            match = re.match(r"^title:\s*(.+)\s*$", line)
            if not match:
                continue
            title = unquote_scalar(match.group(1))
            if is_glossary_candidate(title, MAX_TITLE_WORDS):
                terms[title] = {"file": file, "line": index + 1, "kind": "title", "term": title}
            break

    # Parse list item links
    for index, line in enumerate(lines):
        match = LIST_ITEM_LINK_RE.match(line)
        if not match:
            continue
        label = match.group(1).strip()
        if not is_glossary_candidate(label, MAX_LABEL_WORDS):
            continue
        if label not in terms:
            terms[label] = {"file": file, "line": index + 1, "kind": "link label", "term": label}

    return terms


def main() -> None:
    root = os.getcwd()
    glossary_path = os.path.join(root, "docs", ".i18n", "glossary.zh-CN.json")

    args = parse_args(sys.argv[1:])
    base = resolve_base(args["base"])

    if not base:
        sys.stderr.write(
            "docs:check-i18n-glossary: no merge base found; skipping glossary coverage check.\n"
        )
        sys.exit(0)

    changed_docs = list_changed_docs(base, args["head"])
    if not changed_docs:
        sys.exit(0)

    glossary = load_glossary_sources(glossary_path)
    missing: list[dict] = []

    for rel_path in changed_docs:
        abs_path = os.path.join(root, rel_path)
        if not os.path.exists(abs_path):
            continue

        with open(abs_path, encoding="utf-8") as f:
            current_text = f.read()

        current_terms = extract_terms(rel_path, current_text)
        base_terms = extract_terms(rel_path, read_git_file(base, rel_path))

        for term, match in current_terms.items():
            if term in base_terms:
                continue
            if term in glossary:
                continue
            missing.append(match)

    if not missing:
        sys.exit(0)

    sys.stderr.write(
        "docs:check-i18n-glossary: missing zh-CN glossary entries for changed doc labels:\n"
    )
    for match in missing:
        sys.stderr.write(f"- {match['file']}:{match['line']} {match['kind']} \"{match['term']}\"\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Add exact source terms to docs/.i18n/glossary.zh-CN.json before rerunning docs-i18n.\n"
    )
    sys.stderr.write(f"Checked changed English docs relative to {base}.\n")
    sys.exit(1)


if __name__ == "__main__":
    main()

"""Recent production-Go change history for Health Bench 2.0."""

from __future__ import annotations

import subprocess
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable


HISTORY_LIMIT = 400
MIN_HISTORY_COMMITS = 20


@dataclass(frozen=True)
class ChangeCommit:
    """Production Go paths changed by one first-parent commit."""

    packages: tuple[tuple[str, int], ...]

    @property
    def package_count(self) -> int:
        return len(self.packages)

    @property
    def file_count(self) -> int:
        return sum(count for _, count in self.packages)


@dataclass(frozen=True)
class HistoryFacts:
    available: bool
    detail: str
    commits: tuple[ChangeCommit, ...]
    package_touches: dict[str, int]
    multipackage_touches: dict[str, int]
    cochange_counts: dict[tuple[str, str], int]

    @property
    def commit_count(self) -> int:
        return len(self.commits)


def collect_history(repo_root: Path) -> HistoryFacts:
    command = [
        "git",
        "-C",
        str(repo_root),
        "log",
        "--first-parent",
        "--no-merges",
        f"-n{HISTORY_LIMIT}",
        "--find-renames",
        "--format=@@%H",
        "--name-only",
        "--",
        "gateway-go/internal",
    ]
    try:
        process = subprocess.run(
            command,
            capture_output=True,
            check=False,
            text=True,
            # Source inventory runs concurrently and can temporarily saturate
            # WSL filesystem I/O; this is a tooling timeout, not a quality gap.
            timeout=30,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return _unavailable_history(f"git history unavailable: {exc}")
    if process.returncode != 0:
        detail = process.stderr.strip().splitlines()
        reason = detail[-1] if detail else f"git exited {process.returncode}"
        return _unavailable_history(f"git history unavailable: {reason}")

    # actions/checkout uses a synthetic merge commit for pull requests. A
    # first-parent log correctly follows the base branch, but --no-merges would
    # otherwise omit the PR change entirely and only score it after merge. Treat
    # the merge diff against its first parent as one production change, which is
    # equivalent to the later squash commit for locality/co-change purposes.
    try:
        parent_process = subprocess.run(
            ["git", "-C", str(repo_root), "rev-list", "--parents", "-n1", "HEAD"],
            capture_output=True,
            check=False,
            text=True,
            timeout=10,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return _unavailable_history(
            f"git history unavailable: could not inspect HEAD parents: {exc}"
        )
    if parent_process.returncode != 0:
        return _unavailable_history("git history unavailable: could not inspect HEAD parents")
    parent_fields = parent_process.stdout.strip().split()
    merge_change: ChangeCommit | None = None
    if len(parent_fields) >= 3:
        try:
            diff_process = subprocess.run(
                [
                    "git",
                    "-C",
                    str(repo_root),
                    "diff",
                    "--name-only",
                    "--find-renames",
                    parent_fields[1],
                    "HEAD",
                    "--",
                    "gateway-go/internal",
                ],
                capture_output=True,
                check=False,
                text=True,
                timeout=15,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            return _unavailable_history(
                f"git history unavailable: could not inspect merge change: {exc}"
            )
        if diff_process.returncode != 0:
            return _unavailable_history("git history unavailable: could not inspect merge change")
        merge_change = _history_commit(diff_process.stdout.splitlines())

    commits: list[ChangeCommit] = []
    current: set[str] = set()
    for line in [*process.stdout.splitlines(), "@@END"]:
        if line.startswith("@@"):
            commit = _history_commit(current)
            if commit is not None:
                commits.append(commit)
            current = set()
            continue
        path = line.strip()
        if path:
            current.add(path)

    if merge_change is not None:
        commits.insert(0, merge_change)
    commits = commits[:HISTORY_LIMIT]

    if len(commits) < MIN_HISTORY_COMMITS:
        return _unavailable_history(
            f"only {len(commits)} production-Go commits available; need {MIN_HISTORY_COMMITS}"
        )

    touches: Counter[str] = Counter()
    multipackage: Counter[str] = Counter()
    cochange: Counter[tuple[str, str]] = Counter()
    for commit in commits:
        packages = [package for package, _ in commit.packages]
        touches.update(packages)
        if len(packages) > 1:
            multipackage.update(packages)
        for index, source in enumerate(packages):
            for target in packages[index + 1 :]:
                cochange[(source, target)] += 1
    return HistoryFacts(
        available=True,
        detail=(
            f"{len(commits)} first-parent production-Go changes measured"
            + (" (HEAD merge diff included)" if merge_change is not None else "")
        ),
        commits=tuple(commits),
        package_touches=dict(sorted(touches.items())),
        multipackage_touches=dict(sorted(multipackage.items())),
        cochange_counts=dict(sorted(cochange.items())),
    )


def _unavailable_history(detail: str) -> HistoryFacts:
    return HistoryFacts(
        available=False,
        detail=detail,
        commits=(),
        package_touches={},
        multipackage_touches={},
        cochange_counts={},
    )


def _history_commit(paths: Iterable[str]) -> ChangeCommit | None:
    package_files: dict[str, set[str]] = defaultdict(set)
    for path in paths:
        if not path.endswith(".go") or path.endswith(("_test.go", "_gen.go")):
            continue
        parts = Path(path).parts
        if len(parts) < 4 or parts[:2] != ("gateway-go", "internal"):
            continue
        package = Path(*parts[1:-1]).as_posix()
        package_files[package].add(path)
    if not package_files:
        return None
    return ChangeCommit(
        packages=tuple(
            (package, len(package_files[package])) for package in sorted(package_files)
        )
    )

"""Shared fixtures for the Health Bench 2.0 self-tests.

This module contains no test cases. Keeping the Git repository fixture and
schema checker here lets the contract and adversarial modules stay focused.
"""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any

AUDIT_DIR = Path(__file__).resolve().parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

from health_v2 import inventory
from health_v2.model import Evidence, Finding, Pillar, Report


def finding(
    finding_id: str,
    *,
    pillar: str = "alpha",
    severity: str = "medium",
    priority: float = 50.0,
) -> Finding:
    return Finding(
        id=finding_id,
        pillar=pillar,
        severity=severity,
        path="gateway-go/internal/example/example.go:10",
        evidence="The risky branch has no observable failure contract.",
        why="Callers cannot distinguish a rejected input from an internal failure.",
        remediation="Return a typed error and add a focused boundary test.",
        verify="go test ./internal/example",
        priority=priority,
        related_paths=("gateway-go/internal/example/example_test.go",),
    )


def report(
    alpha: float = 70.0,
    beta: float = 70.0,
    *,
    findings: list[Finding] | None = None,
    evidence: list[Evidence] | None = None,
    readiness: dict[str, bool | None] | None = None,
) -> Report:
    return Report(
        profile="fast",
        revision="abcdef1234567890",
        pillars=[
            Pillar(
                id="alpha",
                title="Alpha",
                weight=50,
                score=alpha,
                intent="Keep the first contract explicit.",
                findings=findings or [],
            ),
            Pillar(
                id="beta",
                title="Beta",
                weight=50,
                score=beta,
                intent="Keep the second contract explicit.",
            ),
        ],
        evidence=evidence
        if evidence is not None
        else [Evidence("inventory", "measured", "fixture inventory", required=True)],
        readiness=readiness if readiness is not None else {"go-test": True},
    )


def pillar(pillars: list[Pillar], pillar_id: str) -> Pillar:
    return next(item for item in pillars if item.id == pillar_id)


def architecture_pillars(repo: inventory.RepositoryInventory) -> list[Pillar]:
    """Score an already collected fixture without repeating Git inventory I/O."""
    from health_v2 import architecture

    ai_facts = architecture.collect_ai_readiness(repo)
    return [
        architecture._boundary_integrity(repo),
        architecture._change_locality(repo),
        architecture._responsibility_cohesion(repo),
        architecture._contract_explicitness(repo),
        architecture._ai_navigability(repo, ai_facts),
        architecture._ai_change_readiness(repo, ai_facts),
    ]


class GitFixture:
    """Small tracked repository used by source analyzers."""

    def __init__(self) -> None:
        self._temporary = tempfile.TemporaryDirectory(prefix="deneb-health-v2-test-")
        self.root = Path(self._temporary.name)
        subprocess.run(
            ["git", "init", "-q"],
            cwd=self.root,
            check=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        self.initial_branch = self.git("symbolic-ref", "--short", "HEAD")
        self.git("config", "user.name", "Health Bench Test")
        self.git("config", "user.email", "health-bench@example.invalid")

    def write(self, relative: str, content: str) -> None:
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def track(self) -> None:
        self.git("add", "-A")

    def git(self, *arguments: str) -> str:
        process = subprocess.run(
            ["git", *arguments],
            cwd=self.root,
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        return process.stdout.strip()

    def commit(self, message: str) -> str:
        self.track()
        self.git("commit", "-q", "-m", message)
        return self.git("rev-parse", "HEAD")

    def close(self) -> None:
        self._temporary.cleanup()

    def __enter__(self) -> GitFixture:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()


def _matches_type(value: object, expected: str) -> bool:
    if expected == "object":
        return isinstance(value, dict)
    if expected == "array":
        return isinstance(value, list)
    if expected == "string":
        return isinstance(value, str)
    if expected == "number":
        return isinstance(value, (int, float)) and not isinstance(value, bool)
    if expected == "integer":
        return isinstance(value, int) and not isinstance(value, bool)
    if expected == "boolean":
        return isinstance(value, bool)
    if expected == "null":
        return value is None
    raise AssertionError(f"unsupported fixture schema type: {expected}")


def assert_schema(
    test: unittest.TestCase,
    value: object,
    schema: dict[str, Any],
    path: str,
) -> None:
    """Validate the JSON Schema subset used by the checked-in contracts."""
    if "const" in schema:
        test.assertEqual(value, schema["const"], path)
    if "enum" in schema:
        test.assertIn(value, schema["enum"], path)

    expected_types = schema.get("type")
    if expected_types:
        candidates = (
            expected_types if isinstance(expected_types, list) else [expected_types]
        )
        test.assertTrue(
            any(_matches_type(value, candidate) for candidate in candidates),
            f"{path}: expected {candidates}, got {type(value).__name__}",
        )

    if isinstance(value, dict):
        required = schema.get("required", [])
        test.assertTrue(set(required) <= set(value), f"{path}: missing required keys")
        properties = schema.get("properties", {})
        additional = schema.get("additionalProperties", True)
        if additional is False:
            test.assertTrue(set(value) <= set(properties), f"{path}: unexpected keys")
        test.assertGreaterEqual(len(value), schema.get("minProperties", 0), path)
        for key, item in value.items():
            child = properties.get(key)
            if child is None and isinstance(additional, dict):
                child = additional
            if child is not None:
                assert_schema(test, item, child, f"{path}.{key}")
    elif isinstance(value, list):
        test.assertGreaterEqual(len(value), schema.get("minItems", 0), path)
        if schema.get("uniqueItems"):
            canonical = [json.dumps(item, sort_keys=True) for item in value]
            test.assertEqual(len(canonical), len(set(canonical)), path)
        if isinstance(schema.get("items"), dict):
            for index, item in enumerate(value):
                assert_schema(test, item, schema["items"], f"{path}[{index}]")
    elif isinstance(value, str):
        test.assertGreaterEqual(len(value), schema.get("minLength", 0), path)
    elif isinstance(value, (int, float)) and not isinstance(value, bool):
        if "minimum" in schema:
            test.assertGreaterEqual(value, schema["minimum"], path)
        if "maximum" in schema:
            test.assertLessEqual(value, schema["maximum"], path)
        if "exclusiveMinimum" in schema:
            test.assertGreater(value, schema["exclusiveMinimum"], path)

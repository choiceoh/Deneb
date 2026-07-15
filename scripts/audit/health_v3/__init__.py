"""Health Bench 3.0 — structure + runtime + RSI fitness.

Design source of truth: ``docs/research/health-bench-3.0.md``.
"""

from .model import (
    DOMAIN_WEIGHTS,
    RUBRIC_VERSION,
    SCHEMA_VERSION,
    Domain,
    Evidence,
    Finding,
    Metric,
    Report,
    clamp,
    geometric_composite,
)

__all__ = [
    "DOMAIN_WEIGHTS",
    "RUBRIC_VERSION",
    "SCHEMA_VERSION",
    "Domain",
    "Evidence",
    "Finding",
    "Metric",
    "Report",
    "clamp",
    "geometric_composite",
]

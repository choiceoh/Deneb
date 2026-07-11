"""Health Bench 2.0 implementation package.

The public CLI lives in ``scripts/audit/codebase-health-v2.py``.  Keeping the
scoring rules in a package makes the benchmark itself reviewable and testable.
"""

from .model import Evidence, Finding, Pillar, Report

__all__ = ["Evidence", "Finding", "Pillar", "Report"]

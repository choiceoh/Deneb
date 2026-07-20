---
title: "MemoHarness 2607.14159 Analysis"
summary: "How Deneb adopts MemoHarness-style six-dimensional experience without weakening deterministic promotion gates."
read_when:
  - Auditing Genesis experience retrieval or harness diagnosis
  - Changing failure routing, evolve prompts, or lifecycle audit metadata
sidebarTitle: "MemoHarness"
---

# MemoHarness (arXiv:2607.14159) - Deneb adoption

Source: <https://arxiv.org/abs/2607.14159>

## Adopted principle

MemoHarness separates harness adaptation into six control dimensions:

1. context assembly;
2. tool interaction;
3. generation control;
4. orchestration;
5. memory management; and
6. output processing.

Its useful production lesson is not to add another unconstrained optimizer. It
is to retain per-case and global experience, diagnose which control dimension a
failure belongs to, retrieve relevant precedents, and adapt the case-specific
harness while preserving correctness.

## Existing Deneb equivalents

Genesis already retained the important experience layers:

- recent verifier-grounded failure patterns and rejected-edit records;
- confirmed evolve exemplars with exact, mechanism, and bounded semantic
  retrieval;
- per-skill optimizer memory and cross-skill low-yield levers; and
- held-out replay, independent judging, post-evolve watch, and rollback.

## Added mapping

Deneb attaches a deterministic primary/secondary six-dimensional diagnosis to
each usage failure trace, aggregated failure route, and evolve audit. The
diagnosis is rendered into failure evidence and confirmed-exemplar prompts,
including legacy records backfilled at read time. It narrows the likely
intervention surface but never changes target files, dispatch eligibility,
validation scores, or promotion decisions.

## Deliberately not adopted

- No retrieved example can authorize a mutation or bypass validation.
- No model-authored dimension is trusted as ground truth; Deneb derives it from
  failure routing and the concrete edited surface.
- No online production rewrite is enabled. Skill edits remain patch-sized and
  source-level corrections stay in their separate reviewed coding lane.

The paper is treated as supporting transfer evidence. Its reported task set is
useful for the architectural pattern, but Deneb's own held-out cases and live
post-evolve outcomes remain the product evidence.

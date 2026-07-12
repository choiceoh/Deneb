// Package genesis is Deneb's recursive self-improvement subsystem — the loop
// that evolves the agent's own skills and, at the meta level, the process that
// does the evolving.
//
// # Layers
//
//   - L1 skill evolution: the evolver rewrites underperforming SKILL.md bodies
//     behind a deterministic gate stack (bounded patch + LLM judge + held-out
//     replay + post-evolve rollback watch). See evolver*.go, tracker*.go.
//   - L2 meta-evolution (the slow loop): a weekly task proposes revisions to
//     the evolve/judge system prompts themselves, gated by epoch-specific
//     deterministic benches (judge-degradation gold pairs, producer
//     shadow-replay), and — once bench-clean — auto-adopts them with a
//     rollback watch. See meta_evolution.go, meta_judge_bench.go,
//     meta_producer_bench.go.
//   - L3 verifier co-evolution (in preparation): the judge learns from labeled
//     mistakes. Its label substrate is grown deterministically by the
//     judge-accuracy lane (planted-defect replay + false-reject mining). See
//     judge_accuracy.go.
//   - L4 source self-edit (operator-authorized): evidence-bearing code
//     candidates (runtime-error mining, tool gaps) file propose-only
//     scope=code corrections through the review lane. The source surface is a
//     declared propose-only editable surface; the acceptance machinery stays
//     forbidden. See runtime_error_mining.go, evolver_tool_gap.go.
//
// The L2 meta-evidence block (assembleEvidence) is grounded by ADVISORY
// operator-utility signals — feed-card verdicts, runtime health (p95 latency),
// and codebase-health baseline score. These inform the producer's prose but no
// gate reads them; the deterministic acceptance circuit stays pure.
//
// # Invariants
//
// The LLM only ever PRODUCES (candidate bodies, cases, exhibits). Every
// accept/reject/adopt/rollback decision is deterministic Go, so the acceptance
// mechanism is never optimized by the loop it accepts for. The acceptance
// machinery is a forbidden self-edit surface (see the surfaces subpackage),
// and the evolution-drift self-brake (evolution_drift.go) freezes auto-adoption
// when the ledgers show the trajectory drifting toward reward-hacking.
//
// # Persistence
//
// All state lives in append-only JSONL under ~/.deneb/data (usage, lifecycle
// log, validation cases, rejected edits, meta-revision ledger, judge-accuracy
// ledger). The Tracker owns those files and the in-memory aggregates rebuilt
// from them on startup.
//
// Roadmap and rationale: docs/research/recursive-self-improvement-roadmap.md.
package genesis

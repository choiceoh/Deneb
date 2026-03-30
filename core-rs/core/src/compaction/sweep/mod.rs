//! Step-based sweep state machine for context compaction.
//!
//! The `SweepEngine` drives the compaction algorithm as a state machine that
//! yields I/O commands (`SweepCommand`) to the host language (TypeScript/Go).
//! The host executes the command (DB read, LLM call, DB write) and feeds the
//! result back via `SweepResponse`, then calls `step()` for the next command.
//!
//! This design avoids callbacks across FFI boundaries while keeping all
//! algorithmic logic in Rust.
//!
//! ## State Machine Diagram
//!
//! ```text
//!  ┌────────────────────────────────────────────────────────────────────────┐
//!  │                         SweepEngine FSM                                │
//!  └────────────────────────────────────────────────────────────────────────┘
//!
//!  start() → FetchTokenCount
//!
//!  ┌──────┐
//!  │ Init │──(count ≤ threshold && !force)──────────────────────────► Done
//!  └──────┘
//!      │ (count > threshold || force)
//!      ▼
//!  ┌────────────────┐
//!  │ FetchingItems  │──(items empty)──────────────────────────────── ► Done
//!  └────────────────┘
//!      │ (msg IDs present)       │ (no msg IDs)
//!      ▼                         │
//!  ┌──────────────────────┐      │
//!  │ PrefetchingMessages  │      │
//!  └──────────────────────┘      │
//!      └──────────────────────────┘
//!      ▼
//!  ══════════════════════════════ Phase 1: Leaf passes ══════════════════════
//!
//!  ┌────────────┐ ◄─────────────────────────────────────────────────────────┐
//!  │ LeafSelect │──(iter ≥ max || no chunk)──► transition_to_condensed_phase │
//!  └────────────┘                                                            │
//!      │ (prior summaries needed & uncached)                                 │
//!      ▼                                                                     │
//!  ┌───────────────────────┐                                                 │
//!  │ LeafFetchPriorSumm.   │                                                 │
//!  └───────────────────────┘                                                 │
//!      │ (either path above)                                                 │
//!      ▼  [prepare_leaf_summarize]                                           │
//!  ┌──────────────────────┐                                                  │
//!  │ LeafSummarizeNormal  │──(summary < source tokens)──► LeafPersist path  │
//!  └──────────────────────┘                                                  │
//!      │ (not reduced enough)                                                │
//!      ▼                                                                     │
//!  ┌────────────────────────────┐                                            │
//!  │ LeafSummarizeAggressive    │──(any outcome)─────► LeafPersist path     │
//!  └────────────────────────────┘                                            │
//!      ▼  [prepare_leaf_persist]                                             │
//!  ┌─────────────┐                                                           │
//!  │ LeafPersist │                                                           │
//!  └─────────────┘                                                           │
//!      ▼                                                                     │
//!  ┌──────────────────────┐                                                  │
//!  │ LeafPostTokenCount   │                                                  │
//!  └──────────────────────┘                                                  │
//!      ▼                                                                     │
//!  ┌───────────────────┐                                                     │
//!  │ LeafPersistEvent  │──► FetchingItems ──────────────────────────────────┘
//!  └───────────────────┘    (re-fetch after mutation, loop back to LeafSelect)
//!
//!  ══════════════════════════════ Phase 2: Condensed passes ════════════════
//!  (entered when leaf iterations exhausted or no more leaf chunks)
//!
//!  ┌───────────────────────┐
//!  │ CondensedFetchDepths  │──(iter ≥ max || no candidate)─────────► Done
//!  └───────────────────────┘
//!      │ (summaries missing from cache)
//!      ▼
//!  ┌─────────────────────────┐
//!  │ CondensedFetchSummaries │──(no candidate found)──────────────► Done
//!  └─────────────────────────┘
//!      │ (candidate found; either path above converges here)
//!      ▼  [prepare_condensed_summarize]
//!  ┌──────────────────────────────┐
//!  │ CondensedFetchPriorSummaries │  (depth=0 only, if uncached)
//!  └──────────────────────────────┘
//!      │ (either path above)
//!      ▼  [emit_condensed_summarize]
//!  ┌─────────────────────────────┐
//!  │ CondensedSummarizeNormal    │──(summary < source tokens)──► Condensed persist path
//!  └─────────────────────────────┘
//!      │ (not reduced enough)
//!      ▼
//!  ┌─────────────────────────────────┐
//!  │ CondensedSummarizeAggressive    │──(any outcome)──► Condensed persist path
//!  └─────────────────────────────────┘
//!      ▼  [prepare_condensed_persist]
//!  ┌──────────────────┐
//!  │ CondensedPersist │
//!  └──────────────────┘
//!      ▼
//!  ┌─────────────────────────┐
//!  │ CondensedPostTokenCount │
//!  └─────────────────────────┘
//!      ▼
//!  ┌──────────────────────────┐
//!  │ CondensedPersistEvent    │──► FetchingItems ──► LeafSelect
//!  └──────────────────────────┘    (re-fetch; LeafSelect re-enters condensed
//!                                   phase since leaf_iter already at max)
//!  ┌──────┐
//!  │ Done │  (yields SweepCommand::Done { result: CompactionResult })
//!  └──────┘
//! ```
//!
//! ### Summarization escalation (both phases)
//!
//! Each summarization step tries three levels in order, stopping at the first
//! that produces a smaller token count than the source:
//!
//! ```text
//!  Normal ──(not smaller)──► Aggressive ──(not smaller)──► Fallback (deterministic)
//! ```

mod condensed;
mod engine;
mod leaf;
pub mod types;

pub use engine::SweepEngine;
pub use types::*;

// ── Tests ───────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use crate::compaction::CompactionConfig;

    fn default_config() -> CompactionConfig {
        CompactionConfig::default()
    }

    #[test]
    fn test_sweep_below_threshold_returns_done() -> Result<(), Box<dyn std::error::Error>> {
        let mut engine = SweepEngine::new(default_config(), 1, 1000, false, false, 1000);
        let cmd = engine.start();
        assert!(matches!(cmd, SweepCommand::FetchTokenCount { .. }));

        // Report tokens below threshold (750)
        let cmd = engine.step(SweepResponse::TokenCount { count: 500 });
        match cmd {
            SweepCommand::Done { result } => {
                assert!(!result.action_taken);
                assert_eq!(result.tokens_before, 500);
            }
            _ => return Err(format!("Expected Done, got {:?}", cmd).into()),
        }
        Ok(())
    }

    #[test]
    fn test_sweep_empty_items_returns_done() -> Result<(), Box<dyn std::error::Error>> {
        let mut engine = SweepEngine::new(default_config(), 1, 1000, false, false, 1000);
        let _ = engine.start();
        let cmd = engine.step(SweepResponse::TokenCount { count: 800 });
        assert!(matches!(cmd, SweepCommand::FetchContextItems { .. }));

        let cmd = engine.step(SweepResponse::ContextItems { items: vec![] });
        match cmd {
            SweepCommand::Done { result } => {
                assert!(!result.action_taken);
            }
            _ => return Err(format!("Expected Done, got {:?}", cmd).into()),
        }
        Ok(())
    }

    #[test]
    fn test_sweep_force_skips_threshold() {
        let mut engine = SweepEngine::new(default_config(), 1, 1000, true, false, 1000);
        let _ = engine.start();

        // Even below threshold, force should proceed
        let cmd = engine.step(SweepResponse::TokenCount { count: 500 });
        assert!(matches!(cmd, SweepCommand::FetchContextItems { .. }));
    }

    #[test]
    fn test_sweep_command_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
        let cmd = SweepCommand::FetchMessages {
            message_ids: vec![1, 2, 3],
        };
        let json = serde_json::to_string(&cmd)?;
        let parsed: SweepCommand = serde_json::from_str(&json)?;
        match parsed {
            SweepCommand::FetchMessages { message_ids } => {
                assert_eq!(message_ids, vec![1, 2, 3]);
            }
            _ => return Err("Wrong variant".into()),
        }
        Ok(())
    }

    #[test]
    fn test_sweep_response_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
        let resp = SweepResponse::TokenCount { count: 42 };
        let json = serde_json::to_string(&resp)?;
        let parsed: SweepResponse = serde_json::from_str(&json)?;
        match parsed {
            SweepResponse::TokenCount { count } => assert_eq!(count, 42),
            _ => return Err("Wrong variant".into()),
        }
        Ok(())
    }
}

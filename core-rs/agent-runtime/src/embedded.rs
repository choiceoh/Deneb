//! Embedded PI agent run state tracking.
//!
//! Mirrors `src/agents/pi-embedded-runner/runs.ts` and
//! `src/agents/pi-embedded-runner/types.ts`. Keep in sync.
//!
//! This is a pure-logic state tracker. The host (TS/Go) manages actual
//! process handles, abort signals, and timers.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Snapshot of an active embedded run for external inspection.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ActiveEmbeddedRunSnapshot {
    pub transcript_leaf_id: Option<String>,
    pub in_flight_prompt: Option<String>,
}

/// Metadata about an embedded PI agent run.
/// Matches TS `EmbeddedPiAgentMeta` from `src/agents/pi-embedded-runner/types.ts`.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EmbeddedPiAgentMeta {
    pub session_id: Option<String>,
    pub session_key: Option<String>,
    pub provider: Option<String>,
    pub model: Option<String>,
    pub compaction_count: Option<u32>,
    pub prompt_tokens: Option<u64>,
    pub input_tokens: Option<u64>,
    pub output_tokens: Option<u64>,
    pub cache_read_tokens: Option<u64>,
    pub cache_write_tokens: Option<u64>,
}

/// A payload entry in an embedded PI run result.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EmbeddedPiRunPayload {
    pub text: Option<String>,
    pub media_url: Option<String>,
    pub media_urls: Option<Vec<String>>,
    pub reply_to_id: Option<String>,
    #[serde(default)]
    pub is_error: bool,
}

/// Result of an embedded PI agent run.
/// Matches TS `EmbeddedPiRunResult` from `src/agents/pi-embedded-runner/types.ts`.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EmbeddedPiRunResult {
    pub payloads: Option<Vec<EmbeddedPiRunPayload>>,
    pub meta: Option<EmbeddedPiRunMeta>,
    #[serde(default)]
    pub did_send_via_messaging_tool: bool,
    pub messaging_tool_sent_texts: Option<Vec<String>>,
    pub messaging_tool_sent_media_urls: Option<Vec<String>>,
    pub successful_cron_adds: Option<u32>,
    pub orphaned_user_prompt: Option<String>,
}

/// Result of a compaction operation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EmbeddedPiCompactResult {
    pub session_id: String,
    pub success: bool,
    pub messages_before: Option<usize>,
    pub messages_after: Option<usize>,
    pub tokens_saved: Option<u64>,
}

/// Metadata about an embedded PI run (runtime state).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EmbeddedPiRunMeta {
    pub session_id: String,
    pub session_key: Option<String>,
    pub is_streaming: bool,
    pub is_compacting: bool,
}

/// State of an active embedded run.
#[derive(Debug, Clone)]
struct ActiveRun {
    pub meta: EmbeddedPiRunMeta,
    pub snapshot: Option<ActiveEmbeddedRunSnapshot>,
}

/// Pure-logic tracker for active embedded PI runs.
/// The host manages actual process handles; this tracks metadata and state.
#[derive(Debug, Default)]
pub struct EmbeddedRunTracker {
    runs: HashMap<String, ActiveRun>,
}

impl EmbeddedRunTracker {
    pub fn new() -> Self {
        Self::default()
    }

    /// Register an active run.
    pub fn set_active(&mut self, session_id: &str, meta: EmbeddedPiRunMeta) {
        self.runs.insert(
            session_id.to_string(),
            ActiveRun {
                meta,
                snapshot: None,
            },
        );
    }

    /// Clear an active run (run completed/aborted).
    pub fn clear_active(&mut self, session_id: &str) -> bool {
        self.runs.remove(session_id).is_some()
    }

    /// Update the snapshot for an active run.
    pub fn update_snapshot(&mut self, session_id: &str, snapshot: ActiveEmbeddedRunSnapshot) {
        if let Some(run) = self.runs.get_mut(session_id) {
            run.snapshot = Some(snapshot);
        }
    }

    /// Check if a session has an active run.
    pub fn is_active(&self, session_id: &str) -> bool {
        self.runs.contains_key(session_id)
    }

    /// Check if a session is currently streaming.
    pub fn is_streaming(&self, session_id: &str) -> bool {
        self.runs
            .get(session_id)
            .map(|r| r.meta.is_streaming)
            .unwrap_or(false)
    }

    /// Check if a session is active by session key (linear scan).
    pub fn is_active_by_session_key(&self, session_key: &str) -> bool {
        self.runs
            .values()
            .any(|r| r.meta.session_key.as_deref() == Some(session_key))
    }

    /// Get the count of active runs.
    pub fn active_count(&self) -> usize {
        self.runs.len()
    }

    /// Get the snapshot for a session.
    pub fn get_snapshot(&self, session_id: &str) -> Option<&ActiveEmbeddedRunSnapshot> {
        self.runs.get(session_id)?.snapshot.as_ref()
    }

    /// Get metadata for all active runs.
    pub fn active_run_metas(&self) -> Vec<&EmbeddedPiRunMeta> {
        self.runs.values().map(|r| &r.meta).collect()
    }

    /// Clear all active runs (for restart/reset).
    pub fn reset(&mut self) {
        self.runs.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_meta(session_id: &str) -> EmbeddedPiRunMeta {
        EmbeddedPiRunMeta {
            session_id: session_id.to_string(),
            session_key: Some(format!("agent:bot:{}", session_id)),
            is_streaming: true,
            is_compacting: false,
        }
    }

    #[test]
    fn set_and_check_active() {
        let mut tracker = EmbeddedRunTracker::new();
        tracker.set_active("s1", make_meta("s1"));
        assert!(tracker.is_active("s1"));
        assert!(!tracker.is_active("s2"));
        assert_eq!(tracker.active_count(), 1);
    }

    #[test]
    fn clear_active() {
        let mut tracker = EmbeddedRunTracker::new();
        tracker.set_active("s1", make_meta("s1"));
        assert!(tracker.clear_active("s1"));
        assert!(!tracker.is_active("s1"));
        assert_eq!(tracker.active_count(), 0);
    }

    #[test]
    fn is_streaming() {
        let mut tracker = EmbeddedRunTracker::new();
        let mut meta = make_meta("s1");
        meta.is_streaming = false;
        tracker.set_active("s1", meta);
        assert!(!tracker.is_streaming("s1"));
    }

    #[test]
    fn is_active_by_session_key() {
        let mut tracker = EmbeddedRunTracker::new();
        tracker.set_active("s1", make_meta("s1"));
        assert!(tracker.is_active_by_session_key("agent:bot:s1"));
        assert!(!tracker.is_active_by_session_key("agent:bot:s2"));
    }

    #[test]
    fn update_snapshot() -> Result<(), Box<dyn std::error::Error>> {
        let mut tracker = EmbeddedRunTracker::new();
        tracker.set_active("s1", make_meta("s1"));
        tracker.update_snapshot(
            "s1",
            ActiveEmbeddedRunSnapshot {
                transcript_leaf_id: Some("leaf1".to_string()),
                in_flight_prompt: None,
            },
        );
        let snap = tracker.get_snapshot("s1").ok_or("get_snapshot returned None")?;
        assert_eq!(snap.transcript_leaf_id.as_deref(), Some("leaf1"));
        Ok(())
    }

    #[test]
    fn reset_clears_all() {
        let mut tracker = EmbeddedRunTracker::new();
        tracker.set_active("s1", make_meta("s1"));
        tracker.set_active("s2", make_meta("s2"));
        tracker.reset();
        assert_eq!(tracker.active_count(), 0);
    }
}

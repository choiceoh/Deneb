//! Context engine — Rust implementation of the Aurora context management system.
//!
//! This module provides:
//! - Core types matching TypeScript `context-engine/types.ts`
//! - Aurora configuration with three-tier resolution (env > plugin > defaults)
//! - Context assembly state machine (DAG-aware token budgeting)
//! - Retrieval operations (grep, describe, expand) via step-based I/O protocol
//!
//! Like the compaction sweep engine, assembly and retrieval use a command/response
//! protocol to keep all algorithmic logic in Rust while delegating DB/LLM I/O
//! to the host (TypeScript/Go).

pub mod assembler;
pub mod napi;
pub mod retrieval;

use serde::{Deserialize, Serialize};
use thiserror::Error;

// ── Errors ───────────────────────────────────────────────────────────────────

#[derive(Error, Debug)]
pub enum ContextEngineError {
    #[error("invalid config JSON: {0}")]
    InvalidConfig(#[from] serde_json::Error),

    #[error("engine not found: {0}")]
    EngineNotFound(String),

    #[error("unexpected response type for current phase")]
    UnexpectedResponse,

    #[error("assembly failed: {0}")]
    AssemblyFailed(String),

    #[error("retrieval failed: {0}")]
    RetrievalFailed(String),
}

// ── Result types (matching TypeScript context-engine/types.ts) ───────────────

/// Result of assembling model context.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssembleResult {
    /// Estimated total tokens in assembled context.
    pub estimated_tokens: u64,
    /// Number of raw messages included.
    pub raw_message_count: u32,
    /// Number of summaries included.
    pub summary_count: u32,
    /// Total context items considered.
    pub total_context_items: u32,
    /// Optional context-engine-provided instructions prepended to the runtime system prompt.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub system_prompt_addition: Option<String>,
    /// Ordered context item IDs (message IDs as "msg_{id}", summary IDs as-is).
    pub selected_item_ids: Vec<String>,
}

/// Result of a compaction operation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CompactResult {
    pub ok: bool,
    pub compacted: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason_code: Option<String>,
}

/// Result of ingesting a message.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IngestResult {
    pub ingested: bool,
}

/// Result of bootstrapping an engine.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct BootstrapResult {
    pub bootstrapped: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub imported_messages: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

// ── Engine metadata ──────────────────────────────────────────────────────────

/// Metadata describing a context engine implementation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ContextEngineInfo {
    pub id: String,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub owns_compaction: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub accepts_session_key: Option<bool>,
}

// ── Aurora Configuration ─────────────────────────────────────────────────────

/// Aurora context engine configuration.
///
/// Matches TypeScript `AuroraConfig` with three-tier resolution:
/// env vars > plugin config > hardcoded defaults.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AuroraConfig {
    /// Whether Aurora is enabled (default: true).
    #[serde(default = "default_true")]
    pub enabled: bool,

    /// Path to the Aurora SQLite database.
    #[serde(default = "default_database_path")]
    pub database_path: String,

    /// Context threshold as fraction of budget (default: 0.75, clamped [0.1, 1.0]).
    #[serde(default = "default_context_threshold")]
    pub context_threshold: f64,

    /// Number of recent messages to protect from compaction (default: 32).
    #[serde(default = "default_fresh_tail_count")]
    pub fresh_tail_count: u32,

    /// Minimum depth-0 summaries for condensation (default: 8).
    #[serde(default = "default_leaf_min_fanout")]
    pub leaf_min_fanout: u32,

    /// Minimum depth>=1 summaries for condensation (default: 4).
    #[serde(default = "default_condensed_min_fanout")]
    pub condensed_min_fanout: u32,

    /// Relaxed min fanout for hard-trigger sweeps (default: 2).
    #[serde(default = "default_condensed_min_fanout_hard")]
    pub condensed_min_fanout_hard: u32,

    /// Incremental depth passes after each leaf compaction (default: 0, -1 = infinity).
    #[serde(default)]
    pub incremental_max_depth: i32,

    /// Max source tokens per leaf chunk (default: 30000).
    #[serde(default = "default_leaf_chunk_tokens")]
    pub leaf_chunk_tokens: u32,

    /// Target tokens for leaf summaries (default: 1500).
    #[serde(default = "default_leaf_target_tokens")]
    pub leaf_target_tokens: u32,

    /// Target tokens for condensed summaries (default: 2500).
    #[serde(default = "default_condensed_target_tokens")]
    pub condensed_target_tokens: u32,

    /// Max tokens for expand operations (default: 6000).
    #[serde(default = "default_max_expand_tokens")]
    pub max_expand_tokens: u32,

    /// Token threshold for large file interception (default: 35000).
    #[serde(default = "default_large_file_token_threshold")]
    pub large_file_token_threshold: u32,

    /// Whether auto-compaction is disabled (default: false).
    #[serde(default)]
    pub autocompact_disabled: bool,

    /// IANA timezone for timestamps in summaries (default: UTC).
    #[serde(default = "default_timezone")]
    pub timezone: String,

    /// Whether to prune heartbeat-ok cycles (default: false).
    #[serde(default)]
    pub prune_heartbeat_ok: bool,
}

fn default_true() -> bool {
    true
}
fn default_database_path() -> String {
    "~/.deneb/aurora.db".to_string()
}
fn default_context_threshold() -> f64 {
    0.75
}
fn default_fresh_tail_count() -> u32 {
    32
}
fn default_leaf_min_fanout() -> u32 {
    8
}
fn default_condensed_min_fanout() -> u32 {
    4
}
fn default_condensed_min_fanout_hard() -> u32 {
    2
}
fn default_leaf_chunk_tokens() -> u32 {
    30_000
}
fn default_leaf_target_tokens() -> u32 {
    1_500
}
fn default_condensed_target_tokens() -> u32 {
    2_500
}
fn default_max_expand_tokens() -> u32 {
    6_000
}
fn default_large_file_token_threshold() -> u32 {
    35_000
}
fn default_timezone() -> String {
    "UTC".to_string()
}

impl Default for AuroraConfig {
    fn default() -> Self {
        Self {
            enabled: true,
            database_path: default_database_path(),
            context_threshold: 0.75,
            fresh_tail_count: 32,
            leaf_min_fanout: 8,
            condensed_min_fanout: 4,
            condensed_min_fanout_hard: 2,
            incremental_max_depth: 0,
            leaf_chunk_tokens: 30_000,
            leaf_target_tokens: 1_500,
            condensed_target_tokens: 2_500,
            max_expand_tokens: 6_000,
            large_file_token_threshold: 35_000,
            autocompact_disabled: false,
            timezone: "UTC".to_string(),
            prune_heartbeat_ok: false,
        }
    }
}

impl AuroraConfig {
    /// Clamp context_threshold to [0.1, 1.0].
    pub fn validated(mut self) -> Self {
        self.context_threshold = self.context_threshold.clamp(0.1, 1.0);
        self
    }
}

// ── Shared context item types ────────────────────────────────────────────────

/// Type of a resolved context item for assembly.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ResolvedItemKind {
    Message,
    Summary,
}

/// A resolved context item with token cost for assembly.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ResolvedContextItem {
    pub ordinal: u64,
    pub kind: ResolvedItemKind,
    /// "msg_{id}" for messages, summary ID for summaries.
    pub item_id: String,
    /// Token cost of this item.
    pub token_count: u64,
    /// Summary depth (0 for messages).
    pub depth: u32,
    /// Whether this is a condensed summary (depth > 0).
    pub is_condensed: bool,
}

/// Summary metadata for system prompt guidance decisions.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SummaryStats {
    pub max_depth: u32,
    pub condensed_count: u32,
    pub leaf_count: u32,
    pub total_summary_tokens: u64,
}

// ── Pure utility functions ───────────────────────────────────────────────────

/// Estimate token count from text length (ceil(len / 4)).
#[inline]
pub fn estimate_tokens(text: &str) -> u64 {
    (text.len() as u64).div_ceil(4)
}

// ── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_aurora_config_defaults() {
        let config = AuroraConfig::default();
        assert!(config.enabled);
        assert_eq!(config.context_threshold, 0.75);
        assert_eq!(config.fresh_tail_count, 32);
        assert_eq!(config.leaf_chunk_tokens, 30_000);
        assert_eq!(config.leaf_target_tokens, 1_500);
        assert_eq!(config.condensed_target_tokens, 2_500);
        assert_eq!(config.max_expand_tokens, 6_000);
        assert_eq!(config.timezone, "UTC");
    }

    #[test]
    fn test_aurora_config_serde_roundtrip() {
        let config = AuroraConfig::default();
        let json = serde_json::to_string(&config).unwrap();
        let parsed: AuroraConfig = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.context_threshold, 0.75);
        assert_eq!(parsed.fresh_tail_count, 32);
    }

    #[test]
    fn test_aurora_config_partial_json() {
        let json = r#"{"contextThreshold": 0.5, "freshTailCount": 16}"#;
        let config: AuroraConfig = serde_json::from_str(json).unwrap();
        assert_eq!(config.context_threshold, 0.5);
        assert_eq!(config.fresh_tail_count, 16);
        assert_eq!(config.leaf_chunk_tokens, 30_000); // default
    }

    #[test]
    fn test_aurora_config_validated_clamp() {
        let mut config = AuroraConfig::default();
        config.context_threshold = 2.0;
        let validated = config.validated();
        assert_eq!(validated.context_threshold, 1.0);

        let mut config2 = AuroraConfig::default();
        config2.context_threshold = 0.01;
        let validated2 = config2.validated();
        assert_eq!(validated2.context_threshold, 0.1);
    }

    #[test]
    fn test_estimate_tokens() {
        assert_eq!(estimate_tokens(""), 0);
        assert_eq!(estimate_tokens("abcd"), 1);
        assert_eq!(estimate_tokens("abcde"), 2);
    }

    #[test]
    fn test_context_engine_info_serde() {
        let info = ContextEngineInfo {
            id: "aurora".to_string(),
            name: "Aurora Context Engine".to_string(),
            version: Some("1.0.0".to_string()),
            owns_compaction: Some(true),
            accepts_session_key: Some(true),
        };
        let json = serde_json::to_string(&info).unwrap();
        let parsed: ContextEngineInfo = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.id, "aurora");
        assert_eq!(parsed.accepts_session_key, Some(true));
    }

    #[test]
    fn test_assemble_result_serde() {
        let result = AssembleResult {
            estimated_tokens: 5000,
            raw_message_count: 10,
            summary_count: 3,
            total_context_items: 13,
            system_prompt_addition: Some("guidance".to_string()),
            selected_item_ids: vec!["msg_1".to_string(), "sum_abc".to_string()],
        };
        let json = serde_json::to_string(&result).unwrap();
        let parsed: AssembleResult = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.estimated_tokens, 5000);
        assert_eq!(parsed.selected_item_ids.len(), 2);
    }
}
